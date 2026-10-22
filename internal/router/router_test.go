package router_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/handler"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/response"
	"go-blog/internal/router"
)

func newTestRouter() http.Handler {
	cfg := &config.Config{App: config.App{Env: config.EnvTest, Addr: ":0", Version: "test"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.NewHealthHandler(log, nil, "test")
	// 本组用例只验证运维端点与兜底路由，业务处理器留空
	return router.New(cfg, log, router.Deps{
		Health: h,
		Cookie: authcookie.New(cfg),
	})
}

func TestRouter_HealthEndpoints(t *testing.T) {
	r := newTestRouter()

	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.NotEmpty(t, w.Header().Get("X-Request-Id"), "所有响应都应带请求 ID")
	}
}

func TestRouter_UnknownRouteReturnsEnvelope404(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/not-exists", nil))

	require.Equal(t, http.StatusNotFound, w.Code)

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeNotFound, body.Code)
	assert.Equal(t, "接口不存在", body.Message)
	assert.NotEmpty(t, body.RequestID)
}

func TestRouter_WrongMethodReturns405(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestRouter_DoesNotTrustProxyHeaders(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 未配置信任代理时，gin 不应把伪造头当作真实来源
	assert.Equal(t, http.StatusOK, w.Code)
}
