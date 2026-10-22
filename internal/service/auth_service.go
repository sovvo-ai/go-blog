package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-blog/internal/authz"
	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/mailer"
	"go-blog/internal/model"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/password"
	"go-blog/internal/pkg/ratelimit"
	"go-blog/internal/repository"
)

// LoginInput 是登录入参。
type LoginInput struct {
	Account  string
	Password string
}

// RegisterInput 是注册入参。
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

// TokenPair 是一次签发产生的一组凭证。
type TokenPair struct {
	AccessToken  string
	AccessExpiry time.Time
	RefreshToken string
	CSRFToken    string
}

// LoginResult 是登录成功的完整结果。
type LoginResult struct {
	User *model.User
	TokenPair
}

// AuthService 实现注册、登录、刷新、退出与密码找回。
type AuthService struct {
	users    UserStore
	sessions *SessionStore
	onetime  *OneTimeTokenStore
	limits   *ratelimit.Limiter
	mailer   mailer.Mailer
	jwt      *jwtutil.Manager
	cfg      *config.Config
	log      *slog.Logger
	now      func() time.Time

	// dummyHash 用于"账号不存在"时仍执行一次同样耗时的校验，
	// 避免通过响应时间枚举已注册账号。
	dummyHash string
}

// NewAuthService 构造 AuthService。
func NewAuthService(
	users UserStore,
	sessions *SessionStore,
	onetime *OneTimeTokenStore,
	limits *ratelimit.Limiter,
	m mailer.Mailer,
	jwt *jwtutil.Manager,
	cfg *config.Config,
	log *slog.Logger,
) (*AuthService, error) {
	dummy, err := password.Hash("dummy-password-for-timing-equalization")
	if err != nil {
		return nil, fmt.Errorf("初始化密码哈希失败: %w", err)
	}
	return &AuthService{
		users:     users,
		sessions:  sessions,
		onetime:   onetime,
		limits:    limits,
		mailer:    m,
		jwt:       jwt,
		cfg:       cfg,
		log:       log,
		now:       time.Now,
		dummyHash: dummy,
	}, nil
}

// AccessTTL 返回访问令牌有效期，供 handler 设置 Cookie Max-Age。
func (s *AuthService) AccessTTL() time.Duration { return s.jwt.TTL() }

// Register 创建账号，状态为 pending_verify，不签发任何令牌（PRD 3.5.1）。
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (uint64, error) {
	username := strings.TrimSpace(in.Username)
	emailAddr := normalizeEmail(in.Email)

	if err := validateUsername(username); err != nil {
		return 0, err
	}
	if err := validateEmail(emailAddr); err != nil {
		return 0, err
	}
	if err := validatePassword(in.Password); err != nil {
		return 0, err
	}

	// 先查再插只能降低冲突概率，真正兜底的是唯一索引
	taken, err := s.users.ExistsUsername(ctx, username)
	if err != nil {
		return 0, errs.Internal(err)
	}
	if taken {
		return 0, errs.New(errs.CodeUsernameTaken, http.StatusConflict, "用户名已被占用")
	}
	taken, err = s.users.ExistsEmail(ctx, emailAddr)
	if err != nil {
		return 0, errs.Internal(err)
	}
	if taken {
		return 0, errs.New(errs.CodeEmailTaken, http.StatusConflict, "邮箱已注册")
	}

	hash, err := password.Hash(in.Password)
	if err != nil {
		return 0, errs.Internal(err)
	}

	u := &model.User{
		Username:     username,
		Email:        emailAddr,
		PasswordHash: hash,
		Nickname:     username,
		Role:         uint8(authz.RoleReader),
		Status:       model.StatusPendingVerify,
	}
	if err := s.users.Create(ctx, u); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			// 并发注册：按冲突的唯一键给出精确提示
			switch repository.DuplicateKey(err) {
			case "username":
				return 0, errs.New(errs.CodeUsernameTaken, http.StatusConflict, "用户名已被占用")
			case "email":
				return 0, errs.New(errs.CodeEmailTaken, http.StatusConflict, "邮箱已注册")
			default:
				return 0, errs.New(errs.CodeUsernameTaken, http.StatusConflict, "用户名或邮箱已被占用")
			}
		}
		return 0, errs.Internal(err)
	}

	if err := s.sendVerifyMail(ctx, u); err != nil {
		// 注册已成功，邮件失败不回滚：用户可以走"重发验证邮件"。
		// 直接失败会让用户既没账号也收不到信，体验更差。
		s.log.Error("verify_mail_failed",
			slog.String("uid", strconv.FormatUint(u.ID, 10)),
			slog.Any("error", err),
		)
	}
	return u.ID, nil
}

// VerifyEmail 消费验证令牌并激活账号。
func (s *AuthService) VerifyEmail(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return errs.New(errs.CodeVerifyExpired, http.StatusBadRequest, "验证链接无效或已过期")
	}
	uid, err := s.onetime.ConsumeVerify(ctx, token)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			return errs.New(errs.CodeVerifyExpired, http.StatusBadRequest, "验证链接无效或已过期")
		}
		return errs.Internal(err)
	}
	if err := s.users.MarkEmailVerified(ctx, uid, s.now()); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errs.New(errs.CodeVerifyExpired, http.StatusBadRequest, "验证链接无效或已过期")
		}
		return errs.Internal(err)
	}
	return nil
}

// ResendVerify 重发验证邮件，带 60 秒间隔与每日上限。
func (s *AuthService) ResendVerify(ctx context.Context, email string) error {
	emailAddr := normalizeEmail(email)
	if err := validateEmail(emailAddr); err != nil {
		return err
	}

	// 无论邮箱是否存在都返回相同结果，避免账号枚举
	u, err := s.users.ByEmail(ctx, emailAddr)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return errs.Internal(err)
	}
	if u == nil || u.Status != model.StatusPendingVerify {
		return nil
	}

	if err := s.reserveMail(ctx, "verify", u.ID); err != nil {
		return err
	}
	if err := s.sendVerifyMail(ctx, u); err != nil {
		return errs.Unavailable(errs.CodeStorageUnavailable, "邮件发送失败，请稍后重试", err)
	}
	return nil
}

// Login 校验凭证并签发令牌。
func (s *AuthService) Login(ctx context.Context, in LoginInput, fp Fingerprint) (*LoginResult, error) {
	account := normalizeAccount(in.Account)
	if account == "" || in.Password == "" {
		return nil, badRequest("请输入账号与密码", "account")
	}

	failKey := "blog:auth:fail:" + hashToken(account)
	if err := s.checkLock(ctx, failKey); err != nil {
		return nil, err
	}

	u, err := s.users.ByAccount(ctx, account)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, errs.Internal(err)
	}
	if u == nil {
		// 账号不存在也跑一次校验，保持与"密码错误"相同的响应时间
		_ = password.Verify(in.Password, s.dummyHash)
		s.recordFailure(ctx, failKey)
		return nil, errs.New(errs.CodeBadCredentials, http.StatusUnauthorized, "账号或密码错误")
	}

	if err := password.Verify(in.Password, u.PasswordHash); err != nil {
		s.recordFailure(ctx, failKey)
		return nil, errs.New(errs.CodeBadCredentials, http.StatusUnauthorized, "账号或密码错误")
	}

	switch u.Status {
	case model.StatusPendingVerify:
		return nil, errs.New(errs.CodeNotVerified, http.StatusForbidden, "账号尚未完成邮箱验证")
	case model.StatusDisabled:
		return nil, errs.New(errs.CodeDisabled, http.StatusForbidden, "账号已被禁用")
	}

	_ = s.limits.Reset(ctx, failKey)

	refresh, err := s.sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, errs.Internal(err)
	}
	access, exp, err := s.jwt.Issue(u.ID, s.now())
	if err != nil {
		return nil, errs.Internal(err)
	}
	csrf, err := newToken()
	if err != nil {
		return nil, errs.Internal(err)
	}

	if err := s.users.TouchLogin(ctx, u.ID, s.now(), fp.IP); err != nil {
		// 登录时间写失败不应阻断登录
		s.log.Warn("touch_login_failed", slog.Any("error", err))
	}

	return &LoginResult{
		User: u,
		TokenPair: TokenPair{
			AccessToken:  access,
			AccessExpiry: exp,
			RefreshToken: refresh,
			CSRFToken:    csrf,
		},
	}, nil
}

// Refresh 轮换刷新令牌并签发新的访问令牌。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string, fp Fingerprint) (*TokenPair, error) {
	newRefresh, uid, err := s.sessions.Rotate(ctx, refreshToken, fp)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionConcurrent):
			// 同一设备的并发刷新：不吊销，让客户端重试一次
			return nil, errs.Unauthorized("会话已更新，请重试")
		case errors.Is(err, ErrSessionReplayed):
			s.log.Warn("refresh_token_replayed", slog.String("ip", fp.IP))
			return nil, errs.Unauthorized("会话已失效，请重新登录")
		case errors.Is(err, ErrSessionInvalid):
			return nil, errs.Unauthorized("会话已失效，请重新登录")
		default:
			return nil, errs.Internal(err)
		}
	}

	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.Unauthorized("会话已失效，请重新登录")
		}
		return nil, errs.Internal(err)
	}
	// 禁用后即使令牌还没过期也不能继续续期
	if u.Status != model.StatusActive {
		_ = s.sessions.RevokeAll(ctx, uid)
		return nil, errs.New(errs.CodeDisabled, http.StatusForbidden, "账号已被禁用")
	}

	access, exp, err := s.jwt.Issue(uid, s.now())
	if err != nil {
		return nil, errs.Internal(err)
	}
	csrf, err := newToken()
	if err != nil {
		return nil, errs.Internal(err)
	}

	return &TokenPair{
		AccessToken:  access,
		AccessExpiry: exp,
		RefreshToken: newRefresh,
		CSRFToken:    csrf,
	}, nil
}

// Logout 吊销当前设备的会话，幂等。
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	if err := s.sessions.RevokeCurrent(ctx, refreshToken); err != nil {
		return errs.Internal(err)
	}
	return nil
}

// ForgotPassword 发送重置密码邮件。
// 无论邮箱是否存在、账号状态如何，对外表现完全一致。
func (s *AuthService) ForgotPassword(ctx context.Context, email string) error {
	emailAddr := normalizeEmail(email)
	if err := validateEmail(emailAddr); err != nil {
		// 为防枚举，格式错误也只提示"请查收邮件"
		return nil
	}

	// 按邮箱维度限流，避免被用来轰炸他人
	key := "blog:auth:forgot:" + hashToken(emailAddr)
	ok, retry, err := s.limits.Reserve(ctx, key, time.Duration(s.cfg.Security.ResetResendIntervalSec)*time.Second)
	if err != nil {
		return errs.Internal(err)
	}
	if !ok {
		return errs.New(errs.CodeTooManyRequests, http.StatusTooManyRequests, "操作过于频繁，请 "+formatRetry(retry)+" 后重试")
	}

	u, err := s.users.ByEmail(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return errs.Internal(err)
	}

	switch u.Status {
	case model.StatusActive:
		token, err := s.onetime.IssueReset(ctx, u.ID)
		if err != nil {
			return errs.Internal(err)
		}
		if err := s.sendResetMail(ctx, u, token); err != nil {
			return errs.Unavailable(errs.CodeStorageUnavailable, "邮件发送失败，请稍后重试", err)
		}
	case model.StatusPendingVerify:
		// 未验证用户申请重置：实际执行"重发验证邮件"（API_SPEC 4.1）
		if err := s.sendVerifyMail(ctx, u); err != nil {
			return errs.Unavailable(errs.CodeStorageUnavailable, "邮件发送失败，请稍后重试", err)
		}
	}
	return nil
}

// ResetPassword 消费重置令牌并更新密码，随后吊销全部会话。
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	uid, err := s.onetime.ConsumeReset(ctx, token)
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			return errs.New(errs.CodeResetExpired, http.StatusBadRequest, "重置链接无效或已过期")
		}
		return errs.Internal(err)
	}

	hash, err := password.Hash(newPassword)
	if err != nil {
		return errs.Internal(err)
	}
	if err := s.users.UpdatePassword(ctx, uid, hash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errs.New(errs.CodeResetExpired, http.StatusBadRequest, "重置链接无效或已过期")
		}
		return errs.Internal(err)
	}

	// 密码已变更：所有设备强制重新登录
	if err := s.sessions.RevokeAll(ctx, uid); err != nil {
		s.log.Error("revoke_all_failed", slog.Any("error", err))
	}
	if err := s.onetime.RevokeAllReset(ctx, uid); err != nil {
		s.log.Warn("revoke_reset_tokens_failed", slog.Any("error", err))
	}
	return nil
}

// ---- 内部工具 ----

func (s *AuthService) checkLock(ctx context.Context, failKey string) error {
	n, err := s.limits.Value(ctx, failKey)
	if err != nil {
		return errs.Internal(err)
	}
	if n < int64(s.cfg.Security.LoginFailLimit) {
		return nil
	}
	ttl, err := s.limits.TTL(ctx, failKey)
	if err != nil || ttl < 0 {
		ttl = time.Duration(s.cfg.Security.LoginLockMin) * time.Minute
	}
	return errs.New(errs.CodeAccountLocked, http.StatusTooManyRequests,
		"失败次数过多，请 "+formatRetry(ttl)+" 后重试")
}

func (s *AuthService) recordFailure(ctx context.Context, failKey string) {
	window := time.Duration(s.cfg.Security.LoginLockMin) * time.Minute
	if _, err := s.limits.Incr(ctx, failKey, window); err != nil {
		s.log.Warn("login_fail_counter_error", slog.Any("error", err))
	}
}

// reserveMail 同时校验"最小间隔"与"每日上限"。
func (s *AuthService) reserveMail(ctx context.Context, kind string, uid uint64) error {
	id := strconv.FormatUint(uid, 10)
	cooldownKey := "blog:auth:mail_cd:" + kind + ":" + id
	ok, retry, err := s.limits.Reserve(ctx, cooldownKey, time.Duration(s.cfg.Security.VerifyResendIntervalSec)*time.Second)
	if err != nil {
		return errs.Internal(err)
	}
	if !ok {
		return errs.New(errs.CodeTooManyRequests, http.StatusTooManyRequests, "操作过于频繁，请 "+formatRetry(retry)+" 后重试")
	}

	dailyKey := "blog:auth:mail_day:" + kind + ":" + id
	allowed, _, err := s.limits.Allow(ctx, dailyKey, s.cfg.Security.VerifyResendDailyLimit, 24*time.Hour)
	if err != nil {
		return errs.Internal(err)
	}
	if !allowed {
		return errs.New(errs.CodeTooManyRequests, http.StatusTooManyRequests, "今日发送次数已达上限")
	}
	return nil
}

func (s *AuthService) sendVerifyMail(ctx context.Context, u *model.User) error {
	token, err := s.onetime.IssueVerify(ctx, u.ID)
	if err != nil {
		return err
	}
	link := withQuery(s.cfg.Mail.VerifyURLBase, "token", token)
	return s.mailer.Send(ctx, mailer.Message{
		To:      u.Email,
		Subject: "验证你的邮箱",
		Text: fmt.Sprintf("你好 %s：\n\n请在 24 小时内打开下面的链接完成邮箱验证：\n%s\n\n如果这不是你本人的操作，请忽略本邮件。",
			u.DisplayName(), link),
	})
}

func (s *AuthService) sendResetMail(ctx context.Context, u *model.User, token string) error {
	link := withQuery(s.cfg.Mail.ResetURLBase, "token", token)
	return s.mailer.Send(ctx, mailer.Message{
		To:      u.Email,
		Subject: "重置密码",
		Text: fmt.Sprintf("你好 %s：\n\n请在 15 分钟内打开下面的链接重置密码：\n%s\n\n如果这不是你本人的操作，请忽略本邮件，你的密码不会改变。",
			u.DisplayName(), link),
	})
}

func withQuery(base, key, val string) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + key + "=" + url.QueryEscape(val)
}

func formatRetry(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds()+0.5)) + " 秒"
	}
	return strconv.Itoa(int(d.Minutes()+0.5)) + " 分钟"
}
