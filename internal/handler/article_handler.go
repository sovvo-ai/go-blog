package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// ArticleHandler 处理文章相关接口。
type ArticleHandler struct {
	svc *service.ArticleService
}

// NewArticleHandler 构造 ArticleHandler。
func NewArticleHandler(svc *service.ArticleService) *ArticleHandler {
	return &ArticleHandler{svc: svc}
}

// List 处理 GET /articles。
func (h *ArticleHandler) List(c *gin.Context) {
	limit := parseLimit(c.Query("limit"))

	var categoryID *uint64
	if raw := c.Query("category_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.Error(c, errs.BadRequest("category_id 格式不正确"))
			return
		}
		categoryID = &id
	}
	var tagID *uint64
	if raw := c.Query("tag_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.Error(c, errs.BadRequest("tag_id 格式不正确"))
			return
		}
		tagID = &id
	}

	sort := c.DefaultQuery("sort", "latest")
	if sort != "latest" && sort != "hot" {
		response.Error(c, errs.BadRequest("sort 只支持 latest 或 hot"))
		return
	}

	views, next, hasMore, err := h.svc.ListPublic(c.Request.Context(), categoryID, tagID, sort, c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}

	items := make([]dto.ArticleBrief, 0, len(views))
	for _, v := range views {
		items = append(items, toArticleBrief(v))
	}
	response.OK(c, dto.ArticleListData{
		Items:      items,
		NextCursor: nullableString(next),
		HasMore:    hasMore,
	})
}

// Detail 处理 GET /articles/{key}。key 可以是 slug 或数字 ID。
func (h *ArticleHandler) Detail(c *gin.Context) {
	views, err := h.svc.GetDetail(c.Request.Context(), c.Param("id"), middleware.UIDOf(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toArticleDetail(views))
}

// GetForEdit 处理 GET /articles/{id}/edit。
func (h *ArticleHandler) GetForEdit(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	a, tagIDs, err := h.svc.GetForEdit(c.Request.Context(), id, middleware.UIDOf(c), middleware.RoleOf(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toArticleEdit(a, tagIDs))
}

// Create 处理 POST /articles。
func (h *ArticleHandler) Create(c *gin.Context) {
	var req dto.CreateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}

	in := service.ArticleInput{
		Title:        req.Title,
		ContentMD:    req.ContentMd,
		Summary:      derefString(req.Summary),
		CoverURL:     derefString(req.CoverUrl),
		CategoryID:   parseIDOrZero(derefString(req.CategoryId)),
		TagIDs:       parseIDs(derefTags(req.TagIds)),
		AllowComment: req.AllowComment == nil || *req.AllowComment,
	}
	if req.Slug != nil {
		in.Slug = *req.Slug
	}

	a, err := h.svc.Create(c.Request.Context(), middleware.UIDOf(c), middleware.RoleOf(c), in)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, toArticleMutation(a))
}

// Update 处理 PUT /articles/{id}。
func (h *ArticleHandler) Update(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req dto.UpdateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}
	// 不在这里判断 version 是否"缺失"：0 是合法初始版本，
	// 缺失时同样为 0，交给 service 做乐观锁比较（不匹配即 409）。

	in := service.ArticleInput{
		Title:        derefString(req.Title),
		ContentMD:    derefString(req.ContentMd),
		Summary:      derefString(req.Summary),
		CoverURL:     derefString(req.CoverUrl),
		CategoryID:   parseIDOrZero(derefString(req.CategoryId)),
		TagIDs:       parseIDs(derefTags(req.TagIds)),
		AllowComment: req.AllowComment == nil || *req.AllowComment,
		Version:      uint32(req.Version),
	}
	if req.Slug != nil {
		in.Slug = *req.Slug
	}

	a, err := h.svc.Update(c.Request.Context(), id, middleware.UIDOf(c), middleware.RoleOf(c), in)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toArticleMutation(a))
}

// Delete 处理 DELETE /articles/{id}。
func (h *ArticleHandler) Delete(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, middleware.UIDOf(c), middleware.RoleOf(c)); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// Transition 处理 submit / withdraw / archive / restore 四个动作。
func (h *ArticleHandler) Transition(action service.ArticleAction) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		// 动作类接口可能没有请求体，绑定失败不视为错误
		_ = c.ShouldBindJSON(&body)

		a, err := h.svc.Transition(c.Request.Context(), id, middleware.UIDOf(c), middleware.RoleOf(c), action, body.Reason)
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, dto.ArticleStatusData{
			Id:     uidString(a.ID),
			Status: dto.ArticleStatus(service.ArticleStatusName(a.Status)),
		})
	}
}

// ListMine 处理 GET /users/me/articles。
func (h *ArticleHandler) ListMine(c *gin.Context) {
	var statuses []uint8
	if raw := strings.TrimSpace(c.Query("status")); raw != "" {
		for _, name := range strings.Split(raw, ",") {
			st, err := service.ArticleStatusFrom(strings.TrimSpace(name))
			if err != nil {
				response.Error(c, errs.BadRequest("status 取值非法"))
				return
			}
			statuses = append(statuses, st)
		}
	}

	views, next, hasMore, err := h.svc.ListMine(c.Request.Context(), middleware.UIDOf(c), statuses, c.Query("cursor"), parseLimit(c.Query("limit")))
	if err != nil {
		response.Error(c, err)
		return
	}

	items := make([]dto.MyArticleItem, 0, len(views))
	for _, v := range views {
		items = append(items, toMyArticleItem(v.Article))
	}
	response.OK(c, dto.MyArticleListData{
		Items:      items,
		NextCursor: nullableString(next),
		HasMore:    hasMore,
	})
}

// Categories 处理 GET /categories。
func (h *ArticleHandler) Categories(c *gin.Context) {
	cats, err := h.svc.ListCategories(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.CategoryItem, 0, len(cats))
	for _, cat := range cats {
		items = append(items, dto.CategoryItem{
			Id:           uidString(cat.ID),
			Name:         cat.Name,
			Slug:         cat.Slug,
			Description:  ptrString(cat.Description),
			ArticleCount: int(cat.ArticleCount),
			Sort:         ptrInt(cat.Sort),
		})
	}
	response.OK(c, gin.H{"items": items})
}

// Tags 处理 GET /tags。
func (h *ArticleHandler) Tags(c *gin.Context) {
	tags, err := h.svc.ListTags(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.TagItem, 0, len(tags))
	for _, t := range tags {
		items = append(items, dto.TagItem{
			Id:           uidString(t.ID),
			Name:         t.Name,
			Slug:         t.Slug,
			ArticleCount: int(t.ArticleCount),
		})
	}
	response.OK(c, gin.H{"items": items})
}

// ---- 参数与映射 ----

func parseIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, errs.NotFound("文章不存在"))
		return 0, false
	}
	return id, true
}

func parseLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > 50 {
		return 20
	}
	return n
}

func parseIDOrZero(raw string) uint64 {
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func parseIDs(raw []string) []uint64 {
	if len(raw) == 0 {
		return nil
	}
	out := make([]uint64, 0, len(raw))
	for _, s := range raw {
		if id, err := strconv.ParseUint(s, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toAuthorBrief(u model.User) dto.AuthorBrief {
	return dto.AuthorBrief{
		Id:        uidString(u.ID),
		Nickname:  u.DisplayName(),
		AvatarUrl: u.AvatarURL,
	}
}

func toTagBriefs(tags []model.Tag) []dto.TagBrief {
	out := make([]dto.TagBrief, 0, len(tags))
	for _, t := range tags {
		out = append(out, dto.TagBrief{Id: uidString(t.ID), Name: t.Name})
	}
	return out
}

func toArticleBrief(v *service.ArticleView) dto.ArticleBrief {
	a := v.Article
	brief := dto.ArticleBrief{
		Id:           uidString(a.ID),
		Slug:         a.Slug,
		Title:        a.Title,
		Summary:      a.Summary,
		CoverUrl:     a.CoverURL,
		Author:       toAuthorBrief(v.Author),
		Tags:         ptrTags(toTagBriefs(v.Tags)),
		ViewCount:    int(a.ViewCount),
		LikeCount:    int(a.LikeCount),
		CommentCount: int(a.CommentCount),
		ReadingTime:  int(a.ReadingTime),
		IsTop:        a.IsTop == 1,
	}
	if v.Category != nil {
		brief.Category = &dto.CategoryBrief{Id: uidString(v.Category.ID), Name: v.Category.Name}
	}
	if a.PublishedAt != nil {
		t := a.PublishedAt.UTC()
		brief.PublishedAt = &t
	}
	return brief
}

func toArticleDetail(v *service.ArticleView) dto.ArticleDetail {
	a := v.Article
	html := ""
	if a.ContentHTML != nil {
		html = *a.ContentHTML
	}
	d := dto.ArticleDetail{
		Id:            uidString(a.ID),
		Slug:          a.Slug,
		Title:         a.Title,
		Summary:       ptrString(a.Summary),
		ContentHtml:   html,
		CoverUrl:      ptrString(a.CoverURL),
		Status:        dto.ArticleStatus(service.ArticleStatusName(a.Status)),
		Author:        toAuthorBrief(v.Author),
		Tags:          ptrTags(toTagBriefs(v.Tags)),
		ViewCount:     int(a.ViewCount),
		LikeCount:     int(a.LikeCount),
		FavoriteCount: int(a.FavoriteCount),
		CommentCount:  int(a.CommentCount),
		ReadingTime:   ptrInt(int(a.ReadingTime)),
		AllowComment:  a.AllowComment == 1,
		// 点赞/收藏在阶段 5 接入，先恒为 false（契约允许未登录态恒 false）
		IsLiked:     false,
		IsFavorited: false,
		UpdatedAt:   ptrTime(a.UpdatedAt.UTC()),
	}
	if v.Category != nil {
		d.Category = &dto.CategoryBrief{Id: uidString(v.Category.ID), Name: v.Category.Name}
	}
	if a.PublishedAt != nil {
		t := a.PublishedAt.UTC()
		d.PublishedAt = &t
	}
	return d
}

func toArticleEdit(a *model.Article, tagIDs []uint64) dto.ArticleEdit {
	ids := make([]string, 0, len(tagIDs))
	for _, id := range tagIDs {
		ids = append(ids, uidString(id))
	}
	e := dto.ArticleEdit{
		Id:           uidString(a.ID),
		Title:        a.Title,
		Slug:         a.Slug,
		Summary:      ptrString(a.Summary),
		ContentMd:    a.ContentMD,
		CoverUrl:     ptrString(a.CoverURL),
		CategoryId:   ptrString(uidString(a.CategoryID)),
		TagIds:       ptrStrings(ids),
		Status:       dto.ArticleStatus(service.ArticleStatusName(a.Status)),
		Version:      int(a.Version),
		AllowComment: a.AllowComment == 1,
		RejectReason: ptrString(a.RejectReason),
		UpdatedAt:    a.UpdatedAt.UTC(),
	}
	return e
}

func toArticleMutation(a *model.Article) dto.ArticleMutationData {
	return dto.ArticleMutationData{
		Id:      uidString(a.ID),
		Slug:    a.Slug,
		Status:  dto.ArticleStatus(service.ArticleStatusName(a.Status)),
		Version: int(a.Version),
	}
}

func toMyArticleItem(a *model.Article) dto.MyArticleItem {
	item := dto.MyArticleItem{
		Id:           uidString(a.ID),
		Slug:         a.Slug,
		Title:        a.Title,
		Summary:      ptrString(a.Summary),
		Status:       dto.ArticleStatus(service.ArticleStatusName(a.Status)),
		Version:      int(a.Version),
		ViewCount:    int(a.ViewCount),
		LikeCount:    int(a.LikeCount),
		CommentCount: int(a.CommentCount),
		RejectReason: ptrString(a.RejectReason),
		UpdatedAt:    a.UpdatedAt.UTC(),
	}
	if a.PublishedAt != nil {
		t := a.PublishedAt.UTC()
		item.PublishedAt = &t
	}
	return item
}

// 生成代码把非 required 字段生成为指针，这里集中做适配。
func ptrString(s string) *string { return &s }

func ptrInt(i int) *int { return &i }

func ptrTags(t []dto.TagBrief) *[]dto.TagBrief { return &t }

func ptrStrings(v []string) *[]string { return &v }

func derefTags(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func ptrTime(t time.Time) *time.Time { return &t }
