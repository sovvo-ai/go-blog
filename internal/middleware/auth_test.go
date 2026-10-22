package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/response"
	"go-blog/internal/repository"
)

// fakeUsers 是可控的用户数据源。
type fakeUsers struct {
	user *model.User
	err  error
}

func (f *fakeUsers) ByID(_ context.Context, _ uint64) (*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

type authEnv struct {
	router  *gin.Engine
	users   *fakeUsers
	authn   *middleware.Authenticator
	jwt     *jwtutil.Manager
	cookies *authcookie.Manager
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	jwtMgr, err := jwtutil.New("middleware_test_secret_32_characters", "go-blog", 15*time.Minute)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Cookie.SameSite = "lax"
	cookies := authcookie.New(cfg)

	users := &fakeUsers{user: &model.User{
		ID:     7,
		Status: model.StatusActive,
		Role:   2, // author
	}}

	authn := middleware.NewAuthenticator(jwtMgr, users, rdb, cookies)

	r := gin.New()
	r.GET("/open", func(c *gin.Context) { response.OK(c, nil) })
	r.GET("/me", authn.Require(), func(c *gin.Context) {
		response.OK(c, gin.H{"uid": middleware.UIDOf(c), "role": middleware.RoleOf(c).String()})
	})
	r.GET("/admin", authn.Require(), authn.RequireRole(3), func(c *gin.Context) { response.OK(c, nil) })
	r.POST("/write", middleware.CSRF(cookies, map[string]struct{}{"/api/v1/auth/login": {}}), func(c *gin.Context) {
		response.OK(c, nil)
	})
	return &authEnv{router: r, users: users, authn: authn, jwt: jwtMgr, cookies: cookies}
}

// request 发一个请求，cookies 为要附加的 Cookie 列表。
func (e *authEnv) request(t *testing.T, method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *authEnv) accessCookie(t *testing.T, uid uint64) *http.Cookie {
	t.Helper()
	token, _, err := e.jwt.Issue(uid, time.Now())
	require.NoError(t, err)
	return &http.Cookie{Name: authcookie.NameAccess, Value: token}
}

func TestRequire_NoCookie(t *testing.T) {
	env := newAuthEnv(t)
	w := env.request(t, http.MethodGet, "/me")

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeUnauthorized, body.Code)
}

func TestRequire_InvalidToken(t *testing.T) {
	env := newAuthEnv(t)
	w := env.request(t, http.MethodGet, "/me", &http.Cookie{Name: authcookie.NameAccess, Value: "forged.token.value"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequire_ActiveUserPassesAndSetsIdentity(t *testing.T) {
	env := newAuthEnv(t)
	w := env.request(t, http.MethodGet, "/me", env.accessCookie(t, 7))

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			UID  uint64 `json:"uid"`
			Role string `json:"role"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, uint64(7), body.Data.UID)
	assert.Equal(t, "author", body.Data.Role)
}

func TestRequire_PendingVerifyRejected(t *testing.T) {
	env := newAuthEnv(t)
	env.users.user.Status = model.StatusPendingVerify

	w := env.request(t, http.MethodGet, "/me", env.accessCookie(t, 7))
	require.Equal(t, http.StatusForbidden, w.Code)

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeNotVerified, body.Code)
}

func TestRequire_DisabledRejected(t *testing.T) {
	env := newAuthEnv(t)
	env.users.user.Status = model.StatusDisabled

	w := env.request(t, http.MethodGet, "/me", env.accessCookie(t, 7))
	require.Equal(t, http.StatusForbidden, w.Code)

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeDisabled, body.Code)
}

func TestRequire_UserDeleted(t *testing.T) {
	env := newAuthEnv(t)
	env.users.err = repository.ErrNotFound

	w := env.request(t, http.MethodGet, "/me", env.accessCookie(t, 7))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireRole_Denied(t *testing.T) {
	env := newAuthEnv(t)
	// author(2) 访问 admin(3) 接口
	w := env.request(t, http.MethodGet, "/admin", env.accessCookie(t, 7))

	require.Equal(t, http.StatusForbidden, w.Code)
	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeForbidden, body.Code)
}

// TestInvalidate_TakesEffectImmediately 验证"角色/状态变更后立即生效"这条硬要求：
// 缓存命中期间改了状态不会立刻反映，必须靠 Invalidate 主动清除。
func TestInvalidate_TakesEffectImmediately(t *testing.T) {
	env := newAuthEnv(t)
	cookie := env.accessCookie(t, 7)

	// 第一次请求填充缓存
	require.Equal(t, http.StatusOK, env.request(t, http.MethodGet, "/me", cookie).Code)

	// 后台把用户禁用（只改数据源，不清缓存）
	env.users.user.Status = model.StatusDisabled

	// 缓存仍在，仍放行——这正是必须主动失效的原因
	require.Equal(t, http.StatusOK, env.request(t, http.MethodGet, "/me", cookie).Code,
		"10 秒缓存期内不会自动感知状态变化")

	// 禁用操作必须调用 Invalidate
	require.NoError(t, env.authn.Invalidate(context.Background(), 7))

	require.Equal(t, http.StatusForbidden, env.request(t, http.MethodGet, "/me", cookie).Code,
		"清除缓存后必须立即拒绝")
}

func TestCSRF_SafeMethodBypassed(t *testing.T) {
	env := newAuthEnv(t)
	assert.Equal(t, http.StatusOK, env.request(t, http.MethodGet, "/open").Code)
}

func TestCSRF_MissingHeaderRejected(t *testing.T) {
	env := newAuthEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.AddCookie(&http.Cookie{Name: authcookie.NameCSRF, Value: "csrf-value"})
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeCSRFInvalid, body.Code)
}

func TestCSRF_MismatchRejected(t *testing.T) {
	env := newAuthEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.AddCookie(&http.Cookie{Name: authcookie.NameCSRF, Value: "cookie-value"})
	req.Header.Set(authcookie.HeaderCSRF, "header-value")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCSRF_MatchPasses(t *testing.T) {
	env := newAuthEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.AddCookie(&http.Cookie{Name: authcookie.NameCSRF, Value: "same-value"})
	req.Header.Set(authcookie.HeaderCSRF, "same-value")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthenticate_InternalErrorBecomes500(t *testing.T) {
	env := newAuthEnv(t)
	env.users.err = errors.New("db down")

	w := env.request(t, http.MethodGet, "/me", env.accessCookie(t, 7))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
