// Command admincreate 创建或提升一个管理员账号。
//
// 为什么不在迁移脚本里插入管理员：
// 迁移文件会进版本库，把密码哈希写进去等于把口令公开。
// 这里从命令行参数或环境变量读取，跑完即弃。
//
// 用法：
//
//	go run ./cmd/admincreate -env-file deploy/.env \
//	    -username admin -email admin@example.com -password 'YourPass123'
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"go-blog/internal/authz"
	"go-blog/internal/config"
	"go-blog/internal/model"
	"go-blog/internal/pkg/password"
	"go-blog/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		envFile  string
		username string
		email    string
		pass     string
	)
	flag.StringVar(&envFile, "env-file", "", "本地环境文件路径，如 deploy/.env")
	flag.StringVar(&username, "username", "", "管理员用户名（创建时必填）")
	flag.StringVar(&email, "email", "", "管理员邮箱（必填，用于定位已有账号）")
	flag.StringVar(&pass, "password", "", "管理员密码（创建时必填；已存在则忽略）")
	flag.Parse()

	if envFile != "" {
		if err := config.LoadDotEnv(envFile); err != nil {
			return err
		}
	}

	dsn := strings.TrimSpace(os.Getenv("BLOG_DB_DSN"))
	if dsn == "" {
		return errors.New("缺少 BLOG_DB_DSN，可通过 -env-file 或环境变量提供")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("-email 必填")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("数据库不可用: %w", err)
	}

	repo := repository.NewUserRepo(db)
	existing, err := repo.ByEmail(ctx, email)
	switch {
	case err == nil:
		// 已存在：只提升角色，不改密码
		if err := db.WithContext(ctx).Model(&model.User{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"role":   uint8(authz.RoleAdmin),
				"status": model.StatusActive,
			}).Error; err != nil {
			return fmt.Errorf("更新角色失败: %w", err)
		}
		fmt.Printf("已将现有账号提升为管理员: id=%d email=%s\n", existing.ID, email)
		return nil
	case !errors.Is(err, repository.ErrNotFound):
		return fmt.Errorf("查询账号失败: %w", err)
	}

	// 新建
	if username == "" || pass == "" {
		return errors.New("创建新管理员需要 -username 与 -password")
	}
	if err := validate(username, pass); err != nil {
		return err
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return err
	}
	now := time.Now()
	u := &model.User{
		Username:        username,
		Email:           email,
		PasswordHash:    hash,
		Nickname:        username,
		Role:            uint8(authz.RoleAdmin),
		Status:          model.StatusActive,
		EmailVerifiedAt: &now,
	}
	if err := repo.Create(ctx, u); err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}
	fmt.Printf("已创建管理员: id=%d username=%s email=%s\n", u.ID, u.Username, email)
	return nil
}

// validate 复用与注册一致的基本校验，避免建出无法使用的账号。
func validate(username, pwd string) error {
	if l := len(username); l < 4 || l > 20 {
		return errors.New("用户名长度需为 4–20 位")
	}
	if len(pwd) < 8 || len(pwd) > 32 {
		return errors.New("密码长度需为 8–32 位")
	}
	return nil
}
