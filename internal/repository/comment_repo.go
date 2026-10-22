package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-blog/internal/model"
)

// CreateComment 在事务内插入评论；countUp 为 true 时文章评论数 +1。
//
// 计数更新显式写回 updated_at，避免触发 ON UPDATE 影响对账判断。
func (r *InteractionRepo) CreateComment(ctx context.Context, c *model.Comment, countUp bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		if countUp {
			return shiftArticleCount(tx, c.ArticleID, "comment_count", 1)
		}
		return nil
	})
}

// CommentByID 按主键取评论。
func (r *InteractionRepo) CommentByID(ctx context.Context, id uint64) (*model.Comment, error) {
	var c model.Comment
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// commentVisibility 是评论可见性条件：
// 已通过（approved）的评论人人可见；pending/rejected 仅评论者本人可见。
func commentVisibility(viewerID uint64) func(*gorm.DB) *gorm.DB {
	return func(q *gorm.DB) *gorm.DB {
		// 注意：不能写 []uint8{...}，GORM 会把 []uint8 当作 []byte（二进制）
		// 序列化成 BLOB，导致 status IN 语法错误。必须用 []int。
		return q.Where(
			"deleted_at IS NULL AND (status = ? OR (user_id = ? AND status IN ?))",
			model.CommentApproved, viewerID, []int{int(model.CommentPending), int(model.CommentRejected)},
		)
	}
}

// RootComments 分页返回主楼（root_id=0），按 ID 倒序（新楼在前）。
func (r *InteractionRepo) RootComments(ctx context.Context, articleID, viewerID uint64, cursorID uint64, limit int) ([]model.Comment, error) {
	var rows []model.Comment
	q := r.db.WithContext(ctx).Model(&model.Comment{}).
		Scopes(commentVisibility(viewerID)).
		Where("article_id = ? AND root_id = 0", articleID)
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// RepliesByRoots 批量取多个主楼各自的前 perRoot 条回复（按 ID 正序）。
func (r *InteractionRepo) RepliesByRoots(ctx context.Context, rootIDs []uint64, viewerID uint64, perRoot int) (map[uint64][]model.Comment, error) {
	out := make(map[uint64][]model.Comment, len(rootIDs))
	if len(rootIDs) == 0 {
		return out, nil
	}
	var rows []model.Comment
	err := r.db.WithContext(ctx).Model(&model.Comment{}).
		Scopes(commentVisibility(viewerID)).
		Where("root_id IN ? AND root_id <> 0", rootIDs).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if len(out[rows[i].RootID]) < perRoot {
			out[rows[i].RootID] = append(out[rows[i].RootID], rows[i])
		}
	}
	return out, nil
}

// ListReplies 分页返回某主楼下的回复，按 ID 正序。
func (r *InteractionRepo) ListReplies(ctx context.Context, rootID, viewerID uint64, cursorID uint64, limit int) ([]model.Comment, error) {
	var rows []model.Comment
	q := r.db.WithContext(ctx).Model(&model.Comment{}).
		Scopes(commentVisibility(viewerID)).
		Where("root_id = ? AND root_id <> 0", rootID)
	if cursorID > 0 {
		q = q.Where("id > ?", cursorID)
	}
	err := q.Order("id ASC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// SoftDeleteComment 软删除评论。
//
// 主楼删除时级联软删其下所有回复；返回被删除的 approved 条数，
// 供 article.comment_count 递减。
func (r *InteractionRepo) SoftDeleteComment(ctx context.Context, id uint64) (int, error) {
	var removed int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Comment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&c).Error; err != nil {
			return err
		}
		if c.DeletedAt != nil {
			// 已删除，幂等返回 0
			return nil
		}

		now := time.Now()

		// 必须先统计、再 UPDATE：UPDATE 会把回复一起改成 deleted，
		// 若先删再统计，approved 回复数会被算成 0，计数只减主楼一条。
		if c.Status == model.CommentApproved {
			removed = 1
		}
		if c.RootID == 0 {
			var n int64
			if err := tx.Model(&model.Comment{}).
				Where("root_id = ? AND status = ? AND deleted_at IS NULL", id, model.CommentApproved).
				Count(&n).Error; err != nil {
				return err
			}
			removed += int(n)
		}

		// 主楼：连带回复一起删；回复：只删自身
		cond := tx.Model(&model.Comment{}).Where("id = ?", id)
		if c.RootID == 0 {
			cond = tx.Model(&model.Comment{}).Where("id = ? OR root_id = ?", id, id)
		}
		res := cond.Updates(map[string]any{
			"status":     model.CommentDeleted,
			"deleted_at": now,
		})
		if res.Error != nil {
			return res.Error
		}

		if removed > 0 {
			return shiftArticleCount(tx, c.ArticleID, "comment_count", -removed)
		}
		return nil
	})
	return removed, err
}

// ApproveComment 审核通过（pending → approved），评论数 +1。
func (r *InteractionRepo) ApproveComment(ctx context.Context, id uint64) (*model.Comment, error) {
	var c model.Comment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&c).Error; err != nil {
			return err
		}
		if c.Status != model.CommentPending {
			return ErrConflict
		}
		if err := tx.Model(&model.Comment{}).Where("id = ?", id).
			Update("status", model.CommentApproved).Error; err != nil {
			return err
		}
		c.Status = model.CommentApproved
		return shiftArticleCount(tx, c.ArticleID, "comment_count", 1)
	})
	return &c, err
}

// RejectComment 审核拒绝（pending → rejected），不改变计数。
func (r *InteractionRepo) RejectComment(ctx context.Context, id uint64) (*model.Comment, error) {
	var c model.Comment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&c).Error; err != nil {
			return err
		}
		if c.Status != model.CommentPending {
			return ErrConflict
		}
		if err := tx.Model(&model.Comment{}).Where("id = ?", id).
			Update("status", model.CommentRejected).Error; err != nil {
			return err
		}
		c.Status = model.CommentRejected
		return nil
	})
	return &c, err
}
