package middleware_test

import (
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
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/response"
)

func init() { gin.SetMode(gin.TestMode) }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/", func(c *gin.Context) { response.OK(c, nil) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get(middleware.HeaderRequestID), "必须回写请求 ID")

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.RequestID)
	assert.Equal(t, w.Header().Get(middleware.HeaderRequestID), body.RequestID, "响应头与响应体必须一致")
}

func TestRequestID_ReusesValidClientID(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/", func(c *gin.Context) { response.OK(c, nil) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, "trace-abc-123456")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "trace-abc-123456", w.Header().Get(middleware.HeaderRequestID))
}

func TestRequestID_RejectsIllegalValue(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/", func(c *gin.Context) { response.OK(c, nil) })

	// 含换行符，属于日志注入尝试
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, "abc\nInjected: yes")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := w.Header().Get(middleware.HeaderRequestID)
	assert.NotContains(t, got, "\n", "非法请求 ID 必须被丢弃")
	assert.NotEqual(t, "abc\nInjected: yes", got)
}

func TestRecovery_ReturnsUnifiedEnvelope(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(discardLogger()))
	r.GET("/boom", func(*gin.Context) { panic(errors.New("内部炸了")) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	require.Equal(t, http.StatusInternalServerError, w.Code)

	var body response.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, errs.CodeInternal, body.Code)
	assert.Equal(t, "服务器内部错误", body.Message, "不能把 panic 内容返回给客户端")
	assert.NotContains(t, w.Body.String(), "内部炸了")
}

func TestAccessLog_DoesNotBreakRequest(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.AccessLog(discardLogger(), "/healthz"))
	r.GET("/ok", func(c *gin.Context) { response.OK(c, gin.H{"a": 1}) })
	r.GET("/healthz", func(c *gin.Context) { response.OK(c, nil) })

	for _, path := range []string{"/ok", "/healthz"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, w.Code, path)
	}
}
