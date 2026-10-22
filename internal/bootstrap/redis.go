package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"go-blog/internal/config"
)

// NewRedis 建立 Redis 客户端并做启动探活。
//
// 地址、密码、库编号全部来自配置，任何一个都不允许硬编码。
func NewRedis(cfg *config.Config, log *slog.Logger) (*redis.Client, func(), error) {
	client := redis.NewClient(&redis.Options{
		Addr:        cfg.Redis.Addr,
		Password:    cfg.Redis.Password,
		DB:          cfg.Redis.DB,
		PoolSize:    cfg.Redis.PoolSize,
		DialTimeout: time.Duration(cfg.Redis.DialTimeoutSec) * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("Redis 探活失败（%s）: %w", cfg.Redis.Addr, err)
	}

	cleanup := func() {
		if err := client.Close(); err != nil {
			log.Warn("redis_close_failed", slog.Any("error", err))
		}
	}
	return client, cleanup, nil
}

// redisPinger 把 Redis 客户端适配成健康检查所需的签名。
type redisPinger struct{ client *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }
