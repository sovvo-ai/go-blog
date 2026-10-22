package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// SearchHandler 处理全文检索。
type SearchHandler struct {
	svc *service.SearchService
}

// NewSearchHandler 构造 SearchHandler。
func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// Search 处理 GET /search。
//
// 检索服务不可用时返回 200 + 空结果 + degraded=true：
// 搜索是增强能力，不该因为它挂了就让页面报错。
func (h *SearchHandler) Search(c *gin.Context) {
	page := queryInt(c, "page", 1)
	pageSize := queryInt(c, "page_size", 20)

	res, err := h.svc.Search(c.Request.Context(), c.Query("q"), page, pageSize)
	if err != nil {
		response.Error(c, err)
		return
	}

	items := make([]dto.SearchItem, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, dto.SearchItem{
			Id:          uidString(it.ID),
			Slug:        it.Slug,
			Title:       it.Title,
			Highlight:   ptrString(it.Highlight),
			Summary:     it.Summary,
			PublishedAt: it.PublishedAt,
		})
	}

	response.OK(c, dto.SearchData{
		Items:          items,
		Page:           res.Page,
		PageSize:       res.PageSize,
		EstimatedTotal: res.EstimatedTotal,
		HasMore:        res.HasMore,
		Degraded:       res.Degraded,
	})
}

// queryInt 读取整型查询参数，非法值回落默认值而不是报错。
func queryInt(c *gin.Context, key string, def int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
