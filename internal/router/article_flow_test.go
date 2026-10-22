package router_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/handler"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/markdown"
	"go-blog/internal/pkg/ratelimit"
	"go-blog/internal/repository"
	"go-blog/internal/router"
	"go-blog/internal/service"
)

// memArticles 是内存版文章存储，让生命周期测试不依赖 MySQL。
type memArticles struct {
	mu      sync.Mutex
	nextID  uint64
	data    map[uint64]*model.Article
	tags    map[uint64][]uint64
	cats    map[uint64]model.Category
	allTags map[uint64]model.Tag
}

func newMemArticles() *memArticles {
	return &memArticles{
		nextID: 1,
		data:   map[uint64]*model.Article{},
		tags:   map[uint64][]uint64{},
		cats: map[uint64]model.Category{
			1: {ID: 1, Name: "技术", Slug: "tech"},
		},
		allTags: map[uint64]model.Tag{
			5: {ID: 5, Name: "Go", Slug: "go"},
		},
	}
}

func (m *memArticles) Create(_ context.Context, a *model.Article, tagIDs []uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, exist := range m.data {
		if exist.Slug == a.Slug {
			return repository.ErrDuplicate
		}
	}
	a.ID = m.nextID
	m.nextID++
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	cp := *a
	m.data[a.ID] = &cp
	m.tags[a.ID] = append([]uint64(nil), tagIDs...)
	return nil
}

func (m *memArticles) Update(_ context.Context, a *model.Article, tagIDs, _ []uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.data[a.ID]
	if !ok || cur.Version != a.Version {
		return repository.ErrConflict
	}
	cp := *a
	cp.Version = a.Version + 1
	cp.UpdatedAt = time.Now()
	m.data[a.ID] = &cp
	m.tags[a.ID] = append([]uint64(nil), tagIDs...)
	return nil
}

func (m *memArticles) ChangeStatus(_ context.Context, id uint64, from, to uint8, extra map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.data[id]
	if !ok {
		return repository.ErrNotFound
	}
	if a.Status != from {
		return repository.ErrConflict
	}
	a.Status = to
	a.UpdatedAt = time.Now()
	if to == model.ArticlePublished && a.PublishedAt == nil {
		now := time.Now()
		a.PublishedAt = &now
	}
	if extra != nil {
		if v, ok := extra["reject_reason"].(string); ok {
			a.RejectReason = v
		}
		if v, ok := extra["offline_reason"].(string); ok {
			a.OfflineReason = v
		}
	}
	return nil
}

func (m *memArticles) SoftDelete(_ context.Context, id uint64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.data[id]
	if !ok || a.DeletedAt != nil {
		return repository.ErrNotFound
	}
	a.Status = model.ArticleDeleted
	a.DeletedAt = &at
	return nil
}

func (m *memArticles) ByID(_ context.Context, id uint64) (*model.Article, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.data[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *memArticles) ByIDs(_ context.Context, ids []uint64) ([]model.Article, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.Article, 0, len(ids))
	for _, id := range ids {
		if a, ok := m.data[id]; ok {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memArticles) BySlug(_ context.Context, slug string) (*model.Article, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.data {
		if a.Slug == slug {
			cp := *a
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memArticles) SlugExists(_ context.Context, slug string, excludeID uint64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, a := range m.data {
		if a.Slug == slug && id != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memArticles) List(_ context.Context, f repository.ListFilter) ([]model.Article, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Article
	for _, a := range m.data {
		if f.AuthorID > 0 && a.AuthorID != f.AuthorID {
			continue
		}
		if f.AuthorID == 0 && (a.Status != model.ArticlePublished || a.DeletedAt != nil) {
			continue
		}
		out = append(out, *a)
	}
	// 简化：按 id 倒序，分页由 limit 截断
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ID > out[i].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *memArticles) TagIDs(_ context.Context, articleID uint64) ([]uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uint64(nil), m.tags[articleID]...), nil
}

func (m *memArticles) TagsByArticleIDs(_ context.Context, ids []uint64) (map[uint64][]model.Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[uint64][]model.Tag{}
	for _, id := range ids {
		for _, tid := range m.tags[id] {
			if t, ok := m.allTags[tid]; ok {
				out[id] = append(out[id], t)
			}
		}
	}
	return out, nil
}

func (m *memArticles) UsersByIDs(_ context.Context, ids []uint64) (map[uint64]model.User, error) {
	out := map[uint64]model.User{}
	for _, id := range ids {
		out[id] = model.User{ID: id, Username: "u" + strconv.FormatUint(id, 10), Nickname: "作者" + strconv.FormatUint(id, 10)}
	}
	return out, nil
}

func (m *memArticles) CategoriesByIDs(_ context.Context, ids []uint64) (map[uint64]model.Category, error) {
	out := map[uint64]model.Category{}
	for _, id := range ids {
		if c, ok := m.cats[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (m *memArticles) ListCategories(context.Context) ([]model.Category, error) {
	var out []model.Category
	for _, c := range m.cats {
		out = append(out, c)
	}
	return out, nil
}

func (m *memArticles) ListTags(context.Context, int) ([]model.Tag, error) {
	var out []model.Tag
	for _, t := range m.allTags {
		out = append(out, t)
	}
	return out, nil
}

func (m *memArticles) TagsByIDs(_ context.Context, ids []uint64) (map[uint64]bool, error) {
	out := map[uint64]bool{}
	for _, id := range ids {
		if _, ok := m.allTags[id]; ok {
			out[id] = true
		}
	}
	return out, nil
}

func (m *memArticles) CategoryExists(_ context.Context, id uint64) (bool, error) {
	if id == 0 {
		return true, nil
	}
	_, ok := m.cats[id]
	return ok, nil
}

// ============================================================

func newArticleClient(t *testing.T) (*testClient, *memArticles, *jwtutil.Manager) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := &config.Config{App: config.App{Env: config.EnvTest, Addr: ":0", Version: "test"}}
	cfg.Cookie.SameSite = "lax"
	cfg.JWT.Secret = "article_test_secret_at_least_32_characters"
	cfg.JWT.Issuer = "go-blog"
	cfg.JWT.AccessTTLMin = 15
	cfg.Mail.VerifyURLBase = "http://test.local/v"
	cfg.Mail.ResetURLBase = "http://test.local/r"
	cfg.Security.LoginFailLimit = 5
	cfg.Security.LoginLockMin = 15
	cfg.Security.VerifyResendIntervalSec = 60
	cfg.Security.VerifyResendDailyLimit = 10
	cfg.Security.ResetResendIntervalSec = 60
	cfg.Security.ResetResendDailyLimit = 5

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	users := newMemUsers()
	// 作者与管理员
	require.NoError(t, users.Create(context.Background(), &model.User{
		Username: "author", Email: "author@example.com", Nickname: "作者",
		Role: 2, Status: model.StatusActive,
	}))
	require.NoError(t, users.Create(context.Background(), &model.User{
		Username: "admin", Email: "admin@example.com", Nickname: "管理员",
		Role: 3, Status: model.StatusActive,
	}))

	articles := newMemArticles()
	jwtMgr, err := jwtutil.New(cfg.JWT.Secret, cfg.JWT.Issuer, 15*time.Minute)
	require.NoError(t, err)

	cookies := authcookie.New(cfg)
	sessions := service.NewSessionStore(rdb)
	onetime := service.NewOneTimeTokenStore(rdb)
	limits := ratelimit.New(rdb)

	authSvc, err := service.NewAuthService(users, sessions, onetime, limits, &captureMailer{}, jwtMgr, cfg, log)
	require.NoError(t, err)
	userSvc := service.NewUserService(users, sessions, onetime, log)
	articleSvc := service.NewArticleService(articles, nil, markdown.New(), false, log)

	engine := router.New(cfg, log, router.Deps{
		Health:  handler.NewHealthHandler(log, nil, "test"),
		Auth:    handler.NewAuthHandler(authSvc, cookies, log),
		User:    handler.NewUserHandler(userSvc, nil, cookies),
		Article: handler.NewArticleHandler(articleSvc),
		Authn:   middleware.NewAuthenticator(jwtMgr, users, rdb, cookies),
		Cookie:  cookies,
	})

	return &testClient{t: t, handler: engine, cookies: map[string]*http.Cookie{}}, articles, jwtMgr
}

// loginAs 直接签发访问令牌，跳过登录流程（登录链路由 auth_flow_test 覆盖）。
func loginAs(t *testing.T, c *testClient, jwtMgr *jwtutil.Manager, uid uint64) {
	t.Helper()
	token, _, err := jwtMgr.Issue(uid, time.Now())
	require.NoError(t, err)
	c.cookies = map[string]*http.Cookie{
		authcookie.NameAccess: {Name: authcookie.NameAccess, Value: token},
		// CSRF 双提交：测试里两个值保持一致
		authcookie.NameCSRF: {Name: authcookie.NameCSRF, Value: "test-csrf"},
	}
}

func (c *testClient) withCSRF() map[string]string {
	return map[string]string{authcookie.HeaderCSRF: "test-csrf"}
}

// TestArticleLifecycle 覆盖 PRD 5.1 要求的文章全生命周期主链路：
// 创建 → 提交发布 → 公开可读 → 编辑（乐观锁）→ 删除 → 410。
func TestArticleLifecycle(t *testing.T) {
	c, store, jwtMgr := newArticleClient(t)
	loginAs(t, c, jwtMgr, 1)

	// 1. 创建草稿
	w, body := c.do(http.MethodPost, "/api/v1/articles", map[string]any{
		"title":       "Go 并发模型",
		"content_md":  "# 标题\n\n正文内容",
		"category_id": "1",
		"tag_ids":     []string{"5"},
	}, c.withCSRF())
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	data := body["data"].(map[string]any)
	id := data["id"].(string)
	assert.Equal(t, "draft", data["status"])
	assert.Equal(t, float64(0), data["version"])

	// 2. 草稿对匿名访客 404（不泄露存在性）
	c.cookies = map[string]*http.Cookie{}
	w, _ = c.do(http.MethodGet, "/api/v1/articles/"+id, nil, nil)
	require.Equal(t, http.StatusNotFound, w.Code, "草稿对非作者必须 404")

	// 3. 作者本人可以看草稿（重新带上作者身份）
	loginAs(t, c, jwtMgr, 1)
	w, body = c.do(http.MethodGet, "/api/v1/articles/"+id, nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "draft", body["data"].(map[string]any)["status"])

	// 4. 提交发布（审核关闭 → 直接 published）
	w, body = c.do(http.MethodPost, "/api/v1/articles/"+id+"/submit", nil, c.withCSRF())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "published", body["data"].(map[string]any)["status"])

	// 5. 匿名访客现在能读到，且拿到渲染后的 HTML
	c.cookies = map[string]*http.Cookie{}
	w, body = c.do(http.MethodGet, "/api/v1/articles/"+id, nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	detail := body["data"].(map[string]any)
	assert.Contains(t, detail["content_html"].(string), "<h1>")
	assert.Equal(t, "published", detail["status"])

	// 6. 公开列表里出现该文章
	w, body = c.do(http.MethodGet, "/api/v1/articles?limit=10", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	items := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)

	// 7. 作者编辑：版本不匹配必须 409
	loginAs(t, c, jwtMgr, 1)
	w, body = c.do(http.MethodPut, "/api/v1/articles/"+id, map[string]any{
		"title": "Go 并发模型（修订）", "content_md": "# 新正文", "version": 99,
	}, c.withCSRF())
	require.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, float64(errs.CodeEditConflict), body["code"])

	// 8. 正确版本可以更新，version 自增
	w, body = c.do(http.MethodPut, "/api/v1/articles/"+id, map[string]any{
		"title": "Go 并发模型（修订）", "content_md": "# 新正文", "category_id": "1", "version": 0,
	}, c.withCSRF())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, float64(1), body["data"].(map[string]any)["version"])

	// 9. 编辑取文能看到最新的 content_md
	w, body = c.do(http.MethodGet, "/api/v1/articles/"+id+"/edit", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "# 新正文", body["data"].(map[string]any)["content_md"])

	// 10. 管理员不能改他人正文
	loginAs(t, c, jwtMgr, 2)
	w, body = c.do(http.MethodPut, "/api/v1/articles/"+id, map[string]any{
		"title": "管理员改名", "content_md": "# 不该生效", "version": 1,
	}, c.withCSRF())
	require.Equal(t, http.StatusForbidden, w.Code, "管理员不能修改他人正文")
	assert.Equal(t, float64(errs.CodeForbidden), body["code"])

	// 11. 作者删除 → 204
	loginAs(t, c, jwtMgr, 1)
	w, _ = c.do(http.MethodDelete, "/api/v1/articles/"+id, nil, c.withCSRF())
	require.Equal(t, http.StatusNoContent, w.Code)

	// 12. 删除后公开访问 410（不是 404）
	c.cookies = map[string]*http.Cookie{}
	w, body = c.do(http.MethodGet, "/api/v1/articles/"+id, nil, nil)
	require.Equal(t, http.StatusGone, w.Code)
	assert.Equal(t, float64(errs.CodeArticleDeleted), body["code"])

	// 13. 公开列表不再包含
	w, body = c.do(http.MethodGet, "/api/v1/articles?limit=10", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, body["data"].(map[string]any)["items"])

	// 14. 存储里文章仍在（软删除），状态为 deleted
	stored, err := store.ByID(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.ArticleDeleted, stored.Status)
	assert.NotNil(t, stored.DeletedAt)
}

// TestArticleListHidesDrafts 确认未发布内容不会从公开列表泄露。
func TestArticleListHidesDrafts(t *testing.T) {
	c, _, jwtMgr := newArticleClient(t)
	loginAs(t, c, jwtMgr, 1)

	w, _ := c.do(http.MethodPost, "/api/v1/articles", map[string]any{
		"title": "草稿一篇", "content_md": "内容",
	}, c.withCSRF())
	require.Equal(t, http.StatusCreated, w.Code)

	c.cookies = map[string]*http.Cookie{}
	w, body := c.do(http.MethodGet, "/api/v1/articles", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, body["data"].(map[string]any)["items"], "草稿不能出现在公开列表")
}

// 确保内存实现满足服务层接口（编译期约束）。
var _ service.ArticleStore = (*memArticles)(nil)

// 避免未使用告警：json 在断言辅助中被间接使用
var _ = json.Marshal
