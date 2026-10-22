package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrTokenInvalid 表示一次性令牌不存在、已消费或已过期。
var ErrTokenInvalid = errors.New("令牌无效或已过期")

const (
	// verifyTokenTTL 邮箱验证令牌 24 小时（TECH_DESIGN 6.4）
	verifyTokenTTL = 24 * time.Hour
	// resetTokenTTL 密码重置令牌 15 分钟
	resetTokenTTL = 15 * time.Minute
)

// consumeScript 原子完成"读取并删除"，保证令牌只能消费一次。
// 不用 GETDEL 是为了兼容不支持该命令的 Redis 兼容实现。
var consumeScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if v then
  redis.call('DEL', KEYS[1])
end
return v
`)

// OneTimeTokenStore 管理邮箱验证与密码重置令牌。
//
// 只存 sha256(token)：数据库和 Redis 都不留原文，
// 邮件里的链接是唯一持有凭证的地方。
type OneTimeTokenStore struct {
	rdb *redis.Client
}

// NewOneTimeTokenStore 构造 OneTimeTokenStore。
func NewOneTimeTokenStore(rdb *redis.Client) *OneTimeTokenStore {
	return &OneTimeTokenStore{rdb: rdb}
}

func verifyKey(hash string) string { return "blog:auth:email_verify:" + hash }
func resetKey(hash string) string  { return "blog:auth:pwd_reset:" + hash }
func resetIndex(uid uint64) string { return "blog:auth:pwd_reset_user:" + strconv.FormatUint(uid, 10) }

// IssueVerify 签发邮箱验证令牌。
func (s *OneTimeTokenStore) IssueVerify(ctx context.Context, uid uint64) (string, error) {
	raw, err := newOneTimeToken()
	if err != nil {
		return "", err
	}
	hash := hashToken(raw)
	if err := s.rdb.Set(ctx, verifyKey(hash), uid, verifyTokenTTL).Err(); err != nil {
		return "", fmt.Errorf("写入验证令牌失败: %w", err)
	}
	return raw, nil
}

// ConsumeVerify 消费邮箱验证令牌，返回用户 ID。
func (s *OneTimeTokenStore) ConsumeVerify(ctx context.Context, token string) (uint64, error) {
	return s.consume(ctx, verifyKey(hashToken(token)))
}

// IssueReset 签发密码重置令牌，并加入该用户的重置令牌索引。
func (s *OneTimeTokenStore) IssueReset(ctx context.Context, uid uint64) (string, error) {
	raw, err := newOneTimeToken()
	if err != nil {
		return "", err
	}
	hash := hashToken(raw)

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, resetKey(hash), uid, resetTokenTTL)
	pipe.SAdd(ctx, resetIndex(uid), hash)
	pipe.Expire(ctx, resetIndex(uid), resetTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("写入重置令牌失败: %w", err)
	}
	return raw, nil
}

// ConsumeReset 消费密码重置令牌，返回用户 ID。
func (s *OneTimeTokenStore) ConsumeReset(ctx context.Context, token string) (uint64, error) {
	hash := hashToken(token)
	uid, err := s.consume(ctx, resetKey(hash))
	if err != nil {
		return 0, err
	}
	// 清理索引，失败不影响主流程
	_ = s.rdb.SRem(ctx, resetIndex(uid), hash).Err()
	return uid, nil
}

// RevokeAllReset 清理该用户全部未使用的重置令牌（改密后调用）。
func (s *OneTimeTokenStore) RevokeAllReset(ctx context.Context, uid uint64) error {
	hashes, err := s.rdb.SMembers(ctx, resetIndex(uid)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, h := range hashes {
		keys = append(keys, resetKey(h))
	}
	keys = append(keys, resetIndex(uid))
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

func (s *OneTimeTokenStore) consume(ctx context.Context, key string) (uint64, error) {
	val, err := consumeScript.Run(ctx, s.rdb, []string{key}).Result()
	if errors.Is(err, redis.Nil) || val == nil {
		return 0, ErrTokenInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("消费令牌失败: %w", err)
	}

	switch v := val.(type) {
	case int64:
		return uint64(v), nil
	case string:
		uid, convErr := strconv.ParseUint(v, 10, 64)
		if convErr != nil {
			return 0, fmt.Errorf("令牌内容非法: %w", convErr)
		}
		return uid, nil
	default:
		return 0, fmt.Errorf("令牌内容类型异常: %T", val)
	}
}

// newOneTimeToken 生成 24 字节随机串，比 Refresh Token 略短但仍有 192 位熵。
func newOneTimeToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
