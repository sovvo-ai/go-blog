package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/pkg/password"
	"go-blog/internal/repository"
)

// UserService 负责个人资料与密码。
type UserService struct {
	users    UserStore
	sessions *SessionStore
	onetime  *OneTimeTokenStore
	log      *slog.Logger
}

// NewUserService 构造 UserService。
func NewUserService(users UserStore, sessions *SessionStore, onetime *OneTimeTokenStore, log *slog.Logger) *UserService {
	return &UserService{users: users, sessions: sessions, onetime: onetime, log: log}
}

// Profile 返回当前用户完整信息。
func (s *UserService) Profile(ctx context.Context, uid uint64) (*model.User, error) {
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.Unauthorized("账号不存在")
		}
		return nil, errs.Internal(err)
	}
	return u, nil
}

// UpdateProfile 更新昵称与简介。nil 表示本次不修改该字段。
func (s *UserService) UpdateProfile(ctx context.Context, uid uint64, nickname, bio *string) (*model.User, error) {
	u, err := s.Profile(ctx, uid)
	if err != nil {
		return nil, err
	}

	newNick := u.Nickname
	if nickname != nil {
		v := strings.TrimSpace(*nickname)
		if l := len([]rune(v)); l < 1 || l > 32 {
			return nil, badRequest("昵称长度需为 1–32 个字符", "nickname")
		}
		newNick = v
	}

	newBio := u.Bio
	if bio != nil {
		v := strings.TrimSpace(*bio)
		if len([]rune(v)) > 255 {
			return nil, badRequest("简介不能超过 255 个字符", "bio")
		}
		newBio = v
	}

	if newNick == u.Nickname && newBio == u.Bio {
		return u, nil // 无变化，不写库
	}
	if err := s.users.UpdateProfile(ctx, uid, newNick, newBio); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errs.Unauthorized("账号不存在")
		}
		return nil, errs.Internal(err)
	}

	u.Nickname = newNick
	u.Bio = newBio
	return u, nil
}

// ChangePassword 修改密码：校验原密码，更新后吊销全部会话。
func (s *UserService) ChangePassword(ctx context.Context, uid uint64, oldPwd, newPwd string) error {
	if oldPwd == "" {
		return badRequest("请输入原密码", "old_password")
	}
	if err := validatePassword(newPwd); err != nil {
		return err
	}
	if oldPwd == newPwd {
		return badRequest("新密码不能与原密码相同", "new_password")
	}

	u, err := s.Profile(ctx, uid)
	if err != nil {
		return err
	}
	if err := password.Verify(oldPwd, u.PasswordHash); err != nil {
		return errs.New(errs.CodeOldPasswordBad, 400, "原密码错误")
	}

	hash, err := password.Hash(newPwd)
	if err != nil {
		return errs.Internal(err)
	}
	if err := s.users.UpdatePassword(ctx, uid, hash); err != nil {
		return errs.Internal(err)
	}

	// 密码已变更，旧凭证可能已泄露，必须全线重新认证（TECH_DESIGN 6.3.5）
	if err := s.sessions.RevokeAll(ctx, uid); err != nil {
		s.log.Error("revoke_all_failed", slog.Any("error", err))
	}
	if err := s.onetime.RevokeAllReset(ctx, uid); err != nil {
		s.log.Warn("revoke_reset_tokens_failed", slog.Any("error", err))
	}
	return nil
}

// PublicProfile 返回公开主页信息：不含 email、status、登录记录。
func (s *UserService) PublicProfile(ctx context.Context, uid uint64) (*model.User, int64, error) {
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, errs.NotFound("用户不存在")
		}
		return nil, 0, errs.Internal(err)
	}
	// 被禁用的账号主页仍可访问（内容归属需要），但不展示登录痕迹。
	//
	// TODO(阶段 7 用户注销)：PRD 3.1 要求对已注销用户显示"已注销用户"，
	// 需在此处（及文章/评论的作者展示处）统一做匿名化，当前 V1.0 尚未实现注销流程。
	count, err := s.users.PublishedArticleCount(ctx, uid)
	if err != nil {
		return nil, 0, errs.Internal(err)
	}
	return u, count, nil
}
