package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// AdminStore 是管理端所需的数据能力。
type AdminStore interface {
	GetOverview(ctx context.Context) (*repository.Overview, error)
	ListUsers(ctx context.Context, f repository.UserListFilter) ([]model.User, error)
	UpdateUserStatus(ctx context.Context, id uint64, status uint8) error
	UpdateUserRole(ctx context.Context, id uint64, role authz.Role) error
	CountAdmins(ctx context.Context) (int64, error)
	ListArticlesAdmin(ctx context.Context, status *uint8, authorID uint64, query string, cursorID uint64, limit int) ([]model.Article, error)
	CreateOperationLog(ctx context.Context, log *model.OperationLog) error
	CreateCategory(ctx context.Context, c *model.Category) error
	UpdateCategory(ctx context.Context, c *model.Category) error
	DeleteCategory(ctx context.Context, id uint64) error
	CreateTag(ctx context.Context, t *model.Tag) error
	UpdateTag(ctx context.Context, t *model.Tag) error
	DeleteTag(ctx context.Context, id uint64) error
	MergeTags(ctx context.Context, fromID, toID uint64) error
	ListLogs(ctx context.Context, f repository.LogFilter) ([]model.OperationLog, error)
	ListIndexFailures(ctx context.Context, limit int) ([]model.Article, error)
	RecalcCategoryCounts(ctx context.Context) (int64, error)
	RecalcTagCounts(ctx context.Context) (int64, error)
	ArticleByID(ctx context.Context, id uint64) (*model.Article, error)
	ListMedia(ctx context.Context, f repository.MediaListFilter) ([]model.Media, error)
}

// MediaURLBuilder 由媒体对象 Key 生成访问 URL。
type MediaURLBuilder interface {
	URL(key string) string
}

// AdminInteractionStore 是管理端访问评论/举报数据的能力。
type AdminInteractionStore interface {
	ListCommentsAdmin(ctx context.Context, status *uint8, articleID uint64, cursorID uint64, limit int) ([]model.Comment, error)
	ReportByID(ctx context.Context, id uint64) (*model.Report, error)
	ListReports(ctx context.Context, status *uint8, targetType string, cursorID uint64, limit int) ([]model.Report, error)
	UpdateReportStatus(ctx context.Context, id, handlerID uint64, status uint8, note string) error
}

// AdminUserReader 是管理端读取目标用户的能力。
type AdminUserReader interface {
	ByID(ctx context.Context, id uint64) (*model.User, error)
}

// SessionRevoker 吊销用户全部会话（禁用用户时）。
type SessionRevoker interface {
	RevokeAll(ctx context.Context, uid uint64) error
}

// AuthCacheInvalidator 失效权限缓存（角色/状态变更时）。
type AuthCacheInvalidator interface {
	Invalidate(ctx context.Context, uid uint64) error
}

// AdminService 实现管理端核心操作。
// SettingStore 是系统配置的读写能力。
type SettingStore interface {
	All(ctx context.Context) (map[string]string, error)
	Set(ctx context.Context, key, value, group, desc string) error
}

type AdminService struct {
	store        AdminStore
	interactions AdminInteractionStore
	users        AdminUserReader
	articles     *ArticleService
	comments     *CommentService
	index        *IndexService
	settings     SettingStore
	mediaURL     MediaURLBuilder
	revoker      SessionRevoker
	invalidator  AuthCacheInvalidator
	notify       NotificationWriter
	kicker       UserKicker
	pusher       NotificationPusher
	rebuilding   atomic.Bool // 全量重建互斥：同一时刻只允许一个后台任务
	log          *slog.Logger
}

// UserKicker 把用户踢下线（禁用立即生效的最后一环）。
type UserKicker interface {
	Kick(userID uint64, reason string)
}

// NewAdminService 构造 AdminService。
func NewAdminService(
	store AdminStore,
	interactions AdminInteractionStore,
	users AdminUserReader,
	articles *ArticleService,
	comments *CommentService,
	index *IndexService,
	settings SettingStore,
	mediaURL MediaURLBuilder,
	revoker SessionRevoker,
	invalidator AuthCacheInvalidator,
	notify NotificationWriter,
	kicker UserKicker,
	pusher NotificationPusher,
	log *slog.Logger,
) *AdminService {
	return &AdminService{
		store:        store,
		interactions: interactions,
		users:        users,
		articles:     articles,
		comments:     comments,
		index:        index,
		settings:     settings,
		mediaURL:     mediaURL,
		revoker:      revoker,
		invalidator:  invalidator,
		notify:       notify,
		kicker:       kicker,
		pusher:       pusher,
		log:          log,
	}
}

// Overview 返回概览统计。
func (s *AdminService) Overview(ctx context.Context) (*repository.Overview, error) {
	o, err := s.store.GetOverview(ctx)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return o, nil
}

// ListUsers 分页返回用户列表。
func (s *AdminService) ListUsers(ctx context.Context, query string, role, status *uint8, cursor string, limit int) ([]model.User, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListUsers(ctx, repository.UserListFilter{
		Query:    query,
		Role:     role,
		Status:   status,
		CursorID: cursorID,
		Limit:    limit,
	})
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

// SetUserStatus 禁用或启用用户。
func (s *AdminService) SetUserStatus(ctx context.Context, operatorID, targetID uint64, status uint8, reason, ip, ua string) error {
	if targetID == operatorID {
		return errs.New(errs.CodeCannotDisableSelf, 400, "不能操作自己的账号")
	}
	target, err := s.users.ByID(ctx, targetID)
	if err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("用户不存在")
		}
		return errs.Internal(err)
	}
	// 不能禁用最后一个管理员
	if authz.Role(target.Role) == authz.RoleAdmin && status == model.StatusDisabled {
		n, err := s.store.CountAdmins(ctx)
		if err != nil {
			return errs.Internal(err)
		}
		if n <= 1 {
			return errs.New(errs.CodeLastAdmin, 400, "不能禁用最后一个管理员")
		}
	}

	if err := s.store.UpdateUserStatus(ctx, targetID, status); err != nil {
		return errs.Internal(err)
	}

	// 状态变更立即生效：吊销令牌 + 失效权限缓存 + 踢下线，
	// 不留"看似禁用了实际没禁用"的窗口
	if status == model.StatusDisabled {
		if err := s.revoker.RevokeAll(ctx, targetID); err != nil {
			s.log.Warn("吊销令牌失败", "user_id", targetID, "err", err)
		}
		if s.kicker != nil {
			s.kicker.Kick(targetID, "账号已被禁用")
		}
	}
	if err := s.invalidator.Invalidate(ctx, targetID); err != nil {
		s.log.Warn("失效权限缓存失败", "user_id", targetID, "err", err)
	}

	s.audit(ctx, operatorID, "user.set_status", "user", targetID, ip, ua, map[string]any{
		"status": status, "reason": reason,
	})
	return nil
}

// SetUserRole 调整用户角色。
func (s *AdminService) SetUserRole(ctx context.Context, operatorID, targetID uint64, role authz.Role, ip, ua string) error {
	if !role.Valid() {
		return badRequest("非法角色", "role")
	}
	if role == authz.RoleAdmin {
		// role=admin 本身是合法角色名，拒绝原因是权限管控而非参数错误：
		// 管理员只能通过服务器端命令行创建，避免接口提权。
		return errs.Forbidden("不允许通过接口设置管理员角色，请使用服务器端命令行工具")
	}
	target, err := s.users.ByID(ctx, targetID)
	if err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("用户不存在")
		}
		return errs.Internal(err)
	}
	// 把最后一个 admin 降级 → 拒绝
	if authz.Role(target.Role) == authz.RoleAdmin && role != authz.RoleAdmin {
		n, err := s.store.CountAdmins(ctx)
		if err != nil {
			return errs.Internal(err)
		}
		if n <= 1 {
			return errs.New(errs.CodeLastAdmin, 400, "不能撤销最后一个管理员")
		}
	}

	if err := s.store.UpdateUserRole(ctx, targetID, role); err != nil {
		return errs.Internal(err)
	}
	// 角色实时读取，缓存失效即生效；不吊销令牌（PRD 2.4）
	if err := s.invalidator.Invalidate(ctx, targetID); err != nil {
		s.log.Warn("失效权限缓存失败", "user_id", targetID, "err", err)
	}
	s.audit(ctx, operatorID, "user.set_role", "user", targetID, ip, ua, map[string]any{"role": role.String()})
	return nil
}

// ListArticles 分页返回全站文章。
func (s *AdminService) ListArticles(ctx context.Context, status *uint8, authorID uint64, query, cursor string, limit int) ([]model.Article, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListArticlesAdmin(ctx, status, authorID, query, cursorID, limit)
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

// ReviewArticle 执行文章审核动作（approve/reject/offline/relist）。
func (s *AdminService) ReviewArticle(ctx context.Context, operatorID, articleID uint64, action ArticleAction, reason, ip, ua string) (*model.Article, error) {
	a, err := s.articles.Transition(ctx, articleID, operatorID, authz.RoleAdmin, action, reason)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, operatorID, "article."+string(action), "article", articleID, ip, ua, map[string]any{"reason": reason})
	s.notifyAuthor(ctx, a, action)
	return a, nil
}

// DeleteArticle 管理员删除文章。
func (s *AdminService) DeleteArticle(ctx context.Context, operatorID, articleID uint64, ip, ua string) error {
	if err := s.articles.Delete(ctx, articleID, operatorID, authz.RoleAdmin); err != nil {
		return err
	}
	s.audit(ctx, operatorID, "article.delete", "article", articleID, ip, ua, nil)
	return nil
}

// notifyAuthor 审核结果通知作者（拒绝/下架时）。
func (s *AdminService) notifyAuthor(ctx context.Context, a *model.Article, action ArticleAction) {
	var typ, title string
	switch action {
	case ActionReject:
		typ, title = "article_rejected", "你的文章未通过审核"
	case ActionOffline:
		typ, title = "article_offline", "你的文章已被下架"
	default:
		return
	}
	if a.AuthorID == 0 {
		return
	}
	refID := a.ID
	n2 := &model.Notification{
		UserID:  a.AuthorID,
		Type:    typ,
		Title:   title,
		Content: "文章《" + a.Title + "》",
		Link:    "/articles/" + a.Slug,
		RefID:   &refID,
	}
	if err := s.notify.CreateNotification(ctx, n2); err != nil {
		s.log.Warn("通知作者失败", "article_id", a.ID, "err", err)
		return
	}
	if s.pusher != nil {
		s.pusher.PushToUser(a.AuthorID, NotificationFrame(n2))
	}
}

// audit 写操作审计日志；失败只记日志，不阻断主操作。
func (s *AdminService) audit(ctx context.Context, operatorID uint64, action, targetType string, targetID uint64, ip, ua string, detail map[string]any) {
	var detailJSON *string
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			j := string(b)
			detailJSON = &j
		}
	}
	log := &model.OperationLog{
		OperatorID: operatorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detailJSON,
		IP:         ip,
		UserAgent:  ua,
	}
	if err := s.store.CreateOperationLog(ctx, log); err != nil {
		s.log.Error("写操作日志失败", "action", action, "err", err)
	}
}

// ---- 评论管理 ----

// ListComments 管理端评论列表（可看全部状态）。
func (s *AdminService) ListComments(ctx context.Context, status *uint8, articleID uint64, cursor string, limit int) ([]model.Comment, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.interactions.ListCommentsAdmin(ctx, status, articleID, cursorID, limit)
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

// ReviewComment 评论审核（approve/reject）。
func (s *AdminService) ReviewComment(ctx context.Context, operatorID, commentID uint64, approve bool, ip, ua string) error {
	var err error
	if approve {
		err = s.comments.Approve(ctx, commentID)
	} else {
		err = s.comments.Reject(ctx, commentID)
	}
	if err != nil {
		return err
	}
	action := "comment.reject"
	if approve {
		action = "comment.approve"
	}
	s.audit(ctx, operatorID, action, "comment", commentID, ip, ua, nil)
	return nil
}

// DeleteComment 管理端删除评论。
func (s *AdminService) DeleteComment(ctx context.Context, operatorID, commentID uint64, ip, ua string) error {
	if err := s.comments.Delete(ctx, commentID, operatorID, authz.RoleAdmin); err != nil {
		return err
	}
	s.audit(ctx, operatorID, "comment.delete", "comment", commentID, ip, ua, nil)
	return nil
}

// ---- 举报处理 ----

// ListReports 举报队列。
func (s *AdminService) ListReports(ctx context.Context, status *uint8, targetType, cursor string, limit int) ([]model.Report, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.interactions.ListReports(ctx, status, targetType, cursorID, limit)
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

// HandleReport 处理举报，可联动处置动作。
//
// action 联动失败时不回滚举报状态：举报已处理是事实，
// 处置失败会记录错误日志，管理员可在对应模块重试。
func (s *AdminService) HandleReport(ctx context.Context, operatorID, reportID uint64, result, note, action, ip, ua string) error {
	rep, err := s.interactions.ReportByID(ctx, reportID)
	if err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("举报不存在")
		}
		return errs.Internal(err)
	}

	var status uint8
	switch result {
	case "accepted":
		status = model.ReportAccepted
	case "rejected":
		status = model.ReportRejected
	default:
		return badRequest("非法的处理结果", "result")
	}
	if action == "" {
		action = "none"
	}
	validActions := map[string]bool{"none": true, "offline": true, "delete_comment": true, "disable_user": true}
	if !validActions[action] {
		return badRequest("非法的处置动作", "action")
	}
	if status == model.ReportRejected && action != "none" {
		return badRequest("驳回举报时不能执行处置动作", "action")
	}
	if rep.Status != model.ReportPending {
		return errs.New(errs.CodeInvalidArticleState, 409, "该举报已处理")
	}

	if err := s.interactions.UpdateReportStatus(ctx, reportID, operatorID, status, note); err != nil {
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "report.handle", rep.TargetType, rep.TargetID, ip, ua,
		map[string]any{"result": result, "action": action})

	if status != model.ReportAccepted {
		return nil
	}
	// 联动处置
	switch action {
	case "offline":
		if rep.TargetType != model.ReportTargetArticle {
			return badRequest("下架动作仅适用于文章举报", "action")
		}
		if _, err := s.articles.Transition(ctx, rep.TargetID, operatorID, authz.RoleAdmin, ActionOffline, note); err != nil {
			s.log.Error("举报联动下架失败", "article_id", rep.TargetID, "err", err)
		}
	case "delete_comment":
		if rep.TargetType != model.ReportTargetComment {
			return badRequest("删除评论动作仅适用于评论举报", "action")
		}
		if err := s.comments.Delete(ctx, rep.TargetID, operatorID, authz.RoleAdmin); err != nil {
			s.log.Error("举报联动删评论失败", "comment_id", rep.TargetID, "err", err)
		}
	case "disable_user":
		var uid uint64
		switch rep.TargetType {
		case model.ReportTargetArticle:
			if a, err := s.articles.ByID(ctx, rep.TargetID); err == nil {
				uid = a.AuthorID
			}
		case model.ReportTargetComment:
			if cm, err := s.comments.CommentByID(ctx, rep.TargetID); err == nil {
				uid = cm.UserID
			}
		}
		if uid == 0 || uid == operatorID {
			return nil
		}
		if err := s.SetUserStatus(ctx, operatorID, uid, model.StatusDisabled, "举报处置", ip, ua); err != nil {
			s.log.Error("举报联动禁用用户失败", "user_id", uid, "err", err)
		}
	}
	return nil
}

// ---- 分类与标签 ----

// CreateCategory 创建分类。
func (s *AdminService) CreateCategory(ctx context.Context, c *model.Category, operatorID uint64, ip, ua string) error {
	if err := s.store.CreateCategory(ctx, c); err != nil {
		if err == repository.ErrDuplicate {
			return errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "category.create", "category", c.ID, ip, ua, map[string]any{"name": c.Name})
	return nil
}

// UpdateCategory 更新分类。
func (s *AdminService) UpdateCategory(ctx context.Context, c *model.Category, operatorID uint64, ip, ua string) error {
	if err := s.store.UpdateCategory(ctx, c); err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("分类不存在")
		}
		if err == repository.ErrDuplicate {
			return errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "category.update", "category", c.ID, ip, ua, nil)
	return nil
}

// DeleteCategory 删除分类（文章置为未分类）。
func (s *AdminService) DeleteCategory(ctx context.Context, id, operatorID uint64, ip, ua string) error {
	if err := s.store.DeleteCategory(ctx, id); err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("分类不存在")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "category.delete", "category", id, ip, ua, nil)
	return nil
}

// CreateTag / UpdateTag / DeleteTag / MergeTags 标签管理。
func (s *AdminService) CreateTag(ctx context.Context, t *model.Tag, operatorID uint64, ip, ua string) error {
	if err := s.store.CreateTag(ctx, t); err != nil {
		if err == repository.ErrDuplicate {
			return errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "tag.create", "tag", t.ID, ip, ua, nil)
	return nil
}

func (s *AdminService) UpdateTag(ctx context.Context, t *model.Tag, operatorID uint64, ip, ua string) error {
	if err := s.store.UpdateTag(ctx, t); err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("标签不存在")
		}
		if err == repository.ErrDuplicate {
			return errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "tag.update", "tag", t.ID, ip, ua, nil)
	return nil
}

func (s *AdminService) DeleteTag(ctx context.Context, id, operatorID uint64, ip, ua string) error {
	if err := s.store.DeleteTag(ctx, id); err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("标签不存在")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "tag.delete", "tag", id, ip, ua, nil)
	return nil
}

func (s *AdminService) MergeTags(ctx context.Context, fromID, toID, operatorID uint64, ip, ua string) error {
	if err := s.store.MergeTags(ctx, fromID, toID); err != nil {
		if err == repository.ErrNotFound {
			return errs.NotFound("标签不存在")
		}
		return errs.Internal(err)
	}
	s.audit(ctx, operatorID, "tag.merge", "tag", toID, ip, ua, map[string]any{"from": fromID})
	return nil
}

// ---- 设置 / 日志 / 索引运维 ----

// GetSettings 返回全部配置。
func (s *AdminService) GetSettings(ctx context.Context) (map[string]string, error) {
	m, err := s.settings.All(ctx)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return m, nil
}

// UpdateSettings 批量更新配置；key 白名单校验防止写入任意键。
func (s *AdminService) UpdateSettings(ctx context.Context, operatorID uint64, items map[string]string, ip, ua string) error {
	allowed := map[string]bool{
		"site.name": true, "site.description": true, "site.logo_url": true, "site.icp": true,
		"review.article": true, "review.comment": true, "review.register": true,
		"register.enabled": true, "comment.enabled": true,
	}
	for key, value := range items {
		if !allowed[key] {
			return badRequest("不允许修改配置项: "+key, "items")
		}
		if err := s.settings.Set(ctx, key, value, "", ""); err != nil {
			return errs.Internal(err)
		}
	}
	s.audit(ctx, operatorID, "settings.update", "settings", 0, ip, ua, map[string]any{"keys": strings.Join(keysOf(items), ",")})
	return nil
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ListLogs 操作日志。
func (s *AdminService) ListLogs(ctx context.Context, action, targetType, cursor string, limit int) ([]model.OperationLog, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListLogs(ctx, repository.LogFilter{
		Action: action, TargetType: targetType, CursorID: cursorID, Limit: limit,
	})
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

// IndexFailures 索引同步失败列表。
func (s *AdminService) IndexFailures(ctx context.Context, limit int) ([]model.Article, error) {
	rows, err := s.store.ListIndexFailures(ctx, limit)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return rows, nil
}

// Reindex 重建索引：单篇或全量。
func (s *AdminService) Reindex(ctx context.Context, operatorID uint64, articleID uint64, all bool, ip, ua string) (int, bool, error) {
	if all {
		// 全量重建是 O(文章数) 的同步循环，1 万篇会超过网关 60s 超时。
		// 改为后台执行：立刻返回 202，避免管理端等到 504。
		if !s.rebuilding.CompareAndSwap(false, true) {
			return 0, false, badRequest("全量重建正在进行中，请稍后查看失败队列", "body")
		}
		// 不能用请求的 ctx：请求一返回它就被取消，后台任务会被立刻打断
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		go func() {
			defer cancel()
			defer s.rebuilding.Store(false)
			n, err := s.index.RebuildAll(bg, 200)
			if err != nil {
				s.log.Error("全量重建索引失败", "err", err, "synced", n)
				return
			}
			s.log.Info("全量重建索引完成", "count", n)
			s.audit(bg, operatorID, "index.rebuild", "article", 0, ip, ua, map[string]any{"all": true, "count": n})
		}()
		return 0, true, nil
	}
	if articleID == 0 {
		return 0, false, badRequest("必须指定 article_id 或 all=true", "body")
	}
	if err := s.index.Sync(ctx, articleID); err != nil {
		return 0, false, errs.Internal(err)
	}
	s.audit(ctx, operatorID, "index.rebuild", "article", articleID, ip, ua, map[string]any{"all": false, "count": 1})
	return 1, false, nil
}

// RecalcCounters 重算分类与标签计数（计数漂移的人工修复入口）。
func (s *AdminService) RecalcCounters(ctx context.Context, operatorID uint64, ip, ua string) (int64, int64, error) {
	cats, err := s.store.RecalcCategoryCounts(ctx)
	if err != nil {
		return 0, 0, errs.Internal(err)
	}
	tags, err := s.store.RecalcTagCounts(ctx)
	if err != nil {
		return 0, 0, errs.Internal(err)
	}
	s.audit(ctx, operatorID, "counters.recalc", "system", 0, ip, ua, map[string]any{"categories": cats, "tags": tags})
	return cats, tags, nil
}

// ArticleDetail 管理端文章详情：任意状态，含已删除（PRD 2.2.1）。
func (s *AdminService) ArticleDetail(ctx context.Context, id uint64) (*model.Article, []uint64, string, error) {
	a, err := s.store.ArticleByID(ctx, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, nil, "", errs.NotFound("文章不存在")
		}
		return nil, nil, "", errs.Internal(err)
	}
	tagIDs, err := s.articles.TagIDsOf(ctx, id)
	if err != nil {
		return nil, nil, "", errs.Internal(err)
	}
	authorName := ""
	if users, uerr := s.articles.UsersByIDs(ctx, []uint64{a.AuthorID}); uerr == nil {
		if u, ok := users[a.AuthorID]; ok {
			authorName = u.Nickname
			if authorName == "" {
				authorName = u.Username
			}
		}
	}
	return a, tagIDs, authorName, nil
}

// ListMedia 管理端媒体库列表。
func (s *AdminService) ListMedia(ctx context.Context, purpose string, uploaderID uint64, cursor string, limit int) ([]model.Media, string, bool, error) {
	var cursorID uint64
	if cursor != "" {
		var err error
		cursorID, err = decodeIDCursor(cursor)
		if err != nil {
			return nil, "", false, badRequest("无效的分页游标", "cursor")
		}
	}
	rows, err := s.store.ListMedia(ctx, repository.MediaListFilter{
		Purpose:    purpose,
		UploaderID: uploaderID,
		CursorID:   cursorID,
		Limit:      limit,
	})
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

// MediaURL 暴露媒体 URL 拼接（handler 用）。
func (s *AdminService) MediaURL(key string) string {
	return s.mediaURL.URL(key)
}
