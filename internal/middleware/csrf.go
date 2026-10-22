package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/response"
)

// CSRF 实现双提交 Cookie 校验（TECH_DESIGN 6.7）。
//
// 规则：
//   - GET / HEAD / OPTIONS 等安全方法不校验
//   - 写请求必须同时带 csrf_token Cookie 与 X-CSRF-Token 头，且两者相等
//   - 登录、注册、刷新等尚无 Cookie 的接口走白名单
//
// 为什么不只靠 SameSite=Lax：同站跨子域请求仍会带上 Lax Cookie，
// 管理端与用户端同域部署时这一层是必须的。
func CSRF(cookies *authcookie.Manager, skip map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		if _, ok := skip[c.FullPath()]; ok {
			c.Next()
			return
		}
		// 未匹配到路由时 FullPath 为空，交给 404 处理，不在这里拦截
		if c.FullPath() == "" {
			c.Next()
			return
		}

		fromCookie := cookies.CSRF(c.Request)
		fromHeader := c.GetHeader(authcookie.HeaderCSRF)
		if fromCookie == "" || fromHeader == "" {
			response.Error(c, errs.New(errs.CodeCSRFInvalid, http.StatusForbidden, "CSRF 校验失败，请刷新页面后重试"))
			return
		}
		if subtle.ConstantTimeCompare([]byte(fromCookie), []byte(fromHeader)) != 1 {
			response.Error(c, errs.New(errs.CodeCSRFInvalid, http.StatusForbidden, "CSRF 校验失败，请刷新页面后重试"))
			return
		}

		c.Next()
	}
}
