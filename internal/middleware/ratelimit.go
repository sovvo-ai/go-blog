package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/response"
)

// AllowFunc 是限流判定函数，签名与 ratelimit.Limiter.Allow 一致。
// 定义成函数类型是为了单测能注入假实现，不必起 Redis。
type AllowFunc func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)

// rateRule 是一条限流规则。
// path 用 gin 的路由模板（GetRoute 的 FullPath），不用正则。
type rateRule struct {
	scope  string
	method string
	path   string
	limit  int
	window time.Duration
}

// rateRules 是敏感接口的专属规则。
//
// 取值口径：按「单 IP」估算，比产品限制略宽松，
// 目的是挡住脚本刷接口与误写的循环，不是精确计费。
var rateRules = []rateRule{
	{scope: "login", method: http.MethodPost, path: "/api/v1/auth/login", limit: 10, window: time.Minute},
	{scope: "register", method: http.MethodPost, path: "/api/v1/auth/register", limit: 5, window: time.Hour},
	{scope: "pwd-reset", method: http.MethodPost, path: "/api/v1/auth/forgot-password", limit: 5, window: 10 * time.Minute},
	{scope: "pwd-reset", method: http.MethodPost, path: "/api/v1/auth/reset-password", limit: 5, window: 10 * time.Minute},
	{scope: "verify-mail", method: http.MethodPost, path: "/api/v1/auth/resend-verify", limit: 5, window: 10 * time.Minute},
	{scope: "chat-send", method: http.MethodPost, path: "/api/v1/conversations/:id/messages", limit: 60, window: time.Minute},
	{scope: "chat-open", method: http.MethodPost, path: "/api/v1/conversations", limit: 30, window: time.Hour},
	{scope: "upload", method: http.MethodPost, path: "/api/v1/uploads/images", limit: 30, window: time.Minute},
	{scope: "comment", method: http.MethodPost, path: "/api/v1/articles/:id/comments", limit: 20, window: time.Minute},
	{scope: "article-write", method: http.MethodPost, path: "/api/v1/articles", limit: 60, window: time.Hour},
}

// defaultWriteRule 是所有其它写请求的兜底规则。
var defaultWriteRule = rateRule{scope: "write", limit: 180, window: time.Minute}

// RateLimit 对写请求做固定窗口限流（TECH_DESIGN 6.7）。
//
// 设计取舍：
//   - 只限制写请求：读接口由 CDN/Nginx 层缓存与系统容量兜底，应用层限流收益低
//   - 键按 IP：中间件在认证之前执行，此时拿不到 uid；同域反代下 IP 由 Nginx 透传
//   - Redis 故障时放行：限流是保护手段，不能因为计数器不可用把整站打死，但要留告警日志
func RateLimit(allow AllowFunc, log *slog.Logger, enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		rule, ok := matchRateRule(c)
		if !ok {
			c.Next()
			return
		}

		key := "blog:rate:" + rule.scope + ":" + c.ClientIP()
		pass, retry, err := allow(c.Request.Context(), key, rule.limit, rule.window)
		if err != nil {
			// 计数器不可用时选择放行，只记录告警
			log.Warn("限流计数失败，放行请求", "scope", rule.scope, "err", err)
			c.Next()
			return
		}
		if !pass {
			secs := int(retry.Seconds() + 0.5)
			if secs < 1 {
				secs = 1
			}
			c.Header("Retry-After", strconv.Itoa(secs))
			response.Error(c, errs.New(errs.CodeTooManyRequests, http.StatusTooManyRequests, "请求过于频繁，请稍后再试"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// matchRateRule 先匹配专属规则，再退回写请求兜底规则。
// 运维端点（/healthz、/readyz、/metrics）与 WebSocket 不参与限流。
func matchRateRule(c *gin.Context) (rateRule, bool) {
	full := c.FullPath()
	if full == "" {
		return rateRule{}, false
	}
	for _, r := range rateRules {
		if r.method == c.Request.Method && r.path == full {
			return r, true
		}
	}
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if strings.HasPrefix(full, "/api/v1/") {
			return defaultWriteRule, true
		}
	}
	return rateRule{}, false
}
