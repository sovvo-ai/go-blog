package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog 记录每个请求的方法、路径、状态码、耗时与请求 ID。
//
// 探针路径默认跳过，否则健康检查会以秒级频率刷满日志。
func AccessLog(log *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if _, ok := skip[c.Request.URL.Path]; ok {
			return
		}

		status := c.Writer.Status()
		attrs := []any{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.String("ip", c.ClientIP()),
			slog.Int("bytes", c.Writer.Size()),
		}
		// BusinessError 的业务码对排查更有价值，单独带上。
		if code, exists := c.Get("biz_code"); exists {
			attrs = append(attrs, slog.Any("biz_code", code))
		}

		switch {
		case status >= 500:
			log.Error("http_request", attrs...)
		case status >= 400:
			log.Warn("http_request", attrs...)
		default:
			log.Info("http_request", attrs...)
		}
	}
}
