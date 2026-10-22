package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/middleware"
)

// 假限流器：记录被调用的键，返回预设结果
type fakeLimiter struct {
	pass   bool
	retry  time.Duration
	err    error
	keys   []string
	limits []int
}

func (f *fakeLimiter) allow(_ context.Context, key string, limit int, _ time.Duration) (bool, time.Duration, error) {
	f.keys = append(f.keys, key)
	f.limits = append(f.limits, limit)
	return f.pass, f.retry, f.err
}

func newRouter(limiter *fakeLimiter, enabled bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimit(limiter.allow, slog.Default(), enabled))
	h := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	r.POST("/api/v1/auth/login", h)
	r.POST("/api/v1/articles/:id/comments", h)
	r.POST("/api/v1/conversations/:id/messages", h)
	r.POST("/api/v1/whatever", h)
	r.GET("/api/v1/articles", h)
	r.GET("/healthz", h)
	return r
}

func do(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.9:1234" // 非内网地址，确保走 IP 维度
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimit_AllowsWhenLimiterPasses(t *testing.T) {
	f := &fakeLimiter{pass: true}
	w := do(newRouter(f, true), http.MethodPost, "/api/v1/auth/login")
	if w.Code != http.StatusOK {
		t.Fatalf("应放行，实际 %d", w.Code)
	}
	if len(f.keys) != 1 || f.keys[0] != "blog:rate:login:203.0.113.9" {
		t.Fatalf("限流键不符：%v", f.keys)
	}
	if f.limits[0] != 10 {
		t.Fatalf("登录接口限流阈值应为 10，实际 %d", f.limits[0])
	}
}

func TestRateLimit_RejectsWith429(t *testing.T) {
	f := &fakeLimiter{pass: false, retry: 30 * time.Second}
	w := do(newRouter(f, true), http.MethodPost, "/api/v1/auth/login")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("应返回 429，实际 %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "30" {
		t.Fatalf("Retry-After 应为 30，实际 %q", w.Header().Get("Retry-After"))
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	if body.Code != 10005 {
		t.Fatalf("业务码应为 10005，实际 %d", body.Code)
	}
}

// 读接口不参与限流，不应触发计数器
func TestRateLimit_SkipsReads(t *testing.T) {
	f := &fakeLimiter{pass: false}
	w := do(newRouter(f, true), http.MethodGet, "/api/v1/articles")
	if w.Code != http.StatusOK {
		t.Fatalf("读请求应放行，实际 %d", w.Code)
	}
	if len(f.keys) != 0 {
		t.Fatalf("读请求不应计数：%v", f.keys)
	}
}

// 未命中专属规则时走兜底写规则
func TestRateLimit_DefaultWriteRule(t *testing.T) {
	f := &fakeLimiter{pass: true}
	do(newRouter(f, true), http.MethodPost, "/api/v1/whatever")
	if len(f.keys) != 1 || f.limits[0] != 180 {
		t.Fatalf("兜底规则不符合预期：keys=%v limits=%v", f.keys, f.limits)
	}
}

// 专属路径参数模板必须命中（/articles/:id/comments）
func TestRateLimit_PathParamRule(t *testing.T) {
	f := &fakeLimiter{pass: true}
	do(newRouter(f, true), http.MethodPost, "/api/v1/articles/42/comments")
	if len(f.keys) != 1 || f.limits[0] != 20 {
		t.Fatalf("评论接口应命中 20/min 规则：keys=%v limits=%v", f.keys, f.limits)
	}
}

// 关闭开关后完全不计数
func TestRateLimit_Disabled(t *testing.T) {
	f := &fakeLimiter{pass: false}
	w := do(newRouter(f, false), http.MethodPost, "/api/v1/auth/login")
	if w.Code != http.StatusOK || len(f.keys) != 0 {
		t.Fatalf("关闭后应直接放行：code=%d keys=%v", w.Code, f.keys)
	}
}

// Redis 故障时放行（可用性优先），但要有日志
func TestRateLimit_FailOpenOnLimiterError(t *testing.T) {
	f := &fakeLimiter{err: errors.New("redis down")}
	w := do(newRouter(f, true), http.MethodPost, "/api/v1/auth/login")
	if w.Code != http.StatusOK {
		t.Fatalf("限流器故障时应放行，实际 %d", w.Code)
	}
}
