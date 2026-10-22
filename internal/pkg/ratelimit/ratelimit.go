// Package ratelimit 基于 Redis 实现固定窗口限流与冷却期。
//
// 固定窗口实现简单、误差可接受（窗口边界最坏翻倍），
// 对登录、注册、发信这类低频操作足够；不做令牌桶是刻意的。
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter 是 Redis 限流器。
type Limiter struct {
	rdb *redis.Client
}

// New 构造 Limiter。
func New(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb} }

// Allow 在 window 内最多放行 limit 次。
// 返回是否放行，以及被拒时建议的重试等待时长。
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit <= 0 {
		return false, window, nil
	}

	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window) // 只在键无 TTL 时设置，避免每次请求都续期
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, fmt.Errorf("限流计数失败: %w", err)
	}

	count := incr.Val()
	if count <= int64(limit) {
		return true, 0, nil
	}

	ttl, err := l.rdb.TTL(ctx, key).Result()
	if err != nil || ttl < 0 {
		ttl = window
	}
	return false, ttl, nil
}

// Reserve 抢占一个冷却期：窗口内只允许成功一次，用于"重发邮件最小间隔"。
func (l *Limiter) Reserve(ctx context.Context, key string, window time.Duration) (bool, time.Duration, error) {
	ok, err := l.rdb.SetNX(ctx, key, 1, window).Result()
	if err != nil {
		return false, 0, fmt.Errorf("抢占冷却期失败: %w", err)
	}
	if ok {
		return true, 0, nil
	}
	ttl, err := l.rdb.TTL(ctx, key).Result()
	if err != nil || ttl < 0 {
		ttl = window
	}
	return false, ttl, nil
}

// Incr 累加计数并按需设置 TTL，用于登录失败次数这类需要保留态的场景。
func (l *Limiter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("累加计数失败: %w", err)
	}
	return incr.Val(), nil
}

// Value 读取当前计数，不存在时返回 0。
func (l *Limiter) Value(ctx context.Context, key string) (int64, error) {
	v, err := l.rdb.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return v, nil
}

// TTL 返回键剩余存活时间。
func (l *Limiter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return l.rdb.TTL(ctx, key).Result()
}

// Reset 清除计数（登录成功后调用）。
func (l *Limiter) Reset(ctx context.Context, key string) error {
	err := l.rdb.Del(ctx, key).Err()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}
