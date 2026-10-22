package service

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strconv"
	"strings"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// CommentStore 是评论服务所需的数据能力。
type CommentStore interface {
	CreateComment(ctx context.Context, c *model.Comment, countUp bool) error
	CommentByID(ctx context.Context, id uint64) (*model.Comment, error)
	RootComments(ctx context.Context, articleID, viewerID uint64, cursorID uint64, limit int) ([]model.Comment, error)
	RepliesByRoots(ctx context.Context, rootIDs []uint64, viewerID uint64, perRoot int) (map[uint64][]model.Comment, error)
	ListReplies(ctx context.Context, rootID, viewerID uint64, cursorID uint64, limit int) ([]model.Comment, error)
	SoftDeleteComment(ctx context.Context, id uint64) (int, error)
	ApproveComment(ctx context.Context, id uint64) (*model.Comment, error)
	RejectComment(ctx context.Context, id uint64) (*model.Comment, error)
}

// NotificationWriter 是通知写入能力（评论服务只发不读）。
type NotificationWriter interface {
	CreateNotification(ctx context.Context, n *model.Notification) error
}

// CommentArticleStore 是评论服务需要的文章读取能力（精简接口）。
type CommentArticleStore interface {
	ByID(ctx context.Context, id uint64) (*model.Article, error)
	UsersByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error)
}

// SettingReader 读取系统配置（如评论审核开关）。
type SettingReader interface {
	GetBool(ctx context.Context, key string, def bool) (bool, error)
}

// CommentView 是一条评论的展示结构。
type CommentView struct {
	Comment    *model.Comment
	Author     model.User
	ReplyTo    *model.User
	ReplyCount int
	Replies    []*CommentView
}

// CommentInput 是发表评论的入参。
type CommentInput struct {
	Content       string
	ParentID      uint64
	ReplyToUserID uint64
	IP            string
	UserAgent     string
}

// CommentService 实现评论生命周期。
type CommentService struct {
	store    CommentStore
	articles CommentArticleStore
	settings SettingReader
	notify   NotificationWriter
	pusher   NotificationPusher
	log      *slog.Logger
}

// NewCommentService 构造 CommentService。
func NewCommentService(
	store CommentStore,
	articles CommentArticleStore,
	settings SettingReader,
	notify NotificationWriter,
	pusher NotificationPusher,
	log *slog.Logger,
) *CommentService {
	return &CommentService{store: store, articles: articles, settings: settings, notify: notify, pusher: pusher, log: log}
}

// Create 发表评论。
func (s *CommentService) Create(ctx context.Context, userID uint64, articleID uint64, in CommentInput) (*model.Comment, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, badRequest("评论内容不能为空", "content")
	}
	if len([]rune(content)) > 1000 {
		return nil, badRequest("评论最多 1000 字", "content")
	}

	article, err := s.articles.ByID(ctx, articleID)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, errs.New(errs.CodeArticleNotFound, 404, "文章不存在")
		}
		return nil, errs.Internal(err)
	}
	if article.Status != model.ArticlePublished || article.DeletedAt != nil {
		return nil, errs.New(errs.CodeCommentDisabled, 400, "文章不可评论")
	}
	if article.AllowComment == 0 {
		return nil, errs.New(errs.CodeCommentDisabled, 400, "该文章已关闭评论")
	}

	c := &model.Comment{
		ArticleID:     articleID,
		UserID:        userID,
		Content:       content,
		IP:            in.IP,
		UserAgent:     in.UserAgent,
		ReplyToUserID: in.ReplyToUserID,
	}

	// 回复：校验父评论并推导 root_id
	if in.ParentID != 0 {
		parent, err := s.store.CommentByID(ctx, in.ParentID)
		if err != nil {
			if err == repository.ErrNotFound {
				return nil, errs.New(errs.CodeParentCommentBad, 400, "父评论不存在")
			}
			return nil, errs.Internal(err)
		}
		if parent.ArticleID != articleID {
			return nil, errs.New(errs.CodeParentCommentBad, 400, "父评论不属于该文章")
		}
		if parent.Status != model.CommentApproved {
			return nil, errs.New(errs.CodeParentCommentBad, 400, "父评论尚未通过审核")
		}
		c.ParentID = parent.ID
		if parent.RootID == 0 {
			c.RootID = parent.ID
		} else {
			c.RootID = parent.RootID
		}
	}

	// 审核开关决定初始状态
	review, err := s.settings.GetBool(ctx, "review.comment", true)
	if err != nil {
		return nil, errs.Internal(err)
	}
	if review {
		c.Status = model.CommentPending
	} else {
		c.Status = model.CommentApproved
	}

	if err := s.store.CreateComment(ctx, c, c.Status == model.CommentApproved); err != nil {
		return nil, errs.Internal(err)
	}

	s.sendCommentNotification(ctx, article, c)
	return c, nil
}

// CommentByID 按 ID 取评论（供管理端联动处置时读取作者等信息）。
func (s *CommentService) CommentByID(ctx context.Context, id uint64) (*model.Comment, error) {
	return s.store.CommentByID(ctx, id)
}

// List 返回文章评论列表（主楼 + 前3回复）。
func (s *CommentService) List(ctx context.Context, articleID, viewerID uint64, cursor string, limit int) ([]*CommentView, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}

	roots, err := s.store.RootComments(ctx, articleID, viewerID, cursorID, limit)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	hasMore := len(roots) > limit
	if hasMore {
		roots = roots[:limit]
	}

	rootIDs := make([]uint64, len(roots))
	for i, c := range roots {
		rootIDs[i] = c.ID
	}
	replies, err := s.store.RepliesByRoots(ctx, rootIDs, viewerID, 3)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}

	views := make([]*CommentView, 0, len(roots))
	for i := range roots {
		v, err := s.assembleComment(ctx, &roots[i])
		if err != nil {
			return nil, "", false, err
		}
		for _, r := range replies[roots[i].ID] {
			rv, err := s.assembleComment(ctx, &r)
			if err != nil {
				return nil, "", false, err
			}
			v.Replies = append(v.Replies, rv)
		}
		views = append(views, v)
	}

	var next string
	if hasMore && len(roots) > 0 {
		next = encodeIDCursor(roots[len(roots)-1].ID)
	}
	return views, next, hasMore, nil
}

// ListReplies 返回某主楼下的全部回复。
func (s *CommentService) ListReplies(ctx context.Context, rootID, viewerID uint64, cursor string, limit int) ([]*CommentView, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListReplies(ctx, rootID, viewerID, cursorID, limit)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	views := make([]*CommentView, 0, len(rows))
	for i := range rows {
		v, err := s.assembleComment(ctx, &rows[i])
		if err != nil {
			return nil, "", false, err
		}
		views = append(views, v)
	}
	var next string
	if hasMore && len(rows) > 0 {
		next = encodeIDCursor(rows[len(rows)-1].ID)
	}
	return views, next, hasMore, nil
}

// Delete 删除评论；本人或管理员。
func (s *CommentService) Delete(ctx context.Context, id, actorID uint64, role authz.Role) error {
	c, err := s.store.CommentByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("评论不存在")
		}
		return errs.Internal(err)
	}
	if role != authz.RoleAdmin && c.UserID != actorID {
		return errs.Forbidden("只能删除自己的评论")
	}
	if _, err := s.store.SoftDeleteComment(ctx, id); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// Approve 审核通过（供管理端）。
func (s *CommentService) Approve(ctx context.Context, id uint64) error {
	if _, err := s.store.ApproveComment(ctx, id); err != nil {
		if err == repository.ErrConflict {
			return errs.New(errs.CodeInvalidArticleState, 409, "评论状态已变化")
		}
		return errs.Internal(err)
	}
	return nil
}

// Reject 审核拒绝（供管理端）。
func (s *CommentService) Reject(ctx context.Context, id uint64) error {
	if _, err := s.store.RejectComment(ctx, id); err != nil {
		if err == repository.ErrConflict {
			return errs.New(errs.CodeInvalidArticleState, 409, "评论状态已变化")
		}
		return errs.Internal(err)
	}
	return nil
}

// assembleComment 组装单条评论（含作者与被回复者）。
func (s *CommentService) assembleComment(ctx context.Context, c *model.Comment) (*CommentView, error) {
	uidSet := map[uint64]bool{c.UserID: true}
	if c.ReplyToUserID != 0 {
		uidSet[c.ReplyToUserID] = true
	}
	uids := make([]uint64, 0, len(uidSet))
	for id := range uidSet {
		uids = append(uids, id)
	}
	users, err := s.articles.UsersByIDs(ctx, uids)
	if err != nil {
		return nil, errs.Internal(err)
	}
	v := &CommentView{Comment: c, Author: users[c.UserID]}
	if c.ReplyToUserID != 0 {
		if u, ok := users[c.ReplyToUserID]; ok {
			uu := u
			v.ReplyTo = &uu
		}
	}
	return v, nil
}

// sendCommentNotification 评论后通知（回复 → 被回复者；主楼 → 作者）。
//
// 通知失败不影响评论：通知是弱一致旁路，且唯一索引天然去重。
func (s *CommentService) sendCommentNotification(ctx context.Context, article *model.Article, c *model.Comment) {
	link := "/articles/" + article.Slug
	var target uint64
	var typ string
	var title string

	if c.RootID != 0 && c.ReplyToUserID != 0 && c.ReplyToUserID != c.UserID {
		target = c.ReplyToUserID
		typ = "comment_reply"
		title = "有人回复了你的评论"
	} else if c.RootID == 0 && article.AuthorID != c.UserID {
		target = article.AuthorID
		typ = "article_comment"
		title = "你的文章收到新评论"
	}
	if target == 0 {
		return
	}

	refID := c.ID
	n := &model.Notification{
		UserID:  target,
		Type:    typ,
		Title:   title,
		Content: truncateRunes(c.Content, 50),
		Link:    link,
		RefID:   &refID,
	}
	if err := s.notify.CreateNotification(ctx, n); err != nil {
		s.log.Warn("通知写入失败", "type", typ, "user_id", target, "err", err)
		return
	}
	if s.pusher != nil {
		s.pusher.PushToUser(target, NotificationFrame(n))
	}
}

// encodeIDCursor 编码基于 ID 的游标。
func encodeIDCursor(id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(id, 10)))
}

// decodeIDCursor 解析基于 ID 的游标。
func decodeIDCursor(cursor string) (uint64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(raw), 10, 64)
}
