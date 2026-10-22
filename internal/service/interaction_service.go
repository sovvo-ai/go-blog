package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// InteractionStore 是互动服务所需的数据能力。
type InteractionStore interface {
	Like(ctx context.Context, userID, articleID uint64) (bool, uint32, error)
	Unlike(ctx context.Context, userID, articleID uint64) (bool, uint32, error)
	Liked(ctx context.Context, userID, articleID uint64) (bool, error)
	Favorite(ctx context.Context, userID, articleID uint64) (bool, uint32, error)
	Unfavorite(ctx context.Context, userID, articleID uint64) (bool, uint32, error)
	Favorited(ctx context.Context, userID, articleID uint64) (bool, error)
	FavoritePage(ctx context.Context, userID uint64, after *repository.FavoriteRow, limit int) ([]repository.FavoriteRow, error)
}

// InteractionArticles 是互动服务需要的文章能力（精简接口，便于测试替换）。
type InteractionArticles interface {
	// ByID 读取文章，供互动前的状态校验。
	ByID(ctx context.Context, id uint64) (*model.Article, error)
	// ListByIDs 按 ID 集合返回公开文章视图（保持顺序），供收藏列表组装。
	ListByIDs(ctx context.Context, ids []uint64) ([]*ArticleView, error)
}

// InteractionService 实现点赞、收藏等互动。
type InteractionService struct {
	store    InteractionStore
	articles InteractionArticles
	log      *slog.Logger
}

// NewInteractionService 构造 InteractionService。
func NewInteractionService(store InteractionStore, articles InteractionArticles, log *slog.Logger) *InteractionService {
	return &InteractionService{store: store, articles: articles, log: log}
}

// ToggleLike 点赞（幂等）。
func (s *InteractionService) ToggleLike(ctx context.Context, userID, articleID uint64, like bool) (bool, uint32, error) {
	if err := s.assertCommentable(ctx, articleID); err != nil {
		return false, 0, err
	}
	if like {
		return s.store.Like(ctx, userID, articleID)
	}
	return s.store.Unlike(ctx, userID, articleID)
}

// ToggleFavorite 收藏（幂等）。
func (s *InteractionService) ToggleFavorite(ctx context.Context, userID, articleID uint64, fav bool) (bool, uint32, error) {
	if err := s.assertCommentable(ctx, articleID); err != nil {
		return false, 0, err
	}
	if fav {
		return s.store.Favorite(ctx, userID, articleID)
	}
	return s.store.Unfavorite(ctx, userID, articleID)
}

// ListFavorites 分页返回收藏列表。
func (s *InteractionService) ListFavorites(ctx context.Context, userID uint64, cursor string, limit int) ([]*ArticleView, string, bool, error) {
	after, err := decodeFavoriteCursor(cursor)
	if err != nil {
		return nil, "", false, badRequest("无效的分页游标", "cursor")
	}
	rows, err := s.store.FavoritePage(ctx, userID, after, limit)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	ids := make([]uint64, len(rows))
	for i, r := range rows {
		ids[i] = r.ArticleID
	}
	views, err := s.articles.ListByIDs(ctx, ids)
	if err != nil {
		return nil, "", false, err
	}

	var next string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = encodeFavoriteCursor(last)
	}
	return views, next, hasMore, nil
}

// assertCommentable 校验文章存在且处于可互动状态（published 且未删）。
func (s *InteractionService) assertCommentable(ctx context.Context, articleID uint64) error {
	a, err := s.articles.ByID(ctx, articleID)
	if err != nil {
		if err == repository.ErrNotFound {
			return errs.New(errs.CodeArticleNotFound, 404, "文章不存在")
		}
		return errs.Internal(err)
	}
	if a.Status != model.ArticlePublished || a.DeletedAt != nil {
		return errs.New(errs.CodeInvalidArticleState, 400, "文章不可互动")
	}
	return nil
}

// encodeFavoriteCursor 编码收藏列表游标。
func encodeFavoriteCursor(r repository.FavoriteRow) string {
	raw := fmt.Sprintf("f:%d:%d", r.CreatedAt.UnixMilli(), r.ArticleID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeFavoriteCursor 解析收藏游标；空串表示第一页。
func decodeFavoriteCursor(cursor string) (*repository.FavoriteRow, error) {
	if strings.TrimSpace(cursor) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "f" {
		return nil, fmt.Errorf("游标格式非法")
	}
	ms, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil, err
	}
	return &repository.FavoriteRow{ArticleID: id, CreatedAt: time.UnixMilli(ms)}, nil
}
