package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// 会话相关的哨兵错误，handler 层据此映射业务码。
var (
	// ErrSessionInvalid 表示令牌不存在、已过期或从未签发。
	ErrSessionInvalid = errors.New("会话无效")
	// ErrSessionReplayed 表示检测到已轮换的令牌被再次使用，需要吊销整个 family。
	ErrSessionReplayed = errors.New("检测到令牌重放")
	// ErrSessionConcurrent 表示同一设备的并发刷新，客户端重试一次即可。
	ErrSessionConcurrent = errors.New("并发刷新")
)

const (
	// refreshTTL 与 TECH_DESIGN 6.1 一致：7 天。
	refreshTTL = 7 * 24 * time.Hour
	// metaTTL 是并发刷新的判定窗口（D-D：交接窗口关闭后的替代方案）。
	metaTTL = 60 * time.Second
)

// Fingerprint 是并发刷新判定用的设备指纹。
// 只用于区分"同一客户端重试"与"令牌被复制到另一处使用"，不用于鉴权。
type Fingerprint struct {
	IP string
	UA string
}

// String 归一化为可比较的短串。
func (f Fingerprint) String() string {
	sum := sha256.Sum256([]byte(f.IP + "|" + f.UA))
	return hex.EncodeToString(sum[:8])
}

// SessionStore 管理 Refresh Token 的签发、轮换与吊销（TECH_DESIGN 6.3）。
//
// 服务端只存 sha256(token)，不存原文：即使 Redis 被读取，也拿不到可用凭证。
type SessionStore struct {
	rdb *redis.Client
	now func() time.Time
}

// NewSessionStore 构造 SessionStore。
func NewSessionStore(rdb *redis.Client) *SessionStore {
	return &SessionStore{rdb: rdb, now: time.Now}
}

func rtKey(hash string) string       { return "blog:auth:rt:" + hash }
func familyKey(family string) string { return "blog:auth:family:" + family }
func userFamilies(uid uint64) string { return "blog:auth:user_families:" + strconv.FormatUint(uid, 10) }
func hash2Family(hash string) string { return "blog:auth:hash2family:" + hash }
func rotateMeta(hash string) string  { return "blog:auth:rt:meta:" + hash }

// Create 登录成功时签发一组新的 Refresh Token，并开启新 family。
func (s *SessionStore) Create(ctx context.Context, uid uint64) (string, error) {
	raw, err := newToken()
	if err != nil {
		return "", err
	}
	family, err := newToken()
	if err != nil {
		return "", err
	}
	hash := hashToken(raw)
	now := s.now().Unix()

	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, rtKey(hash), "uid", uid, "family", family, "issued_at", now)
	pipe.Expire(ctx, rtKey(hash), refreshTTL)
	pipe.SAdd(ctx, familyKey(family), hash)
	pipe.Expire(ctx, familyKey(family), refreshTTL)
	pipe.Set(ctx, hash2Family(hash), family, refreshTTL)
	pipe.SAdd(ctx, userFamilies(uid), family)
	pipe.Expire(ctx, userFamilies(uid), refreshTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("写入会话失败: %w", err)
	}
	return raw, nil
}

// rotateScript 原子完成"删旧 + 建新"，避免新旧令牌同时有效的中间态。
//
// KEYS: oldRT, familySet, newRT, hash2family(new), meta(old)
// ARGV: oldHash, newHash, ttlSeconds, nowUnix, fingerprint
//
// 注意：旧 hash 的 hash2family 映射必须保留，
// 否则已轮换的令牌再次出现时无法识别为重放。
var rotateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return 0
end
local uid = redis.call('HGET', KEYS[1], 'uid')
local family = redis.call('HGET', KEYS[1], 'family')
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[2], ARGV[1])
redis.call('HSET', KEYS[3], 'uid', uid, 'family', family, 'issued_at', ARGV[4])
redis.call('EXPIRE', KEYS[3], ARGV[3])
redis.call('SADD', KEYS[2], ARGV[2])
redis.call('EXPIRE', KEYS[2], ARGV[3])
redis.call('SET', KEYS[4], family, 'EX', ARGV[3])
redis.call('HSET', KEYS[5], 'uid', uid, 'fp', ARGV[5], 'at', ARGV[4])
redis.call('EXPIRE', KEYS[5], 60)
return 1
`)

// Rotate 用旧令牌换取新令牌。
//
// 三种失败要继续区分，否则要么把正常用户踢下线，要么放过真实重放：
//   - 并发刷新：60 秒内、指纹一致 → ErrSessionConcurrent，让客户端重试
//   - 重放：旧哈希在 hash2family 中存在但指纹不符或超窗口 → 吊销整个 family
//   - 无效：三者都不存在 → ErrSessionInvalid
func (s *SessionStore) Rotate(ctx context.Context, token string, fp Fingerprint) (string, uint64, error) {
	if token == "" {
		return "", 0, ErrSessionInvalid
	}
	oldHash := hashToken(token)

	newRaw, err := newToken()
	if err != nil {
		return "", 0, err
	}
	newHash := hashToken(newRaw)

	keys := []string{
		rtKey(oldHash),
		"", // family 集合在脚本执行前未知，先占位，下面用单独命令补
		rtKey(newHash),
		hash2Family(newHash),
		rotateMeta(oldHash),
	}

	// 先取 family，脚本里需要它的集合键。多一次往返换取脚本可读性。
	family, err := s.familyOfHash(ctx, oldHash)
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", 0, err
	}
	if family == "" {
		return "", 0, s.diagnose(ctx, oldHash, fp)
	}
	keys[1] = familyKey(family)

	res, err := rotateScript.Run(ctx, s.rdb, keys,
		oldHash, newHash, int(refreshTTL.Seconds()), s.now().Unix(), fp.String(),
	).Int()
	if err != nil {
		return "", 0, fmt.Errorf("轮换令牌失败: %w", err)
	}
	if res == 0 {
		// 读到 family 之后、脚本执行之前令牌被并发消费掉了
		return "", 0, s.diagnose(ctx, oldHash, fp)
	}

	uid, err := s.rdb.HGet(ctx, rtKey(newHash), "uid").Uint64()
	if err != nil {
		return "", 0, fmt.Errorf("读取新会话失败: %w", err)
	}
	return newRaw, uid, nil
}

// diagnose 在轮换失败时判定原因，并执行必要的吊销。
func (s *SessionStore) diagnose(ctx context.Context, oldHash string, fp Fingerprint) error {
	// 1) 并发刷新：窗口内、同一指纹
	meta, err := s.rdb.HGetAll(ctx, rotateMeta(oldHash)).Result()
	if err == nil && len(meta) > 0 {
		if meta["fp"] == fp.String() {
			return ErrSessionConcurrent
		}
		// 指纹不符：令牌可能被复制到另一台设备
	}

	// 2) 重放：曾经签发过，但已被轮换
	family, err := s.rdb.Get(ctx, hash2Family(oldHash)).Result()
	if err == nil && family != "" {
		if revokeErr := s.revokeFamily(ctx, family); revokeErr != nil {
			return fmt.Errorf("吊销 family 失败: %w", revokeErr)
		}
		return ErrSessionReplayed
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}

	return ErrSessionInvalid
}

func (s *SessionStore) familyOfHash(ctx context.Context, hash string) (string, error) {
	return s.rdb.HGet(ctx, rtKey(hash), "family").Result()
}

// RevokeCurrent 退出登录：只吊销当前令牌所属 family，不影响其他设备。
func (s *SessionStore) RevokeCurrent(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	family, err := s.familyOfHash(ctx, hashToken(token))
	if errors.Is(err, redis.Nil) {
		return nil // 已失效，视为成功
	}
	if err != nil {
		return err
	}
	return s.revokeFamily(ctx, family)
}

// RevokeAll 吊销该用户的全部会话（改密码、被禁用、检测到重放时使用）。
func (s *SessionStore) RevokeAll(ctx context.Context, uid uint64) error {
	families, err := s.rdb.SMembers(ctx, userFamilies(uid)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	for _, f := range families {
		if err := s.revokeFamily(ctx, f); err != nil {
			return err
		}
	}
	if err := s.rdb.Del(ctx, userFamilies(uid)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

func (s *SessionStore) revokeFamily(ctx context.Context, family string) error {
	hashes, err := s.rdb.SMembers(ctx, familyKey(family)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if len(hashes) > 0 {
		keys := make([]string, 0, len(hashes))
		for _, h := range hashes {
			keys = append(keys, rtKey(h))
		}
		if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
			return err
		}
	}
	return s.rdb.Del(ctx, familyKey(family)).Err()
}

// newToken 生成 32 字节随机串（base64url，无填充）。
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机令牌失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken 计算令牌摘要，作为 Redis 键与存储值。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
