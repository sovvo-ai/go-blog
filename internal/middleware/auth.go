package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/response"
	"go-blog/internal/repository"
)

// gin.Context 中保存身份信息的键。
const (
	ctxUID  = "auth_uid"
	ctxRole = "auth_role"
)

// userAuthTTL 是"状态 + 角色"的缓存时长。
// 角色或状态变更时会主动删除该键；TTL 只是兜底，不是正确性来源。
const userAuthTTL = 10 * time.Second

// UserReader 是鉴权所需的最小用户读取能力。
// 定义为接口而不是具体类型，便于在单测里替换数据源。
type UserReader interface {
	ByID(ctx context.Context, id uint64) (*model.User, error)
}

// Authenticator 解析访问令牌并加载用户身份。
type Authenticator struct {
	jwt    *jwtutil.Manager
	users  UserReader
	rdb    *redis.Client
	cookie *authcookie.Manager
}

// NewAuthenticator 构造 Authenticator。
func NewAuthenticator(j *jwtutil.Manager, users UserReader, rdb *redis.Client, cookie *authcookie.Manager) *Authenticator {
	return &Authenticator{jwt: j, users: users, rdb: rdb, cookie: cookie}
}

func authCacheKey(uid uint64) string {
	return "blog:user:auth:" + strconv.FormatUint(uid, 10)
}

// Require 要求登录。失败直接中断请求。
func (a *Authenticator) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, role, status, err := a.identify(c)
		if err != nil {
			response.Error(c, err)
			return
		}
		switch status {
		case model.StatusActive:
		case model.StatusPendingVerify:
			response.Error(c, errs.New(errs.CodeNotVerified, http.StatusForbidden, "账号尚未完成邮箱验证"))
			return
		case model.StatusDisabled:
			response.Error(c, errs.New(errs.CodeDisabled, http.StatusForbidden, "账号已被禁用"))
			return
		default:
			response.Error(c, errs.Unauthorized("账号状态异常"))
			return
		}

		c.Set(ctxUID, uid)
		c.Set(ctxRole, role)
		c.Next()
	}
}

// Optional 尝试识别用户，但不阻断请求。用于公开页面个性化。
func (a *Authenticator) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, role, status, err := a.identify(c)
		if err == nil && status == model.StatusActive {
			c.Set(ctxUID, uid)
			c.Set(ctxRole, role)
		}
		c.Next()
	}
}

// RequireRole 要求角色达到最低层级。必须放在 Require 之后。
func (a *Authenticator) RequireRole(min authz.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := RoleOf(c)
		if !role.AtLeast(min) {
			response.Error(c, errs.Forbidden("没有权限执行此操作"))
			return
		}
		c.Next()
	}
}

// identify 解析令牌并读取状态与角色。
func (a *Authenticator) identify(c *gin.Context) (uint64, authz.Role, uint8, error) {
	token := a.cookie.Access(c.Request)
	if token == "" {
		return 0, 0, 0, errs.Unauthorized("请先登录")
	}

	claims, err := a.jwt.Parse(token)
	if err != nil {
		return 0, 0, 0, errs.Unauthorized("登录状态已失效，请重新登录")
	}
	uid, err := claims.UIDValue()
	if err != nil {
		return 0, 0, 0, errs.Unauthorized("登录状态已失效，请重新登录")
	}

	status, roleValue, err := a.loadAuth(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return 0, 0, 0, errs.Unauthorized("账号不存在")
		}
		return 0, 0, 0, errs.Internal(err)
	}
	return uid, authz.Role(roleValue), status, nil
}

// loadAuth 先读缓存，未命中再回源数据库。
func (a *Authenticator) loadAuth(ctx context.Context, uid uint64) (uint8, uint8, error) {
	key := authCacheKey(uid)
	vals, err := a.rdb.HGetAll(ctx, key).Result()
	if err == nil && len(vals) == 2 {
		status, convErr := strconv.ParseUint(vals["status"], 10, 8)
		role, convErr2 := strconv.ParseUint(vals["role"], 10, 8)
		if convErr == nil && convErr2 == nil {
			return uint8(status), uint8(role), nil
		}
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		// 缓存故障不阻断鉴权，直接回源；可用性优先于缓存命中率
	}

	u, err := a.users.ByID(ctx, uid)
	if err != nil {
		return 0, 0, err
	}

	// 回填缓存失败不影响本次请求
	_ = a.rdb.HSet(ctx, key, "status", u.Status, "role", u.Role).Err()
	_ = a.rdb.Expire(ctx, key, userAuthTTL).Err()

	return u.Status, u.Role, nil
}

// Invalidate 清除某用户的状态缓存。禁用、改角色后必须立即调用。
func (a *Authenticator) Invalidate(ctx context.Context, uid uint64) error {
	return a.rdb.Del(ctx, authCacheKey(uid)).Err()
}

// UIDOf 返回当前登录用户 ID，未登录返回 0。
func UIDOf(c *gin.Context) uint64 {
	v, ok := c.Get(ctxUID)
	if !ok {
		return 0
	}
	uid, _ := v.(uint64)
	return uid
}

// RoleOf 返回当前用户角色，未登录返回 0。
func RoleOf(c *gin.Context) authz.Role {
	v, ok := c.Get(ctxRole)
	if !ok {
		return 0
	}
	role, _ := v.(authz.Role)
	return role
}
