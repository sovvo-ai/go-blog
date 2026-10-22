package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-blog/internal/model"
)

// InteractionRepo 负责点赞、收藏、评论、举报、通知的数据访问。
type InteractionRepo struct {
	db *gorm.DB
}

// NewInteractionRepo 构造 InteractionRepo。
func NewInteractionRepo(db *gorm.DB) *InteractionRepo {
	return &InteractionRepo{db: db}
}

// Like 点赞。返回 (是否本次生效, 最新计数)。
//
// 幂等靠联合主键 + INSERT IGNORE：重复点赞 affectedRows==0，不重复加计数。
// 计数更新与记录插入在同一事务内，保证"记录数与计数一致"。
func (r *InteractionRepo) Like(ctx context.Context, userID, articleID uint64) (bool, uint32, error) {
	var applied bool
	var count uint32
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"INSERT IGNORE INTO article_likes (user_id, article_id) VALUES (?, ?)",
			userID, articleID,
		)
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		if applied {
			if err := shiftArticleCount(tx, articleID, "like_count", 1); err != nil {
				return err
			}
		}
		return tx.Model(&model.Article{}).
			Where("id = ?", articleID).
			Pluck("like_count", &count).Error
	})
	return applied, count, err
}

// Unlike 取消点赞。返回 (是否本次生效, 最新计数)。
func (r *InteractionRepo) Unlike(ctx context.Context, userID, articleID uint64) (bool, uint32, error) {
	var applied bool
	var count uint32
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND article_id = ?", userID, articleID).
			Delete(&model.ArticleLike{})
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		if applied {
			if err := shiftArticleCount(tx, articleID, "like_count", -1); err != nil {
				return err
			}
		}
		return tx.Model(&model.Article{}).
			Where("id = ?", articleID).
			Pluck("like_count", &count).Error
	})
	return applied, count, err
}

// Liked 判断用户是否已点赞。
func (r *InteractionRepo) Liked(ctx context.Context, userID, articleID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ArticleLike{}).
		Where("user_id = ? AND article_id = ?", userID, articleID).
		Count(&n).Error
	return n > 0, err
}

// Favorite 收藏。
func (r *InteractionRepo) Favorite(ctx context.Context, userID, articleID uint64) (bool, uint32, error) {
	var applied bool
	var count uint32
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"INSERT IGNORE INTO article_favorites (user_id, article_id) VALUES (?, ?)",
			userID, articleID,
		)
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		if applied {
			if err := shiftArticleCount(tx, articleID, "favorite_count", 1); err != nil {
				return err
			}
		}
		return tx.Model(&model.Article{}).
			Where("id = ?", articleID).
			Pluck("favorite_count", &count).Error
	})
	return applied, count, err
}

// Unfavorite 取消收藏。
func (r *InteractionRepo) Unfavorite(ctx context.Context, userID, articleID uint64) (bool, uint32, error) {
	var applied bool
	var count uint32
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND article_id = ?", userID, articleID).
			Delete(&model.ArticleFavorite{})
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		if applied {
			if err := shiftArticleCount(tx, articleID, "favorite_count", -1); err != nil {
				return err
			}
		}
		return tx.Model(&model.Article{}).
			Where("id = ?", articleID).
			Pluck("favorite_count", &count).Error
	})
	return applied, count, err
}

// Favorited 判断用户是否已收藏。
func (r *InteractionRepo) Favorited(ctx context.Context, userID, articleID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ArticleFavorite{}).
		Where("user_id = ? AND article_id = ?", userID, articleID).
		Count(&n).Error
	return n > 0, err
}

// FavoriteRow 是收藏列表的一行。
type FavoriteRow struct {
	ArticleID uint64    `gorm:"column:article_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// FavoritePage 分页返回用户收藏的、仍公开可读的文章。
//
// 用 JOIN 一次性排除已删除/已下架的文章（PRD：我的收藏不返回死链）。
// 收藏表没有自增主键，游标用 (created_at, article_id) 复合，避免同秒收藏时丢数据。
func (r *InteractionRepo) FavoritePage(ctx context.Context, userID uint64, after *FavoriteRow, limit int) ([]FavoriteRow, error) {
	var rows []FavoriteRow
	q := r.db.WithContext(ctx).Model(&model.ArticleFavorite{}).
		Select("article_favorites.article_id", "article_favorites.created_at").
		Joins("JOIN articles ON articles.id = article_favorites.article_id").
		Where("article_favorites.user_id = ?", userID).
		Where("articles.status = ? AND articles.deleted_at IS NULL", model.ArticlePublished)
	if after != nil {
		q = q.Where(
			"(article_favorites.created_at < ? OR (article_favorites.created_at = ? AND article_favorites.article_id < ?))",
			after.CreatedAt, after.CreatedAt, after.ArticleID,
		)
	}
	err := q.Order("article_favorites.created_at DESC, article_favorites.article_id DESC").
		Limit(limit + 1).
		Find(&rows).Error
	return rows, err
}

// shiftArticleCount 原子调整文章的计数字段，负数时防止减到负值。
// 必须显式把 updated_at 设回原值：计数更新会触发 ON UPDATE，
// 否则会把无关写入误判为"内容变更"。
func shiftArticleCount(tx *gorm.DB, articleID uint64, column string, delta int) error {
	if delta >= 0 {
		return tx.Model(&model.Article{}).Where("id = ?", articleID).
			UpdateColumns(map[string]any{
				column:       gorm.Expr(column+" + ?", delta),
				"updated_at": gorm.Expr("updated_at"),
			}).Error
	}
	return tx.Model(&model.Article{}).
		Where("id = ? AND "+column+" > 0", articleID).
		UpdateColumns(map[string]any{
			column:       gorm.Expr(column+" + ?", delta),
			"updated_at": gorm.Expr("updated_at"),
		}).Error
}

// CreateNotification 写入站内通知。
//
// 唯一索引 (user_id, type, ref_id) 保证同类型同对象不重复通知；
// 冲突时静默忽略（通知重复比漏发更可接受，且语义上幂等）。
func (r *InteractionRepo) CreateNotification(ctx context.Context, n *model.Notification) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(n).Error
}

// ReportCreate 写入举报；唯一键冲突返回 (false, nil) 表示重复举报。
func (r *InteractionRepo) ReportCreate(ctx context.Context, rep *model.Report) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(rep)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
