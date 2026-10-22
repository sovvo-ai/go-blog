// Package model 定义与数据库表一一对应的模型。
package model

import "time"

// 用户状态，取值与 DATABASE.md 5.2 一致。
const (
	StatusPendingVerify uint8 = 0 // 待邮箱验证，不可登录
	StatusActive        uint8 = 1 // 正常
	StatusDisabled      uint8 = 2 // 被管理员禁用
)

// User 对应 users 表。
//
// 注意：users 不做物理删除，禁用用 status 表达（DATABASE.md 4.1）。
type User struct {
	ID              uint64     `gorm:"column:id;primaryKey"`
	Username        string     `gorm:"column:username"`
	Email           string     `gorm:"column:email"`
	PasswordHash    string     `gorm:"column:password_hash"`
	Nickname        string     `gorm:"column:nickname"`
	AvatarURL       string     `gorm:"column:avatar_url"`
	Bio             string     `gorm:"column:bio"`
	Role            uint8      `gorm:"column:role"`
	Status          uint8      `gorm:"column:status"`
	EmailVerifiedAt *time.Time `gorm:"column:email_verified_at"`
	LastLoginAt     *time.Time `gorm:"column:last_login_at"`
	LastLoginIP     string     `gorm:"column:last_login_ip"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

// TableName 指定表名，避免 GORM 复数化推断出错。
func (User) TableName() string { return "users" }

// IsActive 判断账号是否可用。
func (u *User) IsActive() bool { return u.Status == StatusActive }

// DisplayName 返回展示名：昵称为空时回退到用户名。
func (u *User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}
