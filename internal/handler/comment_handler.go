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

// CommentHandler 处理评论接口。
type CommentHandler struct {
	svc *service.CommentService
}

// NewCommentHandler 构造 CommentHandler。
func NewCommentHandler(svc *service.CommentService) *CommentHandler {
	return &CommentHandler{svc: svc}
}

// List 处理 GET /articles/{id}/comments。
//
// root_id 参数决定返回主楼列表还是某主楼下的回复。
func (h *CommentHandler) List(c *gin.Context) {
	articleID := parseUintPath(c.Param("id"))
	if articleID == 0 {
		response.Error(c, errs.BadRequest("无效的文章 ID"))
		return
	}
	limit := queryInt(c, "limit", 20)
	viewerID := middleware.UIDOf(c)

	var items []*service.CommentView
	var next string
	var hasMore bool
	var err error

	rootRaw := c.Query("root_id")
	if rootRaw != "" && rootRaw != "0" {
		rootID := parseUintPath(rootRaw)
		if rootID == 0 {
			response.Error(c, errs.BadRequest("无效的 root_id"))
			return
		}
		items, next, hasMore, err = h.svc.ListReplies(c.Request.Context(), rootID, viewerID, c.Query("cursor"), limit)
	} else {
		items, next, hasMore, err = h.svc.List(c.Request.Context(), articleID, viewerID, c.Query("cursor"), limit)
	}
	if err != nil {
		response.Error(c, err)
		return
	}

	out := make([]dto.CommentItem, 0, len(items))
	for _, v := range items {
		out = append(out, toCommentItem(v))
	}
	response.OK(c, dto.CommentListData{
		Items:      out,
		NextCursor: nullString(next),
		HasMore:    hasMore,
	})
}

// Create 处理 POST /articles/{id}/comments。
func (h *CommentHandler) Create(c *gin.Context) {
	articleID := parseUintPath(c.Param("id"))
	if articleID == 0 {
		response.Error(c, errs.BadRequest("无效的文章 ID"))
		return
	}
	var req dto.CreateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误").WithData(fieldErrorsData("content", "请填写评论内容")))
		return
	}

	parentID := parseUintPath(derefString(req.ParentId))
	replyTo := parseUintPath(derefString(req.ReplyToUserId))

	comment, err := h.svc.Create(c.Request.Context(), middleware.UIDOf(c), articleID, service.CommentInput{
		Content:       req.Content,
		ParentID:      parentID,
		ReplyToUserID: replyTo,
		IP:            c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.CommentMutationData{
		Id:     uidString(comment.ID),
		Status: commentMutationStatusName(comment.Status),
	})
}

// Delete 处理 DELETE /comments/{id}。
func (h *CommentHandler) Delete(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的评论 ID"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, middleware.UIDOf(c), middleware.RoleOf(c)); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

func toCommentItem(v *service.CommentView) dto.CommentItem {
	item := dto.CommentItem{
		Id:         uidString(v.Comment.ID),
		User:       toAuthorBrief(v.Author),
		Content:    v.Comment.Content,
		Status:     commentStatusName(v.Comment.Status),
		ReplyCount: v.ReplyCount,
		CreatedAt:  v.Comment.CreatedAt.UTC(),
	}
	if v.ReplyTo != nil {
		brief := toAuthorBrief(*v.ReplyTo)
		item.ReplyToUser = &brief
	}
	if len(v.Replies) > 0 {
		replies := make([]dto.CommentItem, 0, len(v.Replies))
		for _, r := range v.Replies {
			replies = append(replies, toCommentItem(r))
		}
		item.Replies = &replies
	}
	return item
}

func commentStatusName(status uint8) dto.CommentItemStatus {
	switch status {
	case model.CommentApproved:
		return dto.CommentItemStatusApproved
	case model.CommentRejected:
		return dto.CommentItemStatusRejected
	default:
		return dto.CommentItemStatusPending
	}
}

func commentMutationStatusName(status uint8) dto.CommentMutationDataStatus {
	if status == model.CommentApproved {
		return dto.CommentMutationDataStatusApproved
	}
	return dto.CommentMutationDataStatusPending
}
