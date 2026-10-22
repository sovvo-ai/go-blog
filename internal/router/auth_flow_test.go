package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	"go-blog/internal/mailer"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/ratelimit"
	"go-blog/internal/repository"
	"go-blog/internal/router"
	"go-blog/internal/service"
)

// ============================================================
// 内存实现：让集成测试跑在 httptest 上，不需要 MySQL
// ============================================================

type memUsers struct {
	mu     sync.Mutex
	nextID uint64
	data   map[uint64]*model.User
}

func newMemUsers() *memUsers {
	return &memUsers{nextID: 1, data: map[uint64]*model.User{}}
}

func (m *memUsers) Create(_ context.Context, u *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, exist := range m.data {
		if exist.Username == u.Username || exist.Email == u.Email {
			return repository.ErrDuplicate
		}
	}
	u.ID = m.nextID
	m.nextID++
	u.CreatedAt = time.Now()
	cp := *u
	m.data[u.ID] = &cp
	return nil
}

func (m *memUsers) ByID(_ context.Context, id uint64) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.data[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *memUsers) ByAccount(_ context.Context, account string) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.data {
		if u.Username == account || u.Email == account {
			cp := *u
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memUsers) ByEmail(_ context.Context, email string) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.data {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memUsers) ExistsUsername(_ context.Context, username string) (bool, error) {
	u, err := m.ByAccount(context.Background(), username)
	return u != nil && err == nil && u.Username == username, nil
}

func (m *memUsers) ExistsEmail(_ context.Context, email string) (bool, error) {
	_, err := m.ByEmail(context.Background(), email)
	return err == nil, nil
}

func (m *memUsers) MarkEmailVerified(_ context.Context, id uint64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.data[id]
	if !ok {
		return repository.ErrNotFound
	}
	u.Status = model.StatusActive
	u.EmailVerifiedAt = &at
	return nil
}

func (m *memUsers) TouchLogin(_ context.Context, id uint64, at time.Time, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.data[id]; ok {
		u.LastLoginAt = &at
		u.LastLoginIP = ip
	}
	return nil
}

func (m *memUsers) UpdatePassword(_ context.Context, id uint64, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.data[id]
	if !ok {
		return repository.ErrNotFound
	}
	u.PasswordHash = hash
	return nil
}

func (m *memUsers) UpdateProfile(_ context.Context, id uint64, nickname, bio string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.data[id]
	if !ok {
		return repository.ErrNotFound
	}
	u.Nickname, u.Bio = nickname, bio
	return nil
}

func (m *memUsers) PublishedArticleCount(context.Context, uint64) (int64, error) { return 0, nil }

// captureMailer 捕获邮件正文，测试从中取出验证/重置链接。
type captureMailer struct {
	mu   sync.Mutex
	msgs []mailer.Message
}

func (c *captureMailer) Send(_ context.Context, msg mailer.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
	return nil
}

func (c *captureMailer) last() mailer.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return mailer.Message{}
	}
	return c.msgs[len(c.msgs)-1]
}

func (c *captureMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

var tokenPattern = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func (c *captureMailer) lastToken(t *testing.T) string {
	t.Helper()
	m := tokenPattern.FindStringSubmatch(c.last().Text)
	require.Len(t, m, 2, "邮件正文里没有找到令牌: %s", c.last().Text)
	return m[1]
}

// ============================================================
// 测试客户端：手动维护 Cookie，模拟浏览器行为
// ============================================================

type testClient struct {
	t       *testing.T
	handler http.Handler
	cookies map[string]*http.Cookie
}

func (c *testClient) do(method, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, ck := range c.cookies {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}

	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, req)

	// 记录服务端下发的 Cookie（含 MaxAge=-1 的删除指令）
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
			continue
		}
		c.cookies[ck.Name] = ck
	}

	var payload map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &payload)
	}
	return w, payload
}

func (c *testClient) csrf() string {
	if ck, ok := c.cookies[authcookie.NameCSRF]; ok {
		return ck.Value
	}
	return ""
}

// ============================================================
// 装配：真实 handler + 真实中间件 + 内存存储 + miniredis
// ============================================================

func newIntegrationClient(t *testing.T) (*testClient, *memUsers, *captureMailer) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := &config.Config{
		App: config.App{Env: config.EnvTest, Addr: ":0", Version: "test"},
	}
	cfg.Cookie.SameSite = "lax"
	cfg.JWT.Secret = "integration_test_secret_32_characters_ok"
	cfg.JWT.Issuer = "go-blog"
	cfg.JWT.AccessTTLMin = 15
	cfg.Mail.Driver = "console"
	cfg.Mail.VerifyURLBase = "http://test.local/verify-email"
	cfg.Mail.ResetURLBase = "http://test.local/reset-password"
	cfg.Security.LoginFailLimit = 5
	cfg.Security.LoginLockMin = 15
	cfg.Security.VerifyResendIntervalSec = 60
	cfg.Security.VerifyResendDailyLimit = 10
	cfg.Security.ResetResendIntervalSec = 60
	cfg.Security.ResetResendDailyLimit = 5

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	users := newMemUsers()
	mail := &captureMailer{}

	jwtMgr, err := jwtutil.New(cfg.JWT.Secret, cfg.JWT.Issuer, 15*time.Minute)
	require.NoError(t, err)

	cookies := authcookie.New(cfg)
	sessions := service.NewSessionStore(rdb)
	onetime := service.NewOneTimeTokenStore(rdb)
	limits := ratelimit.New(rdb)

	authSvc, err := service.NewAuthService(users, sessions, onetime, limits, mail, jwtMgr, cfg, log)
	require.NoError(t, err)
	userSvc := service.NewUserService(users, sessions, onetime, log)

	authn := middleware.NewAuthenticator(jwtMgr, users, rdb, cookies)

	engine := router.New(cfg, log, router.Deps{
		Health: handler.NewHealthHandler(log, nil, "test"),
		Auth:   handler.NewAuthHandler(authSvc, cookies, log),
		User:   handler.NewUserHandler(userSvc, nil, cookies),
		Authn:  authn,
		Cookie: cookies,
	})

	return &testClient{t: t, handler: engine, cookies: map[string]*http.Cookie{}}, users, mail
}

// ============================================================
// 主链路：注册 → 验证 → 登录 → 资料 → 刷新 → 退出
// ============================================================

func TestAuthFlow_EndToEnd(t *testing.T) {
	c, users, mail := newIntegrationClient(t)

	const (
		username = "integration_user"
		email    = "integration@example.com"
		passwd   = "abc12345"
	)

	// 1. 注册：创建 pending_verify 账号，不发令牌
	w, body := c.do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": passwd,
	}, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, 0, int(body["code"].(float64)))
	data := body["data"].(map[string]any)
	assert.Equal(t, "pending_verify", data["status"])
	assert.Empty(t, c.cookies, "注册不应下发任何 Cookie")

	// 2. 验证邮件已发出，取出链接里的令牌
	require.Equal(t, 1, mail.count())
	token := mail.lastToken(t)

	// 3. 未验证不能登录
	w, body = c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"account": username, "password": passwd,
	}, nil)
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, float64(errs.CodeNotVerified), body["code"])

	// 4. 验证邮箱
	w, _ = c.do(http.MethodGet, "/api/v1/auth/verify-email?token="+token, nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 5. 同一令牌不能再消费
	w, body = c.do(http.MethodGet, "/api/v1/auth/verify-email?token="+token, nil, nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, float64(errs.CodeVerifyExpired), body["code"])

	// 6. 登录成功：三个 Cookie 齐全，Path 正确
	w, body = c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"account": email, "password": passwd,
	}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	access, ok := c.cookies[authcookie.NameAccess]
	require.True(t, ok, "缺少 access_token Cookie")
	assert.Equal(t, "/", access.Path)
	assert.True(t, access.HttpOnly)

	refresh, ok := c.cookies[authcookie.NameRefresh]
	require.True(t, ok, "缺少 refresh_token Cookie")
	assert.Equal(t, authcookie.RefreshPath, refresh.Path)

	csrfCookie, ok := c.cookies[authcookie.NameCSRF]
	require.True(t, ok, "缺少 csrf_token Cookie")
	assert.False(t, csrfCookie.HttpOnly, "csrf_token 必须可被前端读取")
	assert.Equal(t, "/", csrfCookie.Path)

	loginData := body["data"].(map[string]any)
	assert.Equal(t, csrfCookie.Value, loginData["csrf_token"])
	csrfValue := csrfCookie.Value

	// 7. 读取当前用户
	w, body = c.do(http.MethodGet, "/api/v1/users/me", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	me := body["data"].(map[string]any)
	assert.Equal(t, username, me["username"])
	assert.Equal(t, "active", me["status"])

	// 8. 写接口必须带 CSRF
	w, body = c.do(http.MethodPatch, "/api/v1/users/me", map[string]string{"nickname": "新昵称"}, nil)
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, float64(errs.CodeCSRFInvalid), body["code"])

	w, body = c.do(http.MethodPatch, "/api/v1/users/me", map[string]string{"nickname": "新昵称"},
		map[string]string{authcookie.HeaderCSRF: c.csrf()})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "新昵称", body["data"].(map[string]any)["nickname"])

	// 9. 刷新：换发新令牌
	oldRefresh := refresh.Value
	w, _ = c.do(http.MethodPost, "/api/v1/auth/refresh", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	newRefresh := c.cookies[authcookie.NameRefresh]
	require.NotNil(t, newRefresh)
	assert.NotEqual(t, oldRefresh, newRefresh.Value, "刷新必须轮换令牌")

	// 10. 旧 refresh 不能再用
	c.cookies[authcookie.NameRefresh] = &http.Cookie{Name: authcookie.NameRefresh, Value: oldRefresh}
	w, body = c.do(http.MethodPost, "/api/v1/auth/refresh", nil, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, float64(errs.CodeUnauthorized), body["code"])

	// 失败的刷新会清空 Cookie（正确行为），这里按浏览器重新登录后的状态恢复
	c.cookies[authcookie.NameRefresh] = newRefresh
	c.cookies[authcookie.NameCSRF] = &http.Cookie{Name: authcookie.NameCSRF, Value: csrfValue}

	// 11. 退出登录：必须带 CSRF（本次修复点）
	w, body = c.do(http.MethodPost, "/api/v1/auth/logout", nil, nil)
	require.Equal(t, http.StatusForbidden, w.Code, "logout 缺 CSRF 应被拒绝")
	assert.Equal(t, float64(errs.CodeCSRFInvalid), body["code"])

	w, _ = c.do(http.MethodPost, "/api/v1/auth/logout", nil, map[string]string{authcookie.HeaderCSRF: c.csrf()})
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Empty(t, c.cookies, "退出后三个 Cookie 都应被清除")

	// 12. 退出后 refresh 失效
	c.cookies[authcookie.NameRefresh] = newRefresh
	w, _ = c.do(http.MethodPost, "/api/v1/auth/refresh", nil, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// 13. 账号状态在存储层确实变成 active
	stored, err := users.ByID(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, model.StatusActive, stored.Status)
	assert.NotNil(t, stored.EmailVerifiedAt)
}

func TestAuthFlow_LoginLockAfterFailures(t *testing.T) {
	c, _, _ := newIntegrationClient(t)

	const (
		username = "lock_user"
		email    = "lock@example.com"
		passwd   = "abc12345"
	)
	w, _ := c.do(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": username, "email": email, "password": passwd,
	}, nil)
	require.Equal(t, http.StatusCreated, w.Code)

	for i := 0; i < 5; i++ {
		w, _ = c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
			"account": username, "password": "wrongpass1",
		}, nil)
		require.Equal(t, http.StatusUnauthorized, w.Code, "第 %d 次应返回凭证错误", i+1)
	}

	// 第 6 次：即使密码正确也被锁定
	w, body := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"account": username, "password": passwd,
	}, nil)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, float64(errs.CodeAccountLocked), body["code"])
}

// TestAuthFlow_PublicProfileExcludesSensitiveFields 保证公开主页不泄露邮箱与状态。
func TestAuthFlow_PublicProfileExcludesSensitiveFields(t *testing.T) {
	c, users, _ := newIntegrationClient(t)

	require.NoError(t, users.Create(context.Background(), &model.User{
		Username: "public_user",
		Email:    "public@example.com",
		Nickname: "公开用户",
		Role:     uint8(1),
		Status:   model.StatusActive,
	}))

	w, body := c.do(http.MethodGet, "/api/v1/users/1", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)

	data := body["data"].(map[string]any)
	_, hasEmail := data["email"]
	_, hasStatus := data["status"]
	assert.False(t, hasEmail, "公开主页不能返回 email")
	assert.False(t, hasStatus, "公开主页不能返回 status")
	assert.Equal(t, "public_user", data["username"])
	assert.Equal(t, strconv.Itoa(0), fmt.Sprint(data["article_count"]))
}
