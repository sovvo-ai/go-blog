package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/errs"
	"go-blog/internal/handler"
	"go-blog/internal/pkg/response"
)

func init() { gin.SetMode(gin.TestMode) }

func newEngine(h *handler.HealthHandler) *gin.Engine {
	r := gin.New()
	r.GET("/healthz", h.Live)
	r.GET("/readyz", h.Ready)
	return r
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func okPing(context.Context) error { return nil }

func failPing(context.Context) error { return errors.New("connect: connection refused") }

func TestLive_AlwaysOK(t *testing.T) {
	h := handler.NewHealthHandler(testLogger(), []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: failPing},
	}, "test")

	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	// 存活探针不查依赖：依赖挂了也不应该让容器被重启
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestReady_AllDependenciesUp(t *testing.T) {
	h := handler.NewHealthHandler(testLogger(), []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: okPing},
		{Name: "redis", Required: true, Ping: okPing},
	}, "test")

	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
			Checks []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeOK, body.Code)
	assert.Equal(t, "ok", body.Data.Status)
	require.Len(t, body.Data.Checks, 2)
	for _, chk := range body.Data.Checks {
		assert.Equal(t, "ok", chk.Status, chk.Name)
	}
}

func TestReady_RequiredDependencyDown(t *testing.T) {
	h := handler.NewHealthHandler(testLogger(), []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: failPing},
		{Name: "redis", Required: true, Ping: okPing},
	}, "test")

	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	require.Equal(t, http.StatusServiceUnavailable, w.Code, "必需依赖不可用必须返回 503")

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeNotReady, body.Code)
	assert.Contains(t, body.Message, "mysql", "响应必须指明是哪个依赖不可用")

	// 失败明细要留在 data 里，便于部署时定位
	data, ok := body.Data.(map[string]any)
	require.True(t, ok)
	checks, ok := data["checks"].([]any)
	require.True(t, ok)
	require.Len(t, checks, 2)
}

func TestReady_UnconfiguredDependencySkipped(t *testing.T) {
	h := handler.NewHealthHandler(testLogger(), []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: okPing},
		{Name: "meilisearch", Required: false, Ping: nil},
	}, "test")

	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	require.Equal(t, http.StatusOK, w.Code, "未启用的依赖不应导致整体未就绪")

	var body struct {
		Data struct {
			Checks []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "skipped", body.Data.Checks[1].Status)
}

func TestReady_OptionalDependencyDownDoesNotFail(t *testing.T) {
	h := handler.NewHealthHandler(testLogger(), []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: okPing},
		{Name: "meilisearch", Required: false, Ping: failPing},
	}, "test")

	w := httptest.NewRecorder()
	newEngine(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}
