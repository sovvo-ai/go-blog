package handler

import (
	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// InteractionHandler 处理点赞、收藏等互动接口。
type InteractionHandler struct {
	svc *service.InteractionService
}

// NewInteractionHandler 构造 InteractionHandler。
func NewInteractionHandler(svc *service.InteractionService) *InteractionHandler {
	return &InteractionHandler{svc: svc}
}

// Like 处理 POST /articles/{id}/like。
func (h *InteractionHandler) Like(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	applied, count, err := h.svc.ToggleLike(c.Request.Context(), middleware.UIDOf(c), id, true)
	if err != nil {
		response.Error(c, err)
		return
	}
	_ = applied
	response.OK(c, dto.LikeData{Liked: true, LikeCount: int(count)})
}

// Unlike 处理 DELETE /articles/{id}/like。
func (h *InteractionHandler) Unlike(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	_, count, err := h.svc.ToggleLike(c.Request.Context(), middleware.UIDOf(c), id, false)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.LikeData{Liked: false, LikeCount: int(count)})
}

// Favorite 处理 POST /articles/{id}/favorite。
func (h *InteractionHandler) Favorite(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	_, count, err := h.svc.ToggleFavorite(c.Request.Context(), middleware.UIDOf(c), id, true)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.FavoriteData{Favorited: true, FavoriteCount: int(count)})
}

// Unfavorite 处理 DELETE /articles/{id}/favorite。
func (h *InteractionHandler) Unfavorite(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	_, count, err := h.svc.ToggleFavorite(c.Request.Context(), middleware.UIDOf(c), id, false)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.FavoriteData{Favorited: false, FavoriteCount: int(count)})
}

// MyFavorites 处理 GET /users/me/favorites。
func (h *InteractionHandler) MyFavorites(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	views, next, hasMore, err := h.svc.ListFavorites(c.Request.Context(), middleware.UIDOf(c), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.ArticleBrief, 0, len(views))
	for _, v := range views {
		items = append(items, toArticleBrief(v))
	}
	response.OK(c, dto.FavoriteListData{
		Items:      items,
		NextCursor: nullString(next),
		HasMore:    hasMore,
	})
}
