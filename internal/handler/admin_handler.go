package handler

import (
	"github.com/gin-gonic/gin"

	"go-blog/internal/authz"
	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// AdminHandler 处理管理端接口。
type AdminHandler struct {
	svc *service.AdminService
}

// NewAdminHandler 构造 AdminHandler。
func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// Overview 处理 GET /admin/overview。
func (h *AdminHandler) Overview(c *gin.Context) {
	o, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminOverviewData{
		ArticleCount:        int(o.ArticleCount),
		UserCount:           int(o.UserCount),
		PendingArticleCount: int(o.PendingArticleCount),
		PendingCommentCount: int(o.PendingCommentCount),
		TodayNewArticles:    int(o.TodayNewArticles),
		TodayNewUsers:       int(o.TodayNewUsers),
	})
}

// ListUsers 处理 GET /admin/users。
func (h *AdminHandler) ListUsers(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	role, _ := parseOptionalUint8(c.Query("role"))
	status, _ := parseUserStatusFilter(c.Query("status"))

	rows, next, hasMore, err := h.svc.ListUsers(c.Request.Context(), c.Query("q"), role, status, c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminUserItem, 0, len(rows))
	for _, u := range rows {
		items = append(items, dto.AdminUserItem{
			Id:        uidString(u.ID),
			Username:  u.Username,
			Email:     u.Email,
			Nickname:  u.Nickname,
			Role:      authz.Role(u.Role).String(),
			Status:    userStatusName(u.Status),
			CreatedAt: u.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminUserListData{
		Items:      items,
		NextCursor: nullString(next),
		HasMore:    hasMore,
	})
}

// UpdateUserStatus 处理 PUT /admin/users/{id}/status。
func (h *AdminHandler) UpdateUserStatus(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的用户 ID"))
		return
	}
	var req dto.AdminUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	var status uint8
	switch req.Status {
	case dto.AdminUserStatusRequestStatusActive:
		status = model.StatusActive
	case dto.AdminUserStatusRequestStatusDisabled:
		status = model.StatusDisabled
	default:
		response.Error(c, errs.BadRequest("非法状态"))
		return
	}
	if err := h.svc.SetUserStatus(c.Request.Context(), middleware.UIDOf(c), id, status, derefString(req.Reason), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

// UpdateUserRole 处理 PUT /admin/users/{id}/role。
func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的用户 ID"))
		return
	}
	var req dto.AdminUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	var role authz.Role
	switch req.Role {
	case dto.AdminUserRoleRequestRoleReader:
		role = authz.RoleReader
	case dto.AdminUserRoleRequestRoleAuthor:
		role = authz.RoleAuthor
	default:
		response.Error(c, errs.BadRequest("非法角色"))
		return
	}
	if err := h.svc.SetUserRole(c.Request.Context(), middleware.UIDOf(c), id, role, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

// ListArticles 处理 GET /admin/articles。
func (h *AdminHandler) ListArticles(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	status, _ := parseArticleStatusFilter(c.Query("status"))
	authorID := parseUintPath(c.Query("author_id"))

	rows, next, hasMore, err := h.svc.ListArticles(c.Request.Context(), status, authorID, c.Query("q"), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	// 批量取作者名
	authorIDs := make([]uint64, 0, len(rows))
	for _, a := range rows {
		authorIDs = append(authorIDs, a.AuthorID)
	}
	items := make([]dto.AdminArticleItem, 0, len(rows))
	for _, a := range rows {
		items = append(items, dto.AdminArticleItem{
			Id:           uidString(a.ID),
			Title:        a.Title,
			Slug:         a.Slug,
			Status:       service.ArticleStatusName(a.Status),
			AuthorId:     uidString(a.AuthorID),
			ViewCount:    ptrInt(int(a.ViewCount)),
			LikeCount:    ptrInt(int(a.LikeCount)),
			CommentCount: ptrInt(int(a.CommentCount)),
			RejectReason: nullString(a.RejectReason),
			PublishedAt:  a.PublishedAt,
			CreatedAt:    a.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminArticleListData{
		Items:      items,
		NextCursor: nullString(next),
		HasMore:    hasMore,
	})
}

// review 是文章审核动作的统一处理。
func (h *AdminHandler) review(c *gin.Context, action service.ArticleAction, needReason bool) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的文章 ID"))
		return
	}
	reason := ""
	if needReason {
		var req dto.AdminArticleReasonRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, errs.BadRequest("必须填写理由").WithData(fieldErrorsData("reason", "必须填写理由")))
			return
		}
		reason = req.Reason
	}
	a, err := h.svc.ReviewArticle(c.Request.Context(), middleware.UIDOf(c), id, action, reason, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminArticleStatusData{Id: uidString(a.ID), Status: service.ArticleStatusName(a.Status)})
}

// Approve 处理 POST /admin/articles/{id}/approve。
func (h *AdminHandler) Approve(c *gin.Context) { h.review(c, service.ActionApprove, false) }

// Reject 处理 POST /admin/articles/{id}/reject。
func (h *AdminHandler) Reject(c *gin.Context) { h.review(c, service.ActionReject, true) }

// Offline 处理 POST /admin/articles/{id}/offline。
func (h *AdminHandler) Offline(c *gin.Context) { h.review(c, service.ActionOffline, true) }

// Relist 处理 POST /admin/articles/{id}/relist。
func (h *AdminHandler) Relist(c *gin.Context) { h.review(c, service.ActionReonline, false) }

// DeleteArticle 处理 DELETE /admin/articles/{id}。
func (h *AdminHandler) DeleteArticle(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的文章 ID"))
		return
	}
	if err := h.svc.DeleteArticle(c.Request.Context(), middleware.UIDOf(c), id, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

func userStatusName(status uint8) string {
	switch status {
	case model.StatusActive:
		return "active"
	case model.StatusDisabled:
		return "disabled"
	default:
		return "pending_verify"
	}
}

// ---- 评论管理 ----

// ListComments 处理 GET /admin/comments。
func (h *AdminHandler) ListComments(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	status, _ := parseCommentStatusFilter(c.Query("status"))
	articleID := parseUintPath(c.Query("article_id"))
	rows, next, hasMore, err := h.svc.ListComments(c.Request.Context(), status, articleID, c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminCommentItem, 0, len(rows))
	for _, cm := range rows {
		items = append(items, dto.AdminCommentItem{
			Id:        uidString(cm.ID),
			ArticleId: uidString(cm.ArticleID),
			UserId:    uidString(cm.UserID),
			Content:   cm.Content,
			Status:    string(commentStatusName(cm.Status)),
			Ip:        nullString(cm.IP),
			CreatedAt: cm.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminCommentListData{Items: items, NextCursor: nullString(next), HasMore: hasMore})
}

// ApproveComment 处理 POST /admin/comments/{id}/approve。
func (h *AdminHandler) ApproveComment(c *gin.Context) { h.reviewComment(c, true) }

// RejectComment 处理 POST /admin/comments/{id}/reject。
func (h *AdminHandler) RejectComment(c *gin.Context) { h.reviewComment(c, false) }

func (h *AdminHandler) reviewComment(c *gin.Context, approve bool) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的评论 ID"))
		return
	}
	if err := h.svc.ReviewComment(c.Request.Context(), middleware.UIDOf(c), id, approve, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

// DeleteComment 处理 DELETE /admin/comments/{id}。
func (h *AdminHandler) DeleteComment(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的评论 ID"))
		return
	}
	if err := h.svc.DeleteComment(c.Request.Context(), middleware.UIDOf(c), id, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// ---- 举报处理 ----

// ListReports 处理 GET /admin/reports。
func (h *AdminHandler) ListReports(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	status, _ := parseReportStatusFilter(c.Query("status"))
	rows, next, hasMore, err := h.svc.ListReports(c.Request.Context(), status, c.Query("target_type"), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminReportItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, dto.AdminReportItem{
			Id:         uidString(r.ID),
			ReporterId: uidString(r.ReporterID),
			TargetType: r.TargetType,
			TargetId:   uidString(r.TargetID),
			Reason:     r.Reason,
			Detail:     nullString(r.Detail),
			Status:     reportStatusName(r.Status),
			HandleNote: nullString(r.HandleNote),
			CreatedAt:  r.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminReportListData{Items: items, NextCursor: nullString(next), HasMore: hasMore})
}

// HandleReport 处理 PUT /admin/reports/{id}。
func (h *AdminHandler) HandleReport(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的举报 ID"))
		return
	}
	var req dto.AdminReportHandleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	if err := h.svc.HandleReport(c.Request.Context(), middleware.UIDOf(c), id,
		string(req.Result), derefString(req.Note), string(derefAction(req.Action)),
		c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

func derefAction(p *dto.AdminReportHandleRequestAction) dto.AdminReportHandleRequestAction {
	if p == nil {
		return dto.AdminReportHandleRequestActionNone
	}
	return *p
}

func reportStatusName(status uint8) string {
	switch status {
	case model.ReportAccepted:
		return "accepted"
	case model.ReportRejected:
		return "rejected"
	default:
		return "pending"
	}
}

// ---- 分类与标签管理 ----

// CreateCategory 处理 POST /admin/categories。
func (h *AdminHandler) CreateCategory(c *gin.Context) {
	var req dto.AdminCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	cat := &model.Category{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: derefString(req.Description),
		Sort:        derefInt(req.Sort),
	}
	if err := h.svc.CreateCategory(c.Request.Context(), cat, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.AdminMutationData{Id: uidString(cat.ID)})
}

// UpdateCategory 处理 PUT /admin/categories/{id}。
func (h *AdminHandler) UpdateCategory(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的分类 ID"))
		return
	}
	var req dto.AdminCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	cat := &model.Category{ID: id, Name: req.Name, Slug: req.Slug, Description: derefString(req.Description), Sort: derefInt(req.Sort)}
	if err := h.svc.UpdateCategory(c.Request.Context(), cat, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

// DeleteCategory 处理 DELETE /admin/categories/{id}。
func (h *AdminHandler) DeleteCategory(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的分类 ID"))
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), id, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// CreateTag 处理 POST /admin/tags。
func (h *AdminHandler) CreateTag(c *gin.Context) {
	var req dto.AdminTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	t := &model.Tag{Name: req.Name, Slug: req.Slug}
	if err := h.svc.CreateTag(c.Request.Context(), t, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, dto.AdminMutationData{Id: uidString(t.ID)})
}

// UpdateTag 处理 PUT /admin/tags/{id}。
func (h *AdminHandler) UpdateTag(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的标签 ID"))
		return
	}
	var req dto.AdminTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	t := &model.Tag{ID: id, Name: req.Name, Slug: req.Slug}
	if err := h.svc.UpdateTag(c.Request.Context(), t, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(id)})
}

// DeleteTag 处理 DELETE /admin/tags/{id}。
func (h *AdminHandler) DeleteTag(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的标签 ID"))
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), id, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// MergeTags 处理 POST /admin/tags/merge。
func (h *AdminHandler) MergeTags(c *gin.Context) {
	var req dto.AdminTagMergeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	from := parseUintPath(req.FromId)
	to := parseUintPath(req.ToId)
	if from == 0 || to == 0 {
		response.Error(c, errs.BadRequest("无效的标签 ID"))
		return
	}
	if err := h.svc.MergeTags(c.Request.Context(), from, to, middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminMutationData{Id: uidString(to)})
}

// ---- 设置 / 日志 / 索引运维 ----

// GetSettings 处理 GET /admin/settings。
func (h *AdminHandler) GetSettings(c *gin.Context) {
	m, err := h.svc.GetSettings(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminSettingItem, 0, len(m))
	for k, v := range m {
		items = append(items, dto.AdminSettingItem{Key: k, Value: v})
	}
	response.OK(c, dto.AdminSettingsData{Items: items})
}

// UpdateSettings 处理 PUT /admin/settings。
func (h *AdminHandler) UpdateSettings(c *gin.Context) {
	var req dto.AdminSettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	items := make(map[string]string, len(req.Items))
	for _, it := range req.Items {
		items[it.Key] = it.Value
	}
	if err := h.svc.UpdateSettings(c.Request.Context(), middleware.UIDOf(c), items, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Error(c, err)
		return
	}
	h.GetSettings(c)
}

// ListLogs 处理 GET /admin/logs。
func (h *AdminHandler) ListLogs(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	rows, next, hasMore, err := h.svc.ListLogs(c.Request.Context(), c.Query("action"), c.Query("target_type"), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminLogItem, 0, len(rows))
	for _, l := range rows {
		items = append(items, dto.AdminLogItem{
			Id:         uidString(l.ID),
			OperatorId: uidString(l.OperatorID),
			Action:     l.Action,
			TargetType: l.TargetType,
			TargetId:   nullString(uidString(l.TargetID)),
			Detail:     l.Detail,
			Ip:         nullString(l.IP),
			CreatedAt:  l.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminLogListData{Items: items, NextCursor: nullString(next), HasMore: hasMore})
}

// IndexFailures 处理 GET /admin/index-failures。
func (h *AdminHandler) IndexFailures(c *gin.Context) {
	rows, err := h.svc.IndexFailures(c.Request.Context(), 100)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminIndexFailureItem, 0, len(rows))
	for _, a := range rows {
		items = append(items, dto.AdminIndexFailureItem{
			Id:            uidString(a.ID),
			Title:         a.Title,
			ArticleStatus: nullString(service.ArticleStatusName(a.Status)),
			SyncedAt:      a.SearchSyncedAt,
		})
	}
	response.OK(c, dto.AdminIndexFailureListData{Items: items})
}

// Reindex 处理 POST /admin/index-rebuild。
func (h *AdminHandler) Reindex(c *gin.Context) {
	var req dto.AdminReindexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求格式错误"))
		return
	}
	count, accepted, err := h.svc.Reindex(c.Request.Context(), middleware.UIDOf(c), parseUintPath(derefString(req.ArticleId)), derefBool(req.All), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Error(c, err)
		return
	}
	data := dto.AdminReindexData{Count: ptrInt(count), Accepted: boolPtr(accepted)}
	if accepted {
		// 全量重建已转入后台：立即返回 202，避免网关 60s 超时
		response.Accepted(c, data)
		return
	}
	response.OK(c, data)
}

// RecalcCounters 处理 POST /admin/counters/recalc。
func (h *AdminHandler) RecalcCounters(c *gin.Context) {
	cats, tags, err := h.svc.RecalcCounters(c.Request.Context(), middleware.UIDOf(c), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AdminRecalcData{Categories: int(cats), Tags: int(tags)})
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool {
	return p != nil && *p
}

// ArticleDetail 处理 GET /admin/articles/{id}。
func (h *AdminHandler) ArticleDetail(c *gin.Context) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的文章 ID"))
		return
	}
	a, tagIDs, authorName, err := h.svc.ArticleDetail(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	tags := make([]string, 0, len(tagIDs))
	for _, tid := range tagIDs {
		tags = append(tags, uidString(tid))
	}
	response.OK(c, dto.AdminArticleDetail{
		Id:            uidString(a.ID),
		Title:         a.Title,
		Slug:          a.Slug,
		Summary:       nullString(a.Summary),
		ContentMd:     a.ContentMD,
		CoverUrl:      nullString(a.CoverURL),
		Status:        service.ArticleStatusName(a.Status),
		AuthorId:      uidString(a.AuthorID),
		AuthorName:    nullString(authorName),
		CategoryId:    nullString(uidString(a.CategoryID)),
		TagIds:        &tags,
		ViewCount:     ptrInt(int(a.ViewCount)),
		LikeCount:     ptrInt(int(a.LikeCount)),
		FavoriteCount: ptrInt(int(a.FavoriteCount)),
		CommentCount:  ptrInt(int(a.CommentCount)),
		Version:       int(a.Version),
		AllowComment:  boolPtr(a.AllowComment == 1),
		RejectReason:  nullString(a.RejectReason),
		OfflineReason: nullString(a.OfflineReason),
		Deleted:       boolPtr(a.DeletedAt != nil),
		PublishedAt:   a.PublishedAt,
		DeletedAt:     a.DeletedAt,
		CreatedAt:     ptrTime(a.CreatedAt.UTC()),
		UpdatedAt:     ptrTime(a.UpdatedAt.UTC()),
	})
}

// ListMedia 处理 GET /admin/media。
func (h *AdminHandler) ListMedia(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	rows, next, hasMore, err := h.svc.ListMedia(c.Request.Context(), c.Query("purpose"), parseUintPath(c.Query("uploader_id")), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.AdminMediaItem, 0, len(rows))
	for _, m := range rows {
		items = append(items, dto.AdminMediaItem{
			Id:           uidString(m.ID),
			Url:          h.svc.MediaURL(m.ObjectKey),
			OriginalName: nullString(m.OriginalName),
			MimeType:     m.MimeType,
			Size:         intPtr64(m.Size),
			Width:        ptrInt(int(m.Width)),
			Height:       ptrInt(int(m.Height)),
			Variants:     nullString(derefString(m.Variants)),
			Purpose:      m.Purpose,
			UploaderId:   nullString(uidString(m.UploaderID)),
			CreatedAt:    m.CreatedAt.UTC(),
		})
	}
	response.OK(c, dto.AdminMediaListData{Items: items, NextCursor: nullString(next), HasMore: hasMore})
}

func boolPtr(b bool) *bool { return &b }

func int64Ptr(v int64) *int64 { return &v }

func intPtr64(v int64) *int {
	n := int(v)
	return &n
}
