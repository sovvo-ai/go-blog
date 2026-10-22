package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-blog/internal/model"
)

// ---- 评论管理（管理端）----

// ListCommentsAdmin 管理端评论列表：可看全部状态（含已删）。
func (r *InteractionRepo) ListCommentsAdmin(ctx context.Context, status *uint8, articleID uint64, cursorID uint64, limit int) ([]model.Comment, error) {
	var rows []model.Comment
	q := r.db.WithContext(ctx).Model(&model.Comment{})
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if articleID > 0 {
		q = q.Where("article_id = ?", articleID)
	}
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// ReportByID 按主键取举报。
func (r *InteractionRepo) ReportByID(ctx context.Context, id uint64) (*model.Report, error) {
	var rep model.Report
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&rep).Error
	if err != nil {
		return nil, err
	}
	return &rep, nil
}

// ---- 分类与标签管理 ----

// CreateCategory 创建分类；slug 冲突返回 ErrDuplicate。
func (r *AdminRepo) CreateCategory(ctx context.Context, c *model.Category) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// UpdateCategory 更新分类。
func (r *AdminRepo) UpdateCategory(ctx context.Context, c *model.Category) error {
	res := r.db.WithContext(ctx).Model(&model.Category{}).
		Where("id = ?", c.ID).
		Updates(map[string]any{
			"name":        c.Name,
			"slug":        c.Slug,
			"description": c.Description,
			"sort":        c.Sort,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory 删除分类，其下文章 category_id 置 0（未分类）。
// 计数字段随分类一起消失，无需维护。
func (r *AdminRepo) DeleteCategory(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&model.Category{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Model(&model.Article{}).Where("category_id = ?", id).
			Update("category_id", 0).Error
	})
}

// CreateTag 创建标签。
func (r *AdminRepo) CreateTag(ctx context.Context, t *model.Tag) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// UpdateTag 更新标签。
func (r *AdminRepo) UpdateTag(ctx context.Context, t *model.Tag) error {
	res := r.db.WithContext(ctx).Model(&model.Tag{}).
		Where("id = ?", t.ID).
		Updates(map[string]any{"name": t.Name, "slug": t.Slug})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTag 删除标签及其文章关联。
func (r *AdminRepo) DeleteTag(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&model.Tag{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("tag_id = ?", id).Delete(&model.ArticleTag{}).Error
	})
}

// MergeTags 把 from 标签的所有文章关联迁到 to，删除 from，重算 to 的计数。
//
// 幂等关键：迁移用 INSERT IGNORE，from/to 已同时标注的文章不会重复。
func (r *AdminRepo) MergeTags(ctx context.Context, fromID, toID uint64) error {
	if fromID == toID {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 确认两个标签都存在
		var from, to model.Tag
		if err := tx.Where("id = ?", fromID).Take(&from).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", toID).Take(&to).Error; err != nil {
			return err
		}

		// 迁移关联：为 from 的每篇文章补一条 to 关联（已存在则跳过）
		var articleIDs []uint64
		if err := tx.Model(&model.ArticleTag{}).Where("tag_id = ?", fromID).
			Pluck("article_id", &articleIDs).Error; err != nil {
			return err
		}
		if len(articleIDs) > 0 {
			rows := make([]model.ArticleTag, 0, len(articleIDs))
			for _, aid := range articleIDs {
				rows = append(rows, model.ArticleTag{ArticleID: aid, TagID: toID})
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
				return err
			}
		}

		// 删除 from 标签及其关联
		if err := tx.Where("id = ?", fromID).Delete(&model.Tag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tag_id = ?", fromID).Delete(&model.ArticleTag{}).Error; err != nil {
			return err
		}

		// 重算 to 的计数：只统计公开可见的文章（与 bumpCounters 口径一致）
		return tx.Exec(
			"UPDATE tags t SET article_count = ("+
				"SELECT COUNT(*) FROM article_tags at "+
				"JOIN articles a ON a.id = at.article_id "+
				"WHERE at.tag_id = t.id AND a.status = 2 AND a.deleted_at IS NULL"+
				") WHERE t.id = ?", toID).Error
	})
}

// ---- 操作日志 ----

// LogFilter 是操作日志筛选条件。
type LogFilter struct {
	Action     string
	TargetType string
	CursorID   uint64
	Limit      int
}

// ListLogs 分页返回操作日志。
func (r *AdminRepo) ListLogs(ctx context.Context, f LogFilter) ([]model.OperationLog, error) {
	var rows []model.OperationLog
	q := r.db.WithContext(ctx).Model(&model.OperationLog{})
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.TargetType != "" {
		q = q.Where("target_type = ?", f.TargetType)
	}
	if f.CursorID > 0 {
		q = q.Where("id < ?", f.CursorID)
	}
	err := q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error
	return rows, err
}

// ---- 索引运维 ----

// IndexFailure 是索引同步失败的文章。
type IndexFailure struct {
	ID       uint64
	Title    string
	Status   uint8
	SyncedAt *time.Time
}

// ListIndexFailures 返回同步失败（status=2）的文章。
func (r *AdminRepo) ListIndexFailures(ctx context.Context, limit int) ([]model.Article, error) {
	var rows []model.Article
	err := r.db.WithContext(ctx).
		Select("id", "title", "status", "search_synced_at").
		Where("search_sync_status = ?", model.SearchSyncFailed).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// RecalcCategoryCounts 按公开文章实况重算分类计数。
func (r *AdminRepo) RecalcCategoryCounts(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE categories c SET article_count = (" +
			"SELECT COUNT(*) FROM articles a " +
			"WHERE a.category_id = c.id AND a.status = 2 AND a.deleted_at IS NULL)")
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// RecalcTagCounts 按公开文章实况重算标签计数。
func (r *AdminRepo) RecalcTagCounts(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		"UPDATE tags t SET article_count = (" +
			"SELECT COUNT(*) FROM article_tags at " +
			"JOIN articles a ON a.id = at.article_id " +
			"WHERE at.tag_id = t.id AND a.status = 2 AND a.deleted_at IS NULL)")
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
