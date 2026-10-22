package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// 票据 Redis 键前缀与有效期（TECH_DESIGN 11.1）。
const (
	ticketPrefix = "blog:ws:ticket:"
	ticketTTL    = 60 * time.Second
)

// TicketStore 一次性 WebSocket 连接票据，存 Redis，用完即删。
//
// 握手不在 URL 带 Token（会进日志），改带短时效一次性票据。
type TicketStore struct {
	rdb *redis.Client
}

// NewTicketStore 构造 TicketStore。
func NewTicketStore(rdb *redis.Client) *TicketStore {
	return &TicketStore{rdb: rdb}
}

// Issue 为用户签发票据。
func (s *TicketStore) Issue(ctx context.Context, userID uint64) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成票据随机数失败: %w", err)
	}
	ticket := "tk_" + hex.EncodeToString(buf)
	if err := s.rdb.Set(ctx, ticketPrefix+ticket, strconv.FormatUint(userID, 10), ticketTTL).Err(); err != nil {
		return "", fmt.Errorf("写入票据失败: %w", err)
	}
	return ticket, nil
}

// Consume 消费票据：GETDEL 保证一次性。无效或过期返回 0。
func (s *TicketStore) Consume(ctx context.Context, ticket string) (uint64, error) {
	v, err := s.rdb.GetDel(ctx, ticketPrefix+ticket).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("读取票据失败: %w", err)
	}
	uid, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, nil
	}
	return uid, nil
}
