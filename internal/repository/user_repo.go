// Package repository 封装数据访问。service 层不直接接触 GORM。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"go-blog/internal/model"
)

// 数据访问层的哨兵错误，service 层据此映射业务码。
var (
	ErrNotFound  = errors.New("记录不存在")
	ErrDuplicate = errors.New("唯一键冲突")
	ErrConflict  = errors.New("并发冲突（乐观锁或状态已变更）")
)

// UserRepo 是 users 表的数据访问对象。
type UserRepo struct {
	db *gorm.DB
}

// NewUserRepo 构造 UserRepo。
func NewUserRepo(db *gorm.DB) *UserRepo { return &UserRepo{db: db} }

// Create 插入新用户。username 或 email 冲突时返回 ErrDuplicate。
func (r *UserRepo) Create(ctx context.Context, u *model.User) error {
	err := r.db.WithContext(ctx).Create(u).Error
	if err == nil {
		return nil
	}
	if isDuplicate(err) {
		return ErrDuplicate
	}
	return err
}

// ByID 按主键查询。
func (r *UserRepo) ByID(ctx context.Context, id uint64) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&u).Error
	return wrap(&u, err)
}

// ByIDs 批量按主键查询。缺失的 ID 不在返回值中。
func (r *UserRepo) ByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error) {
	if len(ids) == 0 {
		return map[uint64]model.User{}, nil
	}
	var rows []model.User
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]model.User, len(rows))
	for _, u := range rows {
		out[u.ID] = u
	}
	return out, nil
}

// ByAccount 按用户名或邮箱查询，用于登录。
func (r *UserRepo) ByAccount(ctx context.Context, account string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).
		Where("username = ? OR email = ?", account, account).
		Take(&u).Error
	return wrap(&u, err)
}

// ByEmail 按邮箱查询。
func (r *UserRepo) ByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("email = ?", email).Take(&u).Error
	return wrap(&u, err)
}

// ExistsUsername 判断用户名是否已被占用。
func (r *UserRepo) ExistsUsername(ctx context.Context, username string) (bool, error) {
	return r.exists(ctx, "username = ?", username)
}

// ExistsEmail 判断邮箱是否已注册。
func (r *UserRepo) ExistsEmail(ctx context.Context, email string) (bool, error) {
	return r.exists(ctx, "email = ?", email)
}

func (r *UserRepo) exists(ctx context.Context, cond string, args ...any) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Where(cond, args...).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateProfile 只更新昵称与简介。
// 用 map 而不是 struct：struct 会跳过零值，导致"清空简介"失效。
func (r *UserRepo) UpdateProfile(ctx context.Context, id uint64, nickname, bio string) error {
	// updated_at 显式赋值，避免依赖 ON UPDATE 触发条件
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"nickname":   nickname,
			"bio":        bio,
			"updated_at": time.Now(),
		})
	return affected(res)
}

// UpdatePassword 更新密码哈希。
func (r *UserRepo) UpdatePassword(ctx context.Context, id uint64, hash string) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"password_hash": hash,
			"updated_at":    time.Now(),
		})
	return affected(res)
}

// MarkEmailVerified 把账号置为 active 并记录验证时间。
// 只对 pending_verify 生效，重复调用不会覆盖已验证状态之外的东西。
func (r *UserRepo) MarkEmailVerified(ctx context.Context, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND status = ?", id, model.StatusPendingVerify).
		Updates(map[string]any{
			"status":            model.StatusActive,
			"email_verified_at": at,
			"updated_at":        at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 已经是 active 或 disabled：幂等成功，但要能区分"用户不存在"
		var count int64
		if err := r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// TouchLogin 记录最近登录时间与 IP。
// 注意加 updated_at = updated_at：计数类更新不应改变内容更新时间。
func (r *UserRepo) TouchLogin(ctx context.Context, id uint64, at time.Time, ip string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_login_at": at,
			"last_login_ip": ip,
			"updated_at":    gorm.Expr("updated_at"),
		}).Error
}

// PublishedArticleCount 统计作者已发布文章数，用于公开主页。
func (r *UserRepo) PublishedArticleCount(ctx context.Context, id uint64) (int64, error) {
	var count int64
	// articles.status = 2 表示 published（DATABASE.md 5.1）
	err := r.db.WithContext(ctx).Table("articles").
		Where("author_id = ? AND status = ? AND deleted_at IS NULL", id, 2).
		Count(&count).Error
	return count, err
}

// wrap 统一把 GORM 的"未找到"转换成本层哨兵错误。
func wrap(u *model.User, err error) (*model.User, error) {
	if err == nil {
		return u, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return nil, err
}

func affected(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// isDuplicate 判断是否唯一键冲突（MySQL 1062）。
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// DuplicateKey 从错误信息中提取冲突的唯一键名（uk_username / uk_email）。
// 用于把 1062 精确映射成 20001 / 20002；识别不出来时返回空串。
func DuplicateKey(err error) string {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return ""
	}
	switch {
	case strings.Contains(me.Message, "uk_username"):
		return "username"
	case strings.Contains(me.Message, "uk_email"):
		return "email"
	default:
		return ""
	}
}
