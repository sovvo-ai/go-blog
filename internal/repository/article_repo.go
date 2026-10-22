package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-blog/internal/model"
)

// ArticleRepo 是 articles / categories / tags 的数据访问对象。
type ArticleRepo struct {
	db *gorm.DB
}

// NewArticleRepo 构造 ArticleRepo。
func NewArticleRepo(db *gorm.DB) *ArticleRepo { return &ArticleRepo{db: db} }

// ListFilter 是文章列表查询条件。
type ListFilter struct {
	AuthorID   uint64  // 0 表示不限
	Statuses   []uint8 // 为空表示不限
	CategoryID *uint64 // nil 表示不限；指向 0 表示"未分类"
	TagID      uint64  // 0 表示不限
	Sort       string  // latest | hot
	Limit      int
	CursorTime *time.Time // latest 游标
	CursorID   uint64
	CursorView uint64 // hot 游标
}

// Create 在事务内写入文章与标签关联，并维护分类/标签计数。
func (r *ArticleRepo) Create(ctx context.Context, a *model.Article, tagIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(a).Error; err != nil {
			if isDuplicate(err) {
				return ErrDuplicate
			}
			return err
		}
		if len(tagIDs) > 0 {
			rows := make([]model.ArticleTag, 0, len(tagIDs))
			for _, id := range tagIDs {
				rows = append(rows, model.ArticleTag{ArticleID: a.ID, TagID: id})
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
				return err
			}
		}
		return bumpCounters(tx, a, nil, tagIDs)
	})
}

// Update 更新文章内容，使用 version 做乐观锁。
// 返回 ErrConflict 表示有人抢先修改；返回 ErrDuplicate 表示 slug 冲突。
func (r *ArticleRepo) Update(ctx context.Context, a *model.Article, tagIDs []uint64, oldTagIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 只更新内容字段，状态与计数不在这里改
		res := tx.Model(&model.Article{}).
			Where("id = ? AND version = ?", a.ID, a.Version).
			Updates(map[string]any{
				"title":         a.Title,
				"slug":          a.Slug,
				"summary":       a.Summary,
				"content_md":    a.ContentMD,
				"content_html":  a.ContentHTML,
				"cover_url":     a.CoverURL,
				"category_id":   a.CategoryID,
				"allow_comment": a.AllowComment,
				"word_count":    a.WordCount,
				"reading_time":  a.ReadingTime,
				"version":       a.Version + 1,
				// 显式推进同步状态，供对账任务扫描；不依赖 updated_at 判断
				"search_sync_status": model.SearchSyncPending,
				"updated_at":         time.Now(),
			})
		if res.Error != nil {
			if isDuplicate(res.Error) {
				return ErrDuplicate
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrConflict
		}

		// 替换标签关联：先删后插，避免逐条 diff
		if err := tx.Where("article_id = ?", a.ID).Delete(&model.ArticleTag{}).Error; err != nil {
			return err
		}
		if len(tagIDs) > 0 {
			rows := make([]model.ArticleTag, 0, len(tagIDs))
			for _, id := range tagIDs {
				rows = append(rows, model.ArticleTag{ArticleID: a.ID, TagID: id})
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
				return err
			}
		}
		return bumpCounters(tx, a, oldTagIDs, tagIDs)
	})
}

// ChangeStatus 在事务内做一次状态流转，并同步侧效字段。
// from 一并作为条件，保证并发下只有一次流转成功（CAS 语义）。
func (r *ArticleRepo) ChangeStatus(ctx context.Context, id uint64, from, to uint8, extra map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.Article
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if before.Status != from {
			return ErrConflict
		}

		updates := map[string]any{
			"status":             to,
			"search_sync_status": model.SearchSyncPending,
			"updated_at":         time.Now(),
		}
		if to == model.ArticlePublished && before.PublishedAt == nil {
			now := time.Now()
			updates["published_at"] = now
		}
		for k, v := range extra {
			updates[k] = v
		}
		if err := tx.Model(&model.Article{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}

		// 分类与标签计数只在"进入/离开公开可见"时变化。
		// 漏掉标签这一半，会导致发布不增、下架不减，计数长期漂移。
		beforePublic := before.Status == model.ArticlePublished && before.DeletedAt == nil
		afterPublic := to == model.ArticlePublished
		if beforePublic != afterPublic {
			delta := 1
			if !afterPublic {
				delta = -1
			}
			if err := shiftCategoryCount(tx, before.CategoryID, delta); err != nil {
				return err
			}
			tagIDs, err := r.TagIDsTx(tx, id)
			if err != nil {
				return err
			}
			if err := shiftTagCounts(tx, tagIDs, delta); err != nil {
				return err
			}
		}
		return nil
	})
}

// SoftDelete 软删除，并递减公开可见时的分类与标签计数。
func (r *ArticleRepo) SoftDelete(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a model.Article
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if a.DeletedAt != nil {
			return ErrNotFound
		}
		if err := tx.Model(&model.Article{}).Where("id = ?", id).Updates(map[string]any{
			"status":             model.ArticleDeleted,
			"deleted_at":         at,
			"search_sync_status": model.SearchSyncPending,
			"updated_at":         at,
		}).Error; err != nil {
			return err
		}
		if a.Status == model.ArticlePublished {
			if err := shiftCategoryCount(tx, a.CategoryID, -1); err != nil {
				return err
			}
			tagIDs, err := r.TagIDsTx(tx, id)
			if err != nil {
				return err
			}
			if err := shiftTagCounts(tx, tagIDs, -1); err != nil {
				return err
			}
		}
		return nil
	})
}

// ByID 按主键读取（含已删除），调用方按状态决定是否可见。
func (r *ArticleRepo) ByID(ctx context.Context, id uint64) (*model.Article, error) {
	var a model.Article
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// BySlug 按 slug 读取。
func (r *ArticleRepo) BySlug(ctx context.Context, slug string) (*model.Article, error) {
	var a model.Article
	err := r.db.WithContext(ctx).Where("slug = ?", slug).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SlugExists 判断 slug 是否已被占用（excludeID 用于更新时排除自身）。
func (r *ArticleRepo) SlugExists(ctx context.Context, slug string, excludeID uint64) (bool, error) {
	q := r.db.WithContext(ctx).Model(&model.Article{}).Where("slug = ?", slug)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// List 查询文章列表。limit 由调用方多取一条用于判断 has_more。
//
// 列表查询**不 SELECT 正文**：一页 20 篇正文足以让单次查询传输几十 MB。
func (r *ArticleRepo) List(ctx context.Context, f ListFilter) ([]model.Article, error) {
	q := r.db.WithContext(ctx).Model(&model.Article{}).
		Select("id, author_id, category_id, title, slug, summary, cover_url, status, is_top, allow_comment, " +
			"view_count, like_count, favorite_count, comment_count, word_count, reading_time, " +
			"version, reject_reason, published_at, created_at, updated_at, deleted_at")

	if f.AuthorID > 0 {
		q = q.Where("author_id = ?", f.AuthorID)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	} else if f.AuthorID == 0 {
		// 公开列表固定只看已发布、未删除
		q = q.Where("status = ? AND deleted_at IS NULL", model.ArticlePublished)
	}
	if f.CategoryID != nil {
		q = q.Where("category_id = ?", *f.CategoryID)
	}
	if f.TagID > 0 {
		q = q.Where("id IN (SELECT article_id FROM article_tags WHERE tag_id = ?)", f.TagID)
	}

	switch f.Sort {
	case "hot":
		if f.CursorID > 0 {
			q = q.Where("(view_count < ? OR (view_count = ? AND id < ?))", f.CursorView, f.CursorView, f.CursorID)
		}
		q = q.Order("view_count DESC, id DESC")
		if f.AuthorID == 0 {
			q = q.Where("published_at >= ?", time.Now().AddDate(0, 0, -30))
		}
	default: // latest
		if f.AuthorID > 0 {
			// 作者后台按更新时间排序：草稿没有发布时间，不能用 published_at 做游标
			if f.CursorID > 0 && f.CursorTime != nil {
				q = q.Where("(updated_at < ? OR (updated_at = ? AND id < ?))", *f.CursorTime, *f.CursorTime, f.CursorID)
			}
			q = q.Order("updated_at DESC, id DESC")
		} else {
			if f.CursorID > 0 && f.CursorTime != nil {
				q = q.Where("(published_at < ? OR (published_at = ? AND id < ?))", *f.CursorTime, *f.CursorTime, f.CursorID)
			}
			q = q.Order("published_at DESC, id DESC")
		}
	}

	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var out []model.Article
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// TagIDs 返回文章的标签 ID 列表。
func (r *ArticleRepo) TagIDs(ctx context.Context, articleID uint64) ([]uint64, error) {
	return r.TagIDsTx(r.db.WithContext(ctx), articleID)
}

// TagIDsTx 在给定事务/连接上查询标签 ID。
func (r *ArticleRepo) TagIDsTx(db *gorm.DB, articleID uint64) ([]uint64, error) {
	var ids []uint64
	err := db.Model(&model.ArticleTag{}).Where("article_id = ?", articleID).Pluck("tag_id", &ids).Error
	return ids, err
}

// TagsByArticleIDs 批量查询标签，避免 N+1。
func (r *ArticleRepo) TagsByArticleIDs(ctx context.Context, ids []uint64) (map[uint64][]model.Tag, error) {
	out := make(map[uint64][]model.Tag, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ArticleID uint64
		model.Tag
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("article_tags AS at").
		Select("at.article_id, t.id, t.name, t.slug, t.article_count, t.created_at").
		Joins("JOIN tags t ON t.id = at.tag_id").
		Where("at.article_id IN ?", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, rw := range rows {
		out[rw.ArticleID] = append(out[rw.ArticleID], rw.Tag)
	}
	return out, nil
}

// UsersByIDs 批量查作者基本信息。
func (r *ArticleRepo) UsersByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error) {
	out := make(map[uint64]model.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var users []model.User
	if err := r.db.WithContext(ctx).Select("id, username, nickname, avatar_url").
		Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = u
	}
	return out, nil
}

// CategoriesByIDs 批量查分类。
func (r *ArticleRepo) CategoriesByIDs(ctx context.Context, ids []uint64) (map[uint64]model.Category, error) {
	out := make(map[uint64]model.Category, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var cats []model.Category
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&cats).Error; err != nil {
		return nil, err
	}
	for _, c := range cats {
		out[c.ID] = c
	}
	return out, nil
}

// ListCategories 返回全部分类。
func (r *ArticleRepo) ListCategories(ctx context.Context) ([]model.Category, error) {
	var out []model.Category
	err := r.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&out).Error
	return out, err
}

// ListTags 返回文章数最多的标签。
func (r *ArticleRepo) ListTags(ctx context.Context, limit int) ([]model.Tag, error) {
	var out []model.Tag
	err := r.db.WithContext(ctx).Order("article_count DESC, id ASC").Limit(limit).Find(&out).Error
	return out, err
}

// TagsByIDs 校验标签是否存在，返回有效 ID 集合。
func (r *ArticleRepo) TagsByIDs(ctx context.Context, ids []uint64) (map[uint64]bool, error) {
	out := make(map[uint64]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var found []uint64
	if err := r.db.WithContext(ctx).Model(&model.Tag{}).Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return nil, err
	}
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}

// CategoryExists 判断分类是否存在（category_id=0 表示未分类，不算分类）。
func (r *ArticleRepo) CategoryExists(ctx context.Context, id uint64) (bool, error) {
	if id == 0 {
		return true, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

// ---- 内部工具 ----

// bumpCounters 维护分类与标签计数：只统计"公开可见"的文章。
func bumpCounters(tx *gorm.DB, a *model.Article, oldTags, newTags []uint64) error {
	// 草稿/待审的文章不计入任何公开计数
	if a.Status != model.ArticlePublished {
		return nil
	}
	if err := shiftCategoryCount(tx, a.CategoryID, 1); err != nil {
		return err
	}
	for _, tid := range diff(newTags, oldTags) {
		if err := tx.Model(&model.Tag{}).Where("id = ?", tid).
			UpdateColumn("article_count", gorm.Expr("article_count + 1")).Error; err != nil {
			return err
		}
	}
	for _, tid := range diff(oldTags, newTags) {
		if err := tx.Model(&model.Tag{}).Where("id = ? AND article_count > 0", tid).
			UpdateColumn("article_count", gorm.Expr("article_count - 1")).Error; err != nil {
			return err
		}
	}
	return nil
}

// shiftTagCounts 批量调整标签计数。delta 为 ±1，负数时防止减到负值。
func shiftTagCounts(tx *gorm.DB, tagIDs []uint64, delta int) error {
	if len(tagIDs) == 0 || delta == 0 {
		return nil
	}
	for _, tid := range tagIDs {
		var res *gorm.DB
		if delta > 0 {
			res = tx.Model(&model.Tag{}).Where("id = ?", tid).
				UpdateColumn("article_count", gorm.Expr("article_count + ?", delta))
		} else {
			res = tx.Model(&model.Tag{}).Where("id = ? AND article_count > 0", tid).
				UpdateColumn("article_count", gorm.Expr("article_count + ?", delta))
		}
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

// shiftCategoryCount 调整分类计数，category_id=0（未分类）不计数。
func shiftCategoryCount(tx *gorm.DB, categoryID uint64, delta int) error {
	if categoryID == 0 {
		return nil
	}
	expr := gorm.Expr("article_count + ?", delta)
	if delta < 0 {
		return tx.Model(&model.Category{}).
			Where("id = ? AND article_count > 0", categoryID).
			UpdateColumn("article_count", gorm.Expr("article_count - 1")).Error
	}
	return tx.Model(&model.Category{}).Where("id = ?", categoryID).UpdateColumn("article_count", expr).Error
}

// diff 返回在 a 中但不在 b 中的元素。
func diff(a, b []uint64) []uint64 {
	set := make(map[uint64]struct{}, len(b))
	for _, v := range b {
		set[v] = struct{}{}
	}
	out := make([]uint64, 0, len(a))
	for _, v := range a {
		if _, ok := set[v]; !ok {
			out = append(out, v)
		}
	}
	return out
}

// ByIDs 按 ID 集合批量取文章，保持任意顺序（由调用方排序）。
func (r *ArticleRepo) ByIDs(ctx context.Context, ids []uint64) ([]model.Article, error) {
	if len(ids) == 0 {
		return []model.Article{}, nil
	}
	var rows []model.Article
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error
	return rows, err
}
