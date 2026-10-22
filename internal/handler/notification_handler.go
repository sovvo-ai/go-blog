package handler

import (
	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// NotificationHandler 处理站内通知接口。
type NotificationHandler struct {
	svc       *service.NotificationService
	reportSvc *service.ReportService
}

// NewNotificationHandler 构造 NotificationHandler。
func NewNotificationHandler(svc *service.NotificationService, reportSvc *service.ReportService) *NotificationHandler {
	return &NotificationHandler{svc: svc, reportSvc: reportSvc}
}

// List 处理 GET /notifications。
func (h *NotificationHandler) List(c *gin.Context) {
	unreadOnly := c.Query("unread_only") == "true"
	limit := queryInt(c, "limit", 20)
	rows, next, hasMore, err := h.svc.List(c.Request.Context(), middleware.UIDOf(c), unreadOnly, c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.NotificationItem, 0, len(rows))
	for _, n := range rows {
		items = append(items, toNotificationItem(n))
	}
	response.OK(c, dto.NotificationListData{
		Items:      items,
		NextCursor: nullString(next),
		HasMore:    hasMore,
	})
}

// UnreadCount 处理 GET /notifications/unread-count。
func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	n, err := h.svc.UnreadCount(c.Request.Context(), middleware.UIDOf(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.UnreadCountData{Count: int(n)})
}

// MarkRead 处理 PUT /notifications/{id}/read。
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的通知 ID"))
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), middleware.UIDOf(c), id); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// Report 处理 POST /reports。
func (h *NotificationHandler) Report(c *gin.Context) {
	var req dto.ReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	id, duplicate, err := h.reportSvc.Create(c.Request.Context(), middleware.UIDOf(c), service.ReportInput{
		TargetType: string(req.TargetType),
		TargetID:   parseUintPath(req.TargetId),
		Reason:     string(req.Reason),
		Detail:     derefString(req.Detail),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	dup := duplicate
	response.Created(c, dto.ReportData{
		Id:        uidString(id),
		Duplicate: &dup,
	})
}

func toNotificationItem(n model.Notification) dto.NotificationItem {
	item := dto.NotificationItem{
		Id:        uidString(n.ID),
		Type:      n.Type,
		Title:     n.Title,
		Content:   n.Content,
		Link:      nullString(n.Link),
		IsRead:    n.IsRead == 1,
		CreatedAt: n.CreatedAt.UTC(),
	}
	if n.RefID != nil {
		item.RefId = ptrString(uidString(*n.RefID))
	}
	return item
}

// MarkAllRead 处理 PUT /notifications/read-all。
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	n, err := h.svc.MarkAllRead(c.Request.Context(), middleware.UIDOf(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.NotificationReadAllData{Updated: int(n)})
}
