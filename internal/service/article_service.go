package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/pkg/markdown"
	"go-blog/internal/repository"
)

// ArticleStore 是文章服务所需的数据能力，便于测试替换。
type ArticleStore interface {
	Create(ctx context.Context, a *model.Article, tagIDs []uint64) error
	Update(ctx context.Context, a *model.Article, tagIDs, oldTagIDs []uint64) error
	ChangeStatus(ctx context.Context, id uint64, from, to uint8, extra map[string]any) error
	SoftDelete(ctx context.Context, id uint64, at time.Time) error
	ByID(ctx context.Context, id uint64) (*model.Article, error)
	ByIDs(ctx context.Context, ids []uint64) ([]model.Article, error)
	BySlug(ctx context.Context, slug string) (*model.Article, error)
	SlugExists(ctx context.Context, slug string, excludeID uint64) (bool, error)
	List(ctx context.Context, f repository.ListFilter) ([]model.Article, error)
	TagIDs(ctx context.Context, articleID uint64) ([]uint64, error)
	TagsByArticleIDs(ctx context.Context, ids []uint64) (map[uint64][]model.Tag, error)
	UsersByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error)
	CategoriesByIDs(ctx context.Context, ids []uint64) (map[uint64]model.Category, error)
	ListCategories(ctx context.Context) ([]model.Category, error)
	ListTags(ctx context.Context, limit int) ([]model.Tag, error)
	TagsByIDs(ctx context.Context, ids []uint64) (map[uint64]bool, error)
	CategoryExists(ctx context.Context, id uint64) (bool, error)
}

// ArticleInput 是创建/更新文章的入参。
type ArticleInput struct {
	Title        string
	Slug         string
	Summary      string
	ContentMD    string
	CoverURL     string
	CategoryID   uint64
	TagIDs       []uint64
	AllowComment bool
	Version      uint32 // 更新时必填
}

// ArticleService 实现文章的生命周期管理。
type ArticleService struct {
	store ArticleStore
	queue IndexQueue
	md    *markdown.Renderer
	cfg   articlePolicy
	// settings 非空时，审核开关每次实时读取（管理端改配置立即生效）；
	// 为 nil 时回退到构造时注入的静态开关（单测场景）。
	settings SettingReader
	log      *slog.Logger
	now      func() time.Time
}

// articlePolicy 是文章相关的业务开关。
type articlePolicy struct {
	ReviewEnabled bool
	MaxTags       int
}

// NewArticleService 构造 ArticleService。
func NewArticleService(store ArticleStore, queue IndexQueue, md *markdown.Renderer, reviewEnabled bool, log *slog.Logger) *ArticleService {
	if queue == nil {
		queue = NopIndexQueue{}
	}
	return &ArticleService{
		store: store,
		queue: queue,
		md:    md,
		cfg:   articlePolicy{ReviewEnabled: reviewEnabled, MaxTags: 5},
		log:   log,
		now:   time.Now,
	}
}

// SetSettingReader 注入配置读取器，使审核开关实时生效（由装配层调用）。
func (s *ArticleService) SetSettingReader(r SettingReader) {
	s.settings = r
}

// reviewOn 返回当前审核开关：优先读配置，失败或未配置时用静态值。
func (s *ArticleService) reviewOn(ctx context.Context) bool {
	if s.settings == nil {
		return s.cfg.ReviewEnabled
	}
	on, err := s.settings.GetBool(ctx, "review.article", s.cfg.ReviewEnabled)
	if err != nil {
		s.log.Warn("读取审核开关失败，回退静态配置", "err", err)
		return s.cfg.ReviewEnabled
	}
	return on
}

// Create 创建草稿。
func (s *ArticleService) Create(ctx context.Context, authorID uint64, role authz.Role, in ArticleInput) (*model.Article, error) {
	if !role.AtLeast(authz.RoleAuthor) {
		return nil, errs.Forbidden("需要作者权限")
	}
	if err := s.validateInput(ctx, in, 0); err != nil {
		return nil, err
	}

	slug, err := s.resolveSlug(ctx, in.Slug, in.Title, 0)
	if err != nil {
		return nil, err
	}

	html, err := s.md.Render(in.ContentMD)
	if err != nil {
		return nil, errs.Internal(err)
	}
	words, reading := markdown.Stats(in.ContentMD)
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = markdown.Summarize(in.ContentMD, 120)
	}

	a := &model.Article{
		AuthorID:     authorID,
		CategoryID:   in.CategoryID,
		Title:        strings.TrimSpace(in.Title),
		Slug:         slug,
		Summary:      summary,
		ContentMD:    in.ContentMD,
		ContentHTML:  &html,
		CoverURL:     in.CoverURL,
		Status:       model.ArticleDraft,
		AllowComment: boolToUint8(in.AllowComment),
		WordCount:    uint32(words),
		ReadingTime:  uint32(reading),
	}

	if err := s.store.Create(ctx, a, in.TagIDs); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return nil, errs.Internal(err)
	}
	return a, nil
}

// Update 更新文章内容。仅作者本人可改，管理员也不行（PRD 3.2.1）。
func (s *ArticleService) Update(ctx context.Context, id, actorID uint64, role authz.Role, in ArticleInput) (*model.Article, error) {
	a, err := s.store.ByID(ctx, id)
	if err != nil {
		return nil, mapArticleErr(err)
	}
	if err := s.assertOwnership(a, actorID, role); err != nil {
		return nil, err
	}
	if a.Status == model.ArticleDeleted {
		return nil, errs.New(errs.CodeArticleDeleted, 410, "文章已删除")
	}
	// version=0 是合法初始版本，因此这里只做相等比较，不判断"是否缺失"
	if in.Version != a.Version {
		return nil, errs.New(errs.CodeEditConflict, 409, "文章已被修改，请刷新后重试")
	}
	if err := s.validateInput(ctx, in, id); err != nil {
		return nil, err
	}

	slug, err := s.resolveSlug(ctx, in.Slug, in.Title, id)
	if err != nil {
		return nil, err
	}
	html, err := s.md.Render(in.ContentMD)
	if err != nil {
		return nil, errs.Internal(err)
	}
	words, reading := markdown.Stats(in.ContentMD)

	oldTagIDs, err := s.store.TagIDs(ctx, id)
	if err != nil {
		return nil, errs.Internal(err)
	}

	// 下架文章被作者修改后回到草稿，必须重新走提交流程（PRD 3.2）
	if a.Status == model.ArticleOffline {
		if _, err := Decide(TransitionInput{
			From: a.Status, Action: ActionEditOffline, Role: role,
		}); err != nil {
			return nil, err
		}
		if err := s.store.ChangeStatus(ctx, id, a.Status, model.ArticleDraft, nil); err != nil {
			return nil, mapArticleErr(err)
		}
		a.Status = model.ArticleDraft
	}

	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		summary = markdown.Summarize(in.ContentMD, 120)
	}
	a.Title = strings.TrimSpace(in.Title)
	a.Slug = slug
	a.Summary = summary
	a.ContentMD = in.ContentMD
	a.ContentHTML = &html
	a.CoverURL = in.CoverURL
	a.CategoryID = in.CategoryID
	a.AllowComment = boolToUint8(in.AllowComment)
	a.WordCount = uint32(words)
	a.ReadingTime = uint32(reading)

	if err := s.store.Update(ctx, a, in.TagIDs, oldTagIDs); err != nil {
		switch {
		case errors.Is(err, repository.ErrConflict):
			return nil, errs.New(errs.CodeEditConflict, 409, "文章已被修改，请刷新后重试")
		case errors.Is(err, repository.ErrDuplicate):
			return nil, errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		default:
			return nil, errs.Internal(err)
		}
	}
	a.Version++
	// 编辑已生效，索引同步失败不能回滚编辑（PRD 7.4）：
	// 投递失败只记日志，由 search_sync_status + 对账任务兜底。
	s.enqueueIndex(ctx, id)
	return a, nil
}

// GetForEdit 取正文供编辑器回填，仅作者本人。
func (s *ArticleService) GetForEdit(ctx context.Context, id, actorID uint64, role authz.Role) (*model.Article, []uint64, error) {
	a, err := s.store.ByID(ctx, id)
	if err != nil {
		return nil, nil, mapArticleErr(err)
	}
	if err := s.assertOwnership(a, actorID, role); err != nil {
		return nil, nil, err
	}
	if a.Status == model.ArticleDeleted {
		return nil, nil, errs.New(errs.CodeArticleDeleted, 410, "文章已删除")
	}
	tagIDs, err := s.store.TagIDs(ctx, id)
	if err != nil {
		return nil, nil, errs.Internal(err)
	}
	return a, tagIDs, nil
}

// View 是对外详情结果，携带作者/分类/标签。
type ArticleView struct {
	Article  *model.Article
	Author   model.User
	Category *model.Category
	Tags     []model.Tag
}

// GetDetail 按 slug 或数字 ID 读取详情，并执行可见性判定（PRD 2.2.1）。
//
// viewerID 为 0 表示未登录；管理员在公开接口上没有豁免——
// 非公开文章对管理员同样 404，只能走后台接口。
func (s *ArticleService) GetDetail(ctx context.Context, key string, viewerID uint64) (*ArticleView, error) {
	a, err := s.findByKey(ctx, key)
	if err != nil {
		return nil, err
	}

	switch AudienceOf(a.Status) {
	case AudienceGone:
		return nil, errs.New(errs.CodeArticleDeleted, 410, "文章已删除")
	case AudienceAuthor:
		if viewerID == 0 || a.AuthorID != viewerID {
			// 关键：返回 404 而不是 403，不泄露"这篇文章存在"
			return nil, errs.NotFound("文章不存在")
		}
	}

	return s.assemble(ctx, a)
}

// GetForAuthor 是作者查看自己文章详情（可含非公开状态）。
func (s *ArticleService) GetForAuthor(ctx context.Context, id, actorID uint64, role authz.Role) (*ArticleView, error) {
	a, err := s.store.ByID(ctx, id)
	if err != nil {
		return nil, mapArticleErr(err)
	}
	if err := s.assertOwnership(a, actorID, role); err != nil {
		return nil, err
	}
	if a.Status == model.ArticleDeleted {
		return nil, errs.New(errs.CodeArticleDeleted, 410, "文章已删除")
	}
	return s.assemble(ctx, a)
}

// ListPublic 公开列表。
func (s *ArticleService) ListPublic(ctx context.Context, categoryID, tagID *uint64, sort, cursor string, limit int) ([]*ArticleView, string, bool, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	f := repository.ListFilter{
		CategoryID: categoryID,
		TagID:      derefU64(tagID),
		Sort:       sort,
		Limit:      limit + 1, // 多取一条判断是否有下一页
	}
	if err := decodeCursor(cursor, &f); err != nil {
		return nil, "", false, badRequest("游标无效", "cursor")
	}

	rows, err := s.store.List(ctx, f)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	views, err := s.assembleMany(ctx, rows)
	if err != nil {
		return nil, "", false, err
	}

	next := ""
	if hasMore && len(rows) > 0 {
		next = encodeCursor(f.Sort, rows[len(rows)-1])
	}
	return views, next, hasMore, nil
}

// ListMine 作者后台列表。
func (s *ArticleService) ListMine(ctx context.Context, authorID uint64, statuses []uint8, cursor string, limit int) ([]*ArticleView, string, bool, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	f := repository.ListFilter{
		AuthorID: authorID,
		Statuses: statuses,
		Sort:     "latest",
		Limit:    limit + 1,
	}
	if err := decodeCursor(cursor, &f); err != nil {
		return nil, "", false, badRequest("游标无效", "cursor")
	}

	rows, err := s.store.List(ctx, f)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	// 作者后台不需要作者信息，直接组装
	views := make([]*ArticleView, 0, len(rows))
	for i := range rows {
		views = append(views, &ArticleView{Article: &rows[i]})
	}
	next := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = encodeCursor("mine", last)
	}
	return views, next, hasMore, nil
}

// ByID 按 ID 取文章（不区分状态，含下架/已删除），供其他 service 读取文章本体，
// 例如管理端详情、举报处置定位对象。公开访问的可见性校验在各调用方自行处理。
func (s *ArticleService) ByID(ctx context.Context, id uint64) (*model.Article, error) {
	return s.store.ByID(ctx, id)
}

// TagIDsOf 取文章的标签 ID 集合（供管理端详情等场景）。
func (s *ArticleService) TagIDsOf(ctx context.Context, articleID uint64) ([]uint64, error) {
	return s.store.TagIDs(ctx, articleID)
}

// UsersByIDs 批量取用户（供其他 service 组装作者信息）。
func (s *ArticleService) UsersByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error) {
	return s.store.UsersByIDs(ctx, ids)
}

// ListByIDs 按 ID 集合返回公开可读的文章视图，保持传入顺序。
//
// 供收藏列表等"先拿到 ID、再补文章"的场景复用，避免重复写组装逻辑。
func (s *ArticleService) ListByIDs(ctx context.Context, ids []uint64) ([]*ArticleView, error) {
	if len(ids) == 0 {
		return []*ArticleView{}, nil
	}
	rows, err := s.store.ByIDs(ctx, ids)
	if err != nil {
		return nil, errs.Internal(err)
	}
	views, err := s.assembleMany(ctx, rows)
	if err != nil {
		return nil, err
	}
	// assembleMany 不保证顺序，按传入 ID 重排
	byID := make(map[uint64]*ArticleView, len(views))
	for _, v := range views {
		byID[v.Article.ID] = v
	}
	out := make([]*ArticleView, 0, len(ids))
	for _, id := range ids {
		if v, ok := byID[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *ArticleService) Transition(ctx context.Context, id, actorID uint64, role authz.Role, action ArticleAction, reason string) (*model.Article, error) {
	a, err := s.store.ByID(ctx, id)
	if err != nil {
		return nil, mapArticleErr(err)
	}
	// 管理员审核/下架不需要是作者本人；作者操作必须是自己
	if role != authz.RoleAdmin {
		if err := s.assertOwnership(a, actorID, role); err != nil {
			return nil, err
		}
	}

	res, err := Decide(TransitionInput{
		From:     a.Status,
		Action:   action,
		Role:     role,
		ReviewOn: s.reviewOn(ctx),
		Reason:   reason,
	})
	if err != nil {
		return nil, err
	}

	extra := map[string]any{}
	switch action {
	case ActionReject:
		extra["reject_reason"] = reason
	case ActionOffline:
		extra["offline_reason"] = reason
	case ActionReonline:
		// 重新上架清空下架理由，否则详情页会残留旧理由
		extra["offline_reason"] = ""
	}

	if err := s.store.ChangeStatus(ctx, id, a.Status, res.To, extra); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, errs.New(errs.CodeInvalidArticleState, 409, "文章状态已变化，请刷新后重试")
		}
		return nil, mapArticleErr(err)
	}

	a.Status = res.To
	if res.To == model.ArticlePublished && a.PublishedAt == nil {
		now := s.now()
		a.PublishedAt = &now
	}
	// 发布要入索引，下架/归档要出索引——统一交给 Sync 判断，
	// 调用方不需要知道当前状态对应哪种动作。
	s.enqueueIndex(ctx, id)
	return a, nil
}

// Delete 软删除。作者只能删自己的，管理员可删任意文章。
func (s *ArticleService) Delete(ctx context.Context, id, actorID uint64, role authz.Role) error {
	a, err := s.store.ByID(ctx, id)
	if err != nil {
		return mapArticleErr(err)
	}
	if role != authz.RoleAdmin {
		if err := s.assertOwnership(a, actorID, role); err != nil {
			return err
		}
	}
	if _, err := Decide(TransitionInput{From: a.Status, Action: ActionDelete, Role: role}); err != nil {
		return err
	}
	if err := s.store.SoftDelete(ctx, id, s.now()); err != nil {
		return mapArticleErr(err)
	}
	s.enqueueUnindex(ctx, id)
	return nil
}

// ListCategories 返回分类列表。
func (s *ArticleService) ListCategories(ctx context.Context) ([]model.Category, error) {
	out, err := s.store.ListCategories(ctx)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// ListTags 返回标签列表。
func (s *ArticleService) ListTags(ctx context.Context) ([]model.Tag, error) {
	out, err := s.store.ListTags(ctx, 100)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// ---- 内部工具 ----

// assertOwnership 校验资源归属。管理员同样不能改他人正文（PRD 3.2.1）。
// enqueueIndex 投递索引同步任务。
//
// 一定是"事务已提交"之后调用：任务先于提交入队会读不到新数据。
func (s *ArticleService) enqueueIndex(ctx context.Context, id uint64) {
	if err := s.queue.EnqueueIndex(ctx, id); err != nil {
		s.log.Error("投递索引任务失败，等待对账修复", "article_id", id, "err", err)
	}
}

// enqueueUnindex 投递索引删除任务。
func (s *ArticleService) enqueueUnindex(ctx context.Context, id uint64) {
	if err := s.queue.EnqueueUnindex(ctx, id); err != nil {
		s.log.Error("投递索引删除任务失败，等待对账修复", "article_id", id, "err", err)
	}
}

func (s *ArticleService) assertOwnership(a *model.Article, actorID uint64, role authz.Role) error {
	if a.AuthorID == actorID && role.AtLeast(authz.RoleAuthor) {
		return nil
	}
	if role == authz.RoleAdmin {
		return errs.Forbidden("管理员不能修改他人文章正文，请使用下架或删除")
	}
	return errs.Forbidden("只能操作自己的文章")
}

func (s *ArticleService) validateInput(ctx context.Context, in ArticleInput, excludeID uint64) error {
	title := strings.TrimSpace(in.Title)
	if title == "" || len([]rune(title)) > 200 {
		return badRequest("标题需为 1–200 个字符", "title")
	}
	if strings.TrimSpace(in.ContentMD) == "" {
		return badRequest("正文不能为空", "content_md")
	}
	if len([]rune(in.Summary)) > 512 {
		return badRequest("摘要不能超过 512 个字符", "summary")
	}
	if len(in.TagIDs) > s.cfg.MaxTags {
		return errs.New(errs.CodeTooManyTags, 400, fmt.Sprintf("标签最多 %d 个", s.cfg.MaxTags))
	}
	if in.Slug != "" {
		if err := validateSlug(in.Slug); err != nil {
			return err
		}
	}

	ok, err := s.store.CategoryExists(ctx, in.CategoryID)
	if err != nil {
		return errs.Internal(err)
	}
	if !ok {
		return badRequest("分类不存在", "category_id")
	}

	if len(in.TagIDs) > 0 {
		found, err := s.store.TagsByIDs(ctx, in.TagIDs)
		if err != nil {
			return errs.Internal(err)
		}
		for _, id := range in.TagIDs {
			if !found[id] {
				return badRequest("标签不存在", "tag_ids")
			}
		}
	}
	return nil
}

// resolveSlug 决定最终 slug：显式指定则校验唯一；留空则按标题生成并兜底去重。
func (s *ArticleService) resolveSlug(ctx context.Context, given, title string, excludeID uint64) (string, error) {
	if given != "" {
		exists, err := s.store.SlugExists(ctx, given, excludeID)
		if err != nil {
			return "", errs.Internal(err)
		}
		if exists {
			return "", errs.New(errs.CodeSlugConflict, 409, "slug 已被占用")
		}
		return given, nil
	}

	base := markdown.Slugify(title)
	// 中文标题去掉汉字后可能只剩一两个字母（如"Go 并发模型"→"go"），
	// 这种 slug 既不可读也容易撞车，统一走时间戳兜底。
	if len(base) < 3 {
		// 中文标题生成不出 ASCII slug，用时间戳兜底：稳定、可读性差但不会冲突
		base = "post-" + strconv.FormatInt(s.now().Unix(), 36)
	}
	candidate := base
	for i := 2; i <= 20; i++ {
		exists, err := s.store.SlugExists(ctx, candidate, excludeID)
		if err != nil {
			return "", errs.Internal(err)
		}
		if !exists {
			return candidate, nil
		}
		candidate = base + "-" + strconv.Itoa(i)
	}
	return "", errs.New(errs.CodeSlugConflict, 409, "无法生成唯一 slug，请手动指定")
}

// findByKey 支持 slug 与纯数字 ID 混用（TECH_DESIGN 13 章路径约定）。
func (s *ArticleService) findByKey(ctx context.Context, key string) (*model.Article, error) {
	if id, err := strconv.ParseUint(key, 10, 64); err == nil && id > 0 {
		a, err := s.store.ByID(ctx, id)
		if err != nil {
			return nil, mapArticleErr(err)
		}
		return a, nil
	}
	a, err := s.store.BySlug(ctx, key)
	if err != nil {
		return nil, mapArticleErr(err)
	}
	return a, nil
}

func (s *ArticleService) assemble(ctx context.Context, a *model.Article) (*ArticleView, error) {
	views, err := s.assembleMany(ctx, []model.Article{*a})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// assembleMany 批量补齐作者/分类/标签，避免 N+1 查询。
func (s *ArticleService) assembleMany(ctx context.Context, rows []model.Article) ([]*ArticleView, error) {
	if len(rows) == 0 {
		return []*ArticleView{}, nil
	}

	articleIDs := make([]uint64, 0, len(rows))
	authorIDs := make([]uint64, 0, len(rows))
	categoryIDs := make([]uint64, 0, len(rows))
	seenAuthor := map[uint64]bool{}
	seenCat := map[uint64]bool{}

	for i := range rows {
		articleIDs = append(articleIDs, rows[i].ID)
		if !seenAuthor[rows[i].AuthorID] {
			seenAuthor[rows[i].AuthorID] = true
			authorIDs = append(authorIDs, rows[i].AuthorID)
		}
		if rows[i].CategoryID != 0 && !seenCat[rows[i].CategoryID] {
			seenCat[rows[i].CategoryID] = true
			categoryIDs = append(categoryIDs, rows[i].CategoryID)
		}
	}

	authors, err := s.store.UsersByIDs(ctx, authorIDs)
	if err != nil {
		return nil, errs.Internal(err)
	}
	categories, err := s.store.CategoriesByIDs(ctx, categoryIDs)
	if err != nil {
		return nil, errs.Internal(err)
	}
	tags, err := s.store.TagsByArticleIDs(ctx, articleIDs)
	if err != nil {
		return nil, errs.Internal(err)
	}

	out := make([]*ArticleView, 0, len(rows))
	for i := range rows {
		v := &ArticleView{Article: &rows[i], Author: authors[rows[i].AuthorID], Tags: tags[rows[i].ID]}
		if c, ok := categories[rows[i].CategoryID]; ok {
			cc := c
			v.Category = &cc
		}
		out = append(out, v)
	}
	return out, nil
}

func mapArticleErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errs.NotFound("文章不存在")
	}
	return errs.Internal(err)
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func derefU64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// ---- 游标 ----

// encodeCursor 生成不透明游标。前端不得解析，格式可随时调整。
func encodeCursor(sort string, a model.Article) string {
	var raw string
	switch sort {
	case "hot":
		raw = fmt.Sprintf("v:%d:%d", a.ViewCount, a.ID)
	case "mine":
		raw = fmt.Sprintf("m:%d:%d", a.UpdatedAt.UnixMilli(), a.ID)
	default:
		published := int64(0)
		if a.PublishedAt != nil {
			published = a.PublishedAt.UnixMilli()
		}
		raw = fmt.Sprintf("t:%d:%d", published, a.ID)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor 解析游标并填充过滤条件。空游标表示第一页。
func decodeCursor(cursor string, f *repository.ListFilter) error {
	if strings.TrimSpace(cursor) == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 {
		return fmt.Errorf("游标格式非法")
	}
	val, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return err
	}

	f.CursorID = id
	switch parts[0] {
	case "v":
		f.CursorView = uint64(val)
		f.Sort = "hot"
	case "t":
		t := time.UnixMilli(val)
		f.CursorTime = &t
	case "m":
		t := time.UnixMilli(val)
		f.CursorTime = &t
	default:
		return fmt.Errorf("未知游标类型")
	}
	return nil
}
