// Package router 负责路由注册与全局中间件装配。
package router

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"go-blog/internal/authz"
	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/handler"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/metrics"
	"go-blog/internal/pkg/ratelimit"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// csrfExempt 是"调用时尚无 CSRF Cookie"的接口白名单。
// 登录、注册、刷新、找回密码在调用时都还没有有效会话，无法双提交。
// 退出登录**不在**白名单：此时 csrf_token 仍有效，必须校验（API_SPEC 4.1）。
var csrfExempt = map[string]struct{}{
	"/api/v1/auth/register":        {},
	"/api/v1/auth/login":           {},
	"/api/v1/auth/refresh":         {},
	"/api/v1/auth/resend-verify":   {},
	"/api/v1/auth/forgot-password": {},
	"/api/v1/auth/reset-password":  {},
}

// Deps 是路由层需要的处理器与中间件。
type Deps struct {
	Health       *handler.HealthHandler
	Auth         *handler.AuthHandler
	User         *handler.UserHandler
	Article      *handler.ArticleHandler
	Media        *handler.MediaHandler
	Search       *handler.SearchHandler
	Interaction  *handler.InteractionHandler
	Comment      *handler.CommentHandler
	Notification *handler.NotificationHandler
	Admin        *handler.AdminHandler
	Chat         *handler.ChatHandler
	Ws           *handler.WsHandler
	Authn        *middleware.Authenticator
	Cookie       *authcookie.Manager
	Limiter      *ratelimit.Limiter
}

// internalIP 判断来源是否为回环或内网地址。
func internalIP(ip string) bool {
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate()
}

// New 构造 gin 引擎。
func New(cfg *config.Config, log *slog.Logger, d Deps) *gin.Engine {
	switch cfg.App.Env {
	case config.EnvProduction:
		gin.SetMode(gin.ReleaseMode)
	case config.EnvTest:
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	// 不使用 gin.Default()：它的 Logger 与 Recovery 是另一套实现，
	// 会与统一日志和统一响应结构冲突。
	engine := gin.New()
	// 方法不匹配时返回 405 而不是 404，便于前端快速发现调用错误
	engine.HandleMethodNotAllowed = true

	// V1.0 由同域 Nginx 反代，链路上不存在第三方代理。
	// 不信任任何 X-Forwarded-*，避免客户端伪造来源 IP。
	_ = engine.SetTrustedProxies(nil)

	// 指标注册表由路由层持有：中间件计数、/metrics 导出，必须是同一个实例
	reg := metrics.New()
	// 限流在生产与开发环境生效，测试环境关闭以免干扰用例
	rateLimitOn := cfg.App.Env != config.EnvTest

	engine.Use(
		middleware.RequestID(),
		middleware.Metrics(reg),
		middleware.RateLimit(d.Limiter.Allow, log, rateLimitOn),
		middleware.Recovery(log),
		middleware.AccessLog(log, "/healthz", "/readyz"),
	)

	// 运维端点：不进 /api/v1，避免被网关或限流中间件波及（TECH_DESIGN 14.3）
	engine.GET("/healthz", d.Health.Live)
	engine.GET("/readyz", d.Health.Ready)
	// 指标端点：默认只允许本机/内网访问（公网暴露指标等于给攻击者送侦查报告）。
	// 生产环境还应由 Nginx 再挡一层（deploy/nginx.conf 的 /metrics 段）。
	engine.GET("/metrics", func(c *gin.Context) {
		if !internalIP(c.ClientIP()) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.Status(http.StatusOK)
		reg.WriteText(c.Writer)
	})
	// WebSocket 在站点根路径（TECH_DESIGN 11.1/15.4：Nginx 只代理 /ws），
	// 不进 /api/v1；握手凭一次性 ticket 鉴权
	engine.GET("/ws", d.Ws.Connect)

	v1 := engine.Group("/api/v1")
	// CSRF 只挡写请求；读请求与白名单直接放行
	v1.Use(middleware.CSRF(d.Cookie, csrfExempt))

	auth := v1.Group("/auth")
	{
		auth.POST("/register", d.Auth.Register)
		auth.GET("/verify-email", d.Auth.VerifyEmail)
		auth.POST("/resend-verify", d.Auth.ResendVerify)
		auth.POST("/login", d.Auth.Login)
		auth.POST("/refresh", d.Auth.Refresh)
		auth.POST("/logout", d.Auth.Logout)
		auth.POST("/forgot-password", d.Auth.ForgotPassword)
		auth.POST("/reset-password", d.Auth.ResetPassword)
	}

	// 公开接口：文章、分类、标签、用户主页
	v1.GET("/users/:id", d.User.PublicProfile)
	// 挂可选登录：公开对象匿名可读，聊天图片换签名地址需要身份
	v1.GET("/media/*key", d.Authn.Optional(), d.Media.Serve)
	v1.GET("/search", d.Search.Search)
	v1.GET("/categories", d.Article.Categories)
	v1.GET("/tags", d.Article.Tags)
	v1.GET("/articles", d.Article.List)
	// 详情挂"可选登录"：匿名访问照常，作者本人才能看到自己的非公开文章。
	// 漏掉这一层，作者访问自己的草稿会得到 404。
	v1.GET("/articles/:id", d.Authn.Optional(), d.Article.Detail)

	// 作者接口
	author := v1.Group("", d.Authn.Require(), d.Authn.RequireRole(authz.RoleAuthor))
	{
		author.POST("/articles", d.Article.Create)
		author.PUT("/articles/:id", d.Article.Update)
		author.DELETE("/articles/:id", d.Article.Delete)
		author.GET("/articles/:id/edit", d.Article.GetForEdit)
		author.POST("/articles/:id/submit", d.Article.Transition(service.ActionSubmit))
		author.POST("/articles/:id/withdraw", d.Article.Transition(service.ActionWithdraw))
		author.POST("/articles/:id/archive", d.Article.Transition(service.ActionArchive))
		author.POST("/articles/:id/restore", d.Article.Transition(service.ActionRestore))
	}

	// 需要登录的接口
	me := v1.Group("/users", d.Authn.Require())
	{
		me.GET("/me", d.User.Me)
		me.PATCH("/me", d.User.UpdateProfile)
		me.PUT("/me/password", d.User.ChangePassword)
		me.PUT("/me/avatar", d.User.UpdateAvatar)
		me.GET("/me/articles", d.Article.ListMine)
		me.GET("/me/favorites", d.Interaction.MyFavorites)
	}

	// 互动接口：全部需要登录，写操作走 CSRF 中间件
	v1.POST("/articles/:id/like", d.Authn.Require(), d.Interaction.Like)
	v1.DELETE("/articles/:id/like", d.Authn.Require(), d.Interaction.Unlike)
	v1.POST("/articles/:id/favorite", d.Authn.Require(), d.Interaction.Favorite)
	v1.DELETE("/articles/:id/favorite", d.Authn.Require(), d.Interaction.Unfavorite)

	// 评论：列表公开（可选登录，本人可见 pending），发表/删除需登录
	v1.GET("/articles/:id/comments", d.Authn.Optional(), d.Comment.List)
	v1.POST("/articles/:id/comments", d.Authn.Require(), d.Comment.Create)
	v1.DELETE("/comments/:id", d.Authn.Require(), d.Comment.Delete)

	// 举报
	v1.POST("/reports", d.Authn.Require(), d.Notification.Report)

	// 通知
	v1.GET("/notifications", d.Authn.Require(), d.Notification.List)
	v1.GET("/notifications/unread-count", d.Authn.Require(), d.Notification.UnreadCount)
	v1.PUT("/notifications/:id/read", d.Authn.Require(), d.Notification.MarkRead)
	v1.PUT("/notifications/read-all", d.Authn.Require(), d.Notification.MarkAllRead)

	// 聊天与通用上传（写操作 CSRF 由全局中间件统一拦截）
	v1.POST("/uploads/images", d.Authn.Require(), d.Media.Upload)
	v1.GET("/conversations", d.Authn.Require(), d.Chat.ListConversations)
	v1.POST("/conversations", d.Authn.Require(), d.Chat.CreateConversation)
	v1.GET("/conversations/:id/messages", d.Authn.Require(), d.Chat.ListMessages)
	v1.POST("/conversations/:id/messages", d.Authn.Require(), d.Chat.SendMessage)
	v1.PUT("/conversations/:id/read", d.Authn.Require(), d.Chat.MarkRead)
	v1.PUT("/conversations/:id/mute", d.Authn.Require(), d.Chat.SetMute)
	v1.POST("/ws/ticket", d.Authn.Require(), d.Ws.Ticket)

	// 管理端
	admin := v1.Group("/admin", d.Authn.Require(), d.Authn.RequireRole(authz.RoleAdmin))
	{
		admin.GET("/overview", d.Admin.Overview)
		admin.GET("/users", d.Admin.ListUsers)
		admin.PUT("/users/:id/status", d.Admin.UpdateUserStatus)
		admin.PUT("/users/:id/role", d.Admin.UpdateUserRole)
		admin.GET("/articles", d.Admin.ListArticles)
		admin.GET("/articles/:id", d.Admin.ArticleDetail)
		admin.POST("/articles/:id/approve", d.Admin.Approve)
		admin.POST("/articles/:id/reject", d.Admin.Reject)
		admin.POST("/articles/:id/offline", d.Admin.Offline)
		admin.POST("/articles/:id/relist", d.Admin.Relist)
		admin.DELETE("/articles/:id", d.Admin.DeleteArticle)
		admin.GET("/comments", d.Admin.ListComments)
		admin.POST("/comments/:id/approve", d.Admin.ApproveComment)
		admin.POST("/comments/:id/reject", d.Admin.RejectComment)
		admin.DELETE("/comments/:id", d.Admin.DeleteComment)
		admin.GET("/reports", d.Admin.ListReports)
		admin.PUT("/reports/:id", d.Admin.HandleReport)
		admin.POST("/categories", d.Admin.CreateCategory)
		admin.PUT("/categories/:id", d.Admin.UpdateCategory)
		admin.DELETE("/categories/:id", d.Admin.DeleteCategory)
		admin.POST("/tags", d.Admin.CreateTag)
		admin.PUT("/tags/:id", d.Admin.UpdateTag)
		admin.DELETE("/tags/:id", d.Admin.DeleteTag)
		admin.POST("/tags/merge", d.Admin.MergeTags)
		admin.GET("/settings", d.Admin.GetSettings)
		admin.PUT("/settings", d.Admin.UpdateSettings)
		admin.GET("/media", d.Admin.ListMedia)
		admin.GET("/logs", d.Admin.ListLogs)
		admin.GET("/index-failures", d.Admin.IndexFailures)
		admin.POST("/index-rebuild", d.Admin.Reindex)
		admin.POST("/counters/recalc", d.Admin.RecalcCounters)
	}

	engine.NoRoute(func(c *gin.Context) {
		response.Error(c, errs.NotFound("接口不存在"))
	})
	engine.NoMethod(func(c *gin.Context) {
		response.Error(c, errs.New(errs.CodeInvalidParam, http.StatusMethodNotAllowed, "请求方法不被允许"))
	})

	return engine
}
