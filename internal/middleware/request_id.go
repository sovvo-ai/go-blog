package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go-blog/internal/pkg/logger"
	"go-blog/internal/pkg/reqctx"
)

// HeaderRequestID 是客户端可透传的请求 ID 头，便于跨层追踪。
const HeaderRequestID = "X-Request-Id"

// RequestID 为每个请求分配追踪 ID，写入 gin.Context、request context 与响应头。
//
// 客户端传了合法值就复用（网关或前端生成的链路 ID 更有价值），
// 非法值一律重新生成——避免日志注入与超长头。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := sanitizeRequestID(c.GetHeader(HeaderRequestID))
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(reqctx.GinRequestID, id)
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), id))
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// sanitizeRequestID 只接受长度 8–64 的字母数字与 - _ .，其余视为非法。
func sanitizeRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) < 8 || len(raw) > 64 {
		return ""
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return ""
		}
	}
	return raw
}
