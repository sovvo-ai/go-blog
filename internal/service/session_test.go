package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSessionStore(t *testing.T) (*SessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewSessionStore(rdb), mr
}

var fpA = Fingerprint{IP: "1.1.1.1", UA: "chrome"}

func TestCreateThenRotate(t *testing.T) {
	store, _ := newSessionStore(t)
	ctx := context.Background()

	token, err := store.Create(ctx, 42)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	newToken, uid, err := store.Rotate(ctx, token, fpA)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), uid)
	assert.NotEqual(t, token, newToken, "轮换必须换发新令牌")

	// 旧令牌已作废
	_, _, err = store.Rotate(ctx, token, fpA)
	require.Error(t, err)
}

func TestRotate_ConcurrentSameFingerprintDoesNotRevoke(t *testing.T) {
	store, _ := newSessionStore(t)
	ctx := context.Background()

	token, err := store.Create(ctx, 7)
	require.NoError(t, err)

	newToken, _, err := store.Rotate(ctx, token, fpA)
	require.NoError(t, err)

	// 同一设备并发刷新：判为并发，不吊销
	_, _, err = store.Rotate(ctx, token, fpA)
	assert.True(t, errors.Is(err, ErrSessionConcurrent), "应判定为并发刷新，实际: %v", err)

	// family 仍然有效，新令牌可以继续使用
	next, _, err := store.Rotate(ctx, newToken, fpA)
	require.NoError(t, err, "并发刷新不应导致整个会话被吊销")
	assert.NotEmpty(t, next)
}

func TestRotate_ReplayWithDifferentFingerprintRevokesFamily(t *testing.T) {
	store, _ := newSessionStore(t)
	ctx := context.Background()

	token, err := store.Create(ctx, 9)
	require.NoError(t, err)

	newToken, _, err := store.Rotate(ctx, token, fpA)
	require.NoError(t, err)

	// 同一令牌出现在另一台设备：视为重放，吊销整个 family
	_, _, err = store.Rotate(ctx, token, Fingerprint{IP: "9.9.9.9", UA: "curl"})
	assert.True(t, errors.Is(err, ErrSessionReplayed), "应判定为重放，实际: %v", err)

	// 被吊销后，原本合法的新令牌也不能再用
	_, _, err = store.Rotate(ctx, newToken, fpA)
	require.Error(t, err, "family 吊销后所有令牌必须失效")
}

func TestRotate_ReplayAfterWindowExpired(t *testing.T) {
	store, mr := newSessionStore(t)
	ctx := context.Background()

	token, err := store.Create(ctx, 11)
	require.NoError(t, err)
	_, _, err = store.Rotate(ctx, token, fpA)
	require.NoError(t, err)

	// 让 60 秒并发窗口过期，但 hash2family 仍保留 7 天
	mr.FastForward(2 * time.Minute)

	_, _, err = store.Rotate(ctx, token, fpA)
	assert.True(t, errors.Is(err, ErrSessionReplayed), "窗口过期后仍应识别为重放，实际: %v", err)
}

func TestRotate_UnknownToken(t *testing.T) {
	store, _ := newSessionStore(t)

	_, _, err := store.Rotate(context.Background(), "not-a-real-token", fpA)
	assert.True(t, errors.Is(err, ErrSessionInvalid), "未知令牌应判为无效，实际: %v", err)
}

func TestRevokeCurrent_OnlyAffectsThatSession(t *testing.T) {
	store, _ := newSessionStore(t)
	ctx := context.Background()

	phone, err := store.Create(ctx, 100)
	require.NoError(t, err)
	desktop, err := store.Create(ctx, 100)
	require.NoError(t, err)

	require.NoError(t, store.RevokeCurrent(ctx, phone))

	_, _, err = store.Rotate(ctx, phone, fpA)
	require.Error(t, err, "退出的设备必须失效")

	_, _, err = store.Rotate(ctx, desktop, fpA)
	require.NoError(t, err, "其他设备不应被影响")

	// 再次退出同一个令牌应当幂等
	require.NoError(t, store.RevokeCurrent(ctx, phone))
}

func TestRevokeAll(t *testing.T) {
	store, _ := newSessionStore(t)
	ctx := context.Background()

	a, err := store.Create(ctx, 200)
	require.NoError(t, err)
	b, err := store.Create(ctx, 200)
	require.NoError(t, err)

	require.NoError(t, store.RevokeAll(ctx, 200))

	_, _, err = store.Rotate(ctx, a, fpA)
	require.Error(t, err)
	_, _, err = store.Rotate(ctx, b, fpA)
	require.Error(t, err)
}

func TestFingerprint_StableAndDistinct(t *testing.T) {
	assert.Equal(t, fpA.String(), Fingerprint{IP: "1.1.1.1", UA: "chrome"}.String())
	assert.NotEqual(t, fpA.String(), Fingerprint{IP: "1.1.1.1", UA: "firefox"}.String())
	assert.Len(t, fpA.String(), 16, "指纹只取 8 字节十六进制，避免泄露原始 UA")
}

func TestHashToken_NeverStoresPlaintext(t *testing.T) {
	store, mr := newSessionStore(t)
	ctx := context.Background()

	token, err := store.Create(ctx, 1)
	require.NoError(t, err)

	keys := mr.Keys()
	for _, k := range keys {
		assert.NotContains(t, k, token, "Redis 键不能包含令牌原文")
	}
}
