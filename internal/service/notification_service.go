package service

import (
	"context"
	"errors"
	"log/slog"

	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// NotificationStore 是通知的读写能力（合并接口）。
type NotificationStore interface {
	CreateNotification(ctx context.Context, n *model.Notification) error
	ListNotifications(ctx context.Context, userID uint64, unreadOnly bool, cursorID uint64, limit int) ([]model.Notification, error)
	UnreadCount(ctx context.Context, userID uint64) (int64, error)
	MarkNotificationRead(ctx context.Context, id, userID uint64) (bool, error)
	MarkAllNotificationsRead(ctx context.Context, userID uint64) (int64, error)
}

// NotificationService 处理站内通知的读取与已读状态。
type NotificationService struct {
	store NotificationStore
	log   *slog.Logger
}

// NewNotificationService 构造 NotificationService。
func NewNotificationService(store NotificationStore, log *slog.Logger) *NotificationService {
	return &NotificationService{store: store, log: log}
}

// List 分页返回通知。
func (s *NotificationService) List(ctx context.Context, userID uint64, unreadOnly bool, cursor string, limit int) ([]model.Notification, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListNotifications(ctx, userID, unreadOnly, cursorID, limit)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	var next string
	if hasMore && len(rows) > 0 {
		next = encodeIDCursor(rows[len(rows)-1].ID)
	}
	return rows, next, hasMore, nil
}

// UnreadCount 返回未读数。
func (s *NotificationService) UnreadCount(ctx context.Context, userID uint64) (int64, error) {
	n, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		return 0, errs.Internal(err)
	}
	return n, nil
}

// MarkAllRead 标记全部未读为已读，返回更新条数。
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uint64) (int64, error) {
	n, err := s.store.MarkAllNotificationsRead(ctx, userID)
	if err != nil {
		return 0, errs.Internal(err)
	}
	return n, nil
}

// MarkRead 标记已读；非本人的通知返回 not found（不泄露存在性）。
func (s *NotificationService) MarkRead(ctx context.Context, userID, id uint64) error {
	hit, err := s.store.MarkNotificationRead(ctx, id, userID)
	if err != nil {
		return errs.Internal(err)
	}
	if !hit {
		return errs.NotFound("通知不存在")
	}
	return nil
}

// ReportStore 是举报的数据能力。
type ReportStore interface {
	ReportCreate(ctx context.Context, rep *model.Report) (bool, error)
}

// ReportInput 是举报入参。
type ReportInput struct {
	TargetType string
	TargetID   uint64
	Reason     string
	Detail     string
}

// ReportService 处理举报。
type ReportService struct {
	store    ReportStore
	articles CommentArticleStore
	comments CommentStore
	chats    ReportChatStore
	log      *slog.Logger
}

// ReportChatStore 举报聊天对象所需的最小读取能力。
type ReportChatStore interface {
	MessageByID(ctx context.Context, id uint64) (*model.Message, error)
	MemberByUser(ctx context.Context, conversationID, userID uint64) (*model.ConversationMember, error)
}

// NewReportService 构造 ReportService。
func NewReportService(store ReportStore, articles CommentArticleStore, comments CommentStore, chats ReportChatStore, log *slog.Logger) *ReportService {
	return &ReportService{store: store, articles: articles, comments: comments, chats: chats, log: log}
}

// Create 提交举报。
//
// 返回 (举报 ID, 是否重复)。同一用户重复举报同一对象不新增，返回已存在语义。
func (s *ReportService) Create(ctx context.Context, reporterID uint64, in ReportInput) (uint64, bool, error) {
	if !validReportTarget(in.TargetType) {
		return 0, false, badRequest("非法的举报对象类型", "target_type")
	}
	if !validReportReason(in.Reason) {
		return 0, false, badRequest("非法的举报原因", "reason")
	}
	if in.Reason == "other" && in.Detail == "" {
		return 0, false, badRequest("选择其他原因时必须填写详细说明", "detail")
	}
	if len([]rune(in.Detail)) > 512 {
		return 0, false, badRequest("详细说明最多 512 字", "detail")
	}

	// 校验目标存在：不存在的对象举报没有意义，也防止垃圾数据进队列
	switch in.TargetType {
	case model.ReportTargetArticle:
		if _, err := s.articles.ByID(ctx, in.TargetID); err != nil {
			if err == repository.ErrNotFound {
				return 0, false, errs.NotFound("文章不存在")
			}
			return 0, false, errs.Internal(err)
		}
	case model.ReportTargetComment:
		if _, err := s.comments.CommentByID(ctx, in.TargetID); err != nil {
			if err == repository.ErrNotFound {
				return 0, false, errs.NotFound("评论不存在")
			}
			return 0, false, errs.Internal(err)
		}
	case model.ReportTargetMessage:
		// 仅消息所属会话的成员可举报该消息
		msg, err := s.chats.MessageByID(ctx, in.TargetID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return 0, false, errs.NotFound("消息不存在")
			}
			return 0, false, errs.Internal(err)
		}
		if _, err := s.chats.MemberByUser(ctx, msg.ConversationID, reporterID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return 0, false, errs.Forbidden("只能举报你所在会话中的消息")
			}
			return 0, false, errs.Internal(err)
		}
	case model.ReportTargetConversation:
		// 仅会话成员可举报会话
		if _, err := s.chats.MemberByUser(ctx, in.TargetID, reporterID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return 0, false, errs.Forbidden("只能举报你参与的会话")
			}
			return 0, false, errs.Internal(err)
		}
	}

	rep := &model.Report{
		ReporterID: reporterID,
		TargetType: in.TargetType,
		TargetID:   in.TargetID,
		Reason:     in.Reason,
		Detail:     in.Detail,
		Status:     model.ReportPending,
	}
	created, err := s.store.ReportCreate(ctx, rep)
	if err != nil {
		return 0, false, errs.Internal(err)
	}
	return rep.ID, !created, nil
}

func validReportTarget(t string) bool {
	switch t {
	case model.ReportTargetArticle, model.ReportTargetComment,
		model.ReportTargetMessage, model.ReportTargetConversation:
		return true
	}
	return false
}

func validReportReason(r string) bool {
	switch r {
	case "spam", "abuse", "porn", "illegal", "other":
		return true
	}
	return false
}
