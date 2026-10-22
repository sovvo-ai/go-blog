package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go-blog/internal/model"
	"go-blog/internal/pkg/markdown"
	"go-blog/internal/search"
)

// IndexDoc 组装单篇文章的索引文档。
//
// 第二个返回值表示"是否应存在于索引中"：只有 published 且未删除才为 true。
// 作者名、分类名、标签名在这里一次性取齐，避免 worker 再发多次查询。
func (r *ArticleRepo) IndexDoc(ctx context.Context, id uint64) (*search.Doc, bool, error) {
	var a model.Article
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&a).Error
	if err != nil {
		return nil, false, err
	}
	if a.Status != model.ArticlePublished || a.DeletedAt != nil {
		return nil, false, nil
	}

	doc := &search.Doc{
		ID:      a.ID,
		Title:   a.Title,
		Summary: a.Summary,
		// 索引存纯文本而非 Markdown 原文（TECH_DESIGN 9.1）：
		// 否则 `#`、`*`、代码块 ``` 等标记会被当成可检索内容命中。
		Content:     markdown.PlainText(a.ContentMD),
		AuthorID:    a.AuthorID,
		CategoryID:  a.CategoryID,
		PublishedAt: a.CreatedAt.Unix(),
	}
	if a.PublishedAt != nil {
		doc.PublishedAt = a.PublishedAt.Unix()
	}

	if users, err := r.UsersByIDs(ctx, []uint64{a.AuthorID}); err == nil {
		if u, ok := users[a.AuthorID]; ok {
			doc.AuthorName = u.Nickname
			if doc.AuthorName == "" {
				doc.AuthorName = u.Username
			}
		}
	}
	if a.CategoryID > 0 {
		if cats, err := r.CategoriesByIDs(ctx, []uint64{a.CategoryID}); err == nil {
			if c, ok := cats[a.CategoryID]; ok {
				doc.CategoryName = c.Name
			}
		}
	}
	tags, err := r.TagsByArticleIDs(ctx, []uint64{a.ID})
	if err == nil {
		for _, t := range tags[a.ID] {
			doc.TagIDs = append(doc.TagIDs, t.ID)
			doc.TagNames = append(doc.TagNames, t.Name)
		}
	}

	return doc, true, nil
}

// MarkSearchSynced 标记索引已同步。
//
// 必须显式写回 updated_at：计数类字段的 UPDATE 会触发 ON UPDATE，
// 若不固定，updated_at 会因无关写入而前移，导致对账任务反复判定"待同步"。
func (r *ArticleRepo) MarkSearchSynced(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"search_sync_status": model.SearchSyncDone,
			"search_synced_at":   at,
			"updated_at":         gorm.Expr("updated_at"),
		}).Error
}

// MarkSearchFailed 标记索引同步失败，等待对账任务重试。
func (r *ArticleRepo) MarkSearchFailed(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.Article{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"search_sync_status": model.SearchSyncFailed,
			"updated_at":         gorm.Expr("updated_at"),
		}).Error
}

// PendingSearchIDs 扫描"待同步"的文章 ID。
//
// 两个条件任一满足即视为待同步：
//   - 状态不是 done（pending / failed）
//   - 已同步但之后又被修改过（synced_at < updated_at）
//
// 第二个条件兜住"任务投递失败且状态写不进去"之外的漏网场景，
// 是 TECH_DESIGN 10.9 里"可重新发现的依据"。
func (r *ArticleRepo) PendingSearchIDs(ctx context.Context, limit int) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("search_sync_status <> ? OR (search_synced_at IS NOT NULL AND search_synced_at < updated_at)",
			model.SearchSyncDone).
		Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, err
}

// SamplePublishedForCheck 抽样取一批"声称已同步"的已发布文章，用于比对索引实际内容。
func (r *ArticleRepo) SamplePublishedForCheck(ctx context.Context, limit int) ([]model.Article, error) {
	var rows []model.Article
	err := r.db.WithContext(ctx).
		Select("id", "title").
		Where("status = ? AND deleted_at IS NULL AND search_sync_status = ?",
			model.ArticlePublished, model.SearchSyncDone).
		Order("RAND()").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// PublishedIDsPage 按 ID 顺序分页取已发布文章，用于全量重建。
func (r *ArticleRepo) PublishedIDsPage(ctx context.Context, afterID uint64, limit int) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("id > ? AND status = ? AND deleted_at IS NULL", afterID, model.ArticlePublished).
		Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, err
}

// PublishedBrief 是搜索结果回表所需的最小字段集。
type PublishedBrief struct {
	ID          uint64
	Slug        string
	Title       string
	Summary     string
	PublishedAt *time.Time
}

// PublishedBriefs 按 ID 批量取公开可读的文章摘要。
//
// 这是"搜索结果二次校验"的落地（TECH_DESIGN 9.4）：
// 索引是异步的，只有回表才能确定性排除已删除/已下架的文章。
func (r *ArticleRepo) PublishedBriefs(ctx context.Context, ids []uint64) (map[uint64]PublishedBrief, error) {
	out := make(map[uint64]PublishedBrief, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []PublishedBrief
	err := r.db.WithContext(ctx).Model(&model.Article{}).
		Select("id", "slug", "title", "summary", "published_at").
		Where("id IN ? AND status = ? AND deleted_at IS NULL", ids, model.ArticlePublished).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}
