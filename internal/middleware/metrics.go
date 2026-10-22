package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/pkg/metrics"
)

// Metrics 记录请求量、状态码分布与耗时直方图。
//
// 标签使用 c.FullPath()（路由模板）而不是实际 URL：
// /article/a、/article/b 必须归到同一个 /article/:slug，
// 否则指标基数会随文章数线性增长，Prometheus 先被自己打爆。
func Metrics(reg *metrics.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		// /metrics 自己不计入，避免抓取流量污染业务指标
		if c.FullPath() == "/metrics" {
			c.Next()
			return
		}

		reg.IncInFlight()
		start := time.Now()
		c.Next()
		reg.DecInFlight()
		reg.Observe(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start))
	}
}
