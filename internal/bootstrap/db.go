package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"go-blog/internal/config"
)

// NewDB 建立 MySQL 连接池并做一次启动探活。
//
// 迁移不在这里执行：多实例同时启动会抢锁，失败也无法回滚。
// 迁移由部署脚本显式执行，启动时只连库（TECH_DESIGN 17 章）。
func NewDB(cfg *config.Config, log *slog.Logger) (*gorm.DB, func(), error) {
	gdb, err := gorm.Open(mysql.Open(cfg.DB.DSN), &gorm.Config{
		Logger: newGormLogger(log, cfg.App.IsProduction()),
		// 预编译语句复用，减少解析开销
		PrepareStmt: true,
		// 关闭 GORM 默认的嵌套事务（每次写操作自动包事务），
		// 事务边界由 service 层显式控制
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("获取底层连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.DB.ConnMaxLifetimeMin) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("MySQL 探活失败: %w", err)
	}

	cleanup := func() {
		if err := sqlDB.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			log.Warn("mysql_close_failed", slog.Any("error", err))
		}
	}
	return gdb, cleanup, nil
}

// sqlPinger 把 *sql.DB 适配成 Ping(context) error 形式，供健康检查使用。
type sqlPinger struct{ db *sql.DB }

func (p sqlPinger) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }
