package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// AuthHandler 处理认证相关接口。
type AuthHandler struct {
	svc    *service.AuthService
	cookie *authcookie.Manager
	log    *slog.Logger
}

// NewAuthHandler 构造 AuthHandler。
func NewAuthHandler(svc *service.AuthService, cookie *authcookie.Manager, log *slog.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, cookie: cookie, log: log}
}

// Register 处理 POST /auth/register。
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}

	uid, err := h.svc.Register(c.Request.Context(), service.RegisterInput{
		Username: req.Username,
		Email:    string(req.Email),
		Password: req.Password,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, dto.RegisterData{
		UserId: uidString(uid),
		Status: dto.UserStatusPendingVerify,
	})
}

// VerifyEmail 处理 GET /auth/verify-email。
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	if err := h.svc.VerifyEmail(c.Request.Context(), c.Query("token")); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// ResendVerify 处理 POST /auth/resend-verify。
// 无论邮箱是否存在都返回同一句话，避免账号枚举。
func (h *AuthHandler) ResendVerify(c *gin.Context) {
	var req dto.EmailOnlyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}
	if err := h.svc.ResendVerify(c.Request.Context(), string(req.Email)); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// Login 处理 POST /auth/login。
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}

	res, err := h.svc.Login(c.Request.Context(), service.LoginInput{
		Account:  req.Account,
		Password: req.Password,
	}, service.Fingerprint{
		IP: c.ClientIP(),
		UA: c.Request.UserAgent(),
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	h.cookie.SetSession(c.Writer, res.AccessToken, h.svc.AccessTTL(), res.RefreshToken, res.CSRFToken)
	response.OK(c, dto.LoginData{
		User:      toUserFull(res.User),
		CsrfToken: res.CSRFToken,
	})
}

// Refresh 处理 POST /auth/refresh。
func (h *AuthHandler) Refresh(c *gin.Context) {
	token := h.cookie.Refresh(c.Request)
	if token == "" {
		h.cookie.Clear(c.Writer)
		response.Error(c, errs.Unauthorized("会话已失效，请重新登录"))
		return
	}

	pair, err := h.svc.Refresh(c.Request.Context(), token, service.Fingerprint{
		IP: c.ClientIP(),
		UA: c.Request.UserAgent(),
	})
	if err != nil {
		// 刷新失败必须清 Cookie，否则前端会反复用失效令牌重试
		h.cookie.Clear(c.Writer)
		response.Error(c, err)
		return
	}

	h.cookie.SetSession(c.Writer, pair.AccessToken, h.svc.AccessTTL(), pair.RefreshToken, pair.CSRFToken)
	response.OK(c, dto.RefreshData{CsrfToken: pair.CSRFToken})
}

// Logout 处理 POST /auth/logout。幂等：未登录调用也返回 204。
func (h *AuthHandler) Logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), h.cookie.Refresh(c.Request)); err != nil {
		response.Error(c, err)
		return
	}
	h.cookie.Clear(c.Writer)
	response.NoContent(c)
}

// ForgotPassword 处理 POST /auth/forgot-password。
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req dto.EmailOnlyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}
	if err := h.svc.ForgotPassword(c.Request.Context(), string(req.Email)); err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, nil)
}

// ResetPassword 处理 POST /auth/reset-password。
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), req.Token, req.NewPassword); err != nil {
		response.Error(c, err)
		return
	}
	// 密码已变更，会话全部失效，顺手清掉当前浏览器的 Cookie
	h.cookie.Clear(c.Writer)
	response.OK(c, nil)
}
