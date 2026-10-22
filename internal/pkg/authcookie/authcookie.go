// Package authcookie 统一管理三个认证 Cookie 的写入与读取（TECH_DESIGN 6.1）。
//
// 三个 Cookie 的 Path 各不相同，这是有意设计：
//   - access_token 必须为 /，所有页面与接口都要用
//   - refresh_token 限定为 /api/v1/auth，只有刷新与退出会携带，
//     把 XSS 可窃取的窗口压到最小
//   - csrf_token 为 / 且非 HttpOnly，前端需要读取后放进请求头
package authcookie

import (
	"net/http"
	"strings"
	"time"

	"go-blog/internal/config"
)

// Cookie 名称。
const (
	NameAccess  = "access_token"
	NameRefresh = "refresh_token"
	NameCSRF    = "csrf_token"
)

// HeaderCSRF 是 CSRF 双提交校验使用的请求头。
const HeaderCSRF = "X-CSRF-Token"

// RefreshPath 是 refresh_token 的作用路径。
const RefreshPath = "/api/v1/auth"

// refreshMaxAge 与 Refresh Token 的 7 天保持一致。
const refreshMaxAge = 7 * 24 * 60 * 60

// Manager 按配置写入 Cookie。
type Manager struct {
	secure   bool
	sameSite http.SameSite
}

// New 构造 Manager。生产环境由 config 强制 Secure=true。
func New(cfg *config.Config) *Manager {
	ss := http.SameSiteLaxMode
	if cfg.Cookie.SameSite == "strict" {
		ss = http.SameSiteStrictMode
	}
	return &Manager{secure: cfg.Cookie.Secure, sameSite: ss}
}

// SetSession 写入登录/刷新后的三个 Cookie。
func (m *Manager) SetSession(w http.ResponseWriter, access string, accessTTL time.Duration, refresh, csrf string) {
	m.set(w, NameAccess, access, "/", int(accessTTL.Seconds()), true)
	m.set(w, NameRefresh, refresh, RefreshPath, refreshMaxAge, true)
	m.set(w, NameCSRF, csrf, "/", refreshMaxAge, false)
}

// Clear 清除三个 Cookie。Path 必须与写入时一致，否则浏览器不会删除。
func (m *Manager) Clear(w http.ResponseWriter) {
	m.set(w, NameAccess, "", "/", -1, true)
	m.set(w, NameRefresh, "", RefreshPath, -1, true)
	m.set(w, NameCSRF, "", "/", -1, false)
}

func (m *Manager) set(w http.ResponseWriter, name, value, path string, maxAge int, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		Secure:   m.secure,
		SameSite: m.sameSite,
		// 不设置 Domain：用户端与管理端各自独立登录，凭证不共享
	})
}

// Access 读取访问令牌。
func (m *Manager) Access(r *http.Request) string { return readCookie(r, NameAccess) }

// Refresh 读取刷新令牌。
func (m *Manager) Refresh(r *http.Request) string { return readCookie(r, NameRefresh) }

// CSRF 读取 CSRF Cookie。
func (m *Manager) CSRF(r *http.Request) string { return readCookie(r, NameCSRF) }

func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}
