package service

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOneTimeStore(t *testing.T) (*OneTimeTokenStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewOneTimeTokenStore(rdb), mr
}

func TestVerifyToken_OneTimeUse(t *testing.T) {
	store, _ := newOneTimeStore(t)
	ctx := context.Background()

	token, err := store.IssueVerify(ctx, 55)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	uid, err := store.ConsumeVerify(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, uint64(55), uid)

	// 第二次消费必须失败：这是"一次性"的核心语义
	_, err = store.ConsumeVerify(ctx, token)
	assert.True(t, errors.Is(err, ErrTokenInvalid), "令牌只能消费一次，实际: %v", err)
}

func TestVerifyToken_Expired(t *testing.T) {
	store, mr := newOneTimeStore(t)
	ctx := context.Background()

	token, err := store.IssueVerify(ctx, 56)
	require.NoError(t, err)

	mr.FastForward(25 * 60 * 60 * 1e9) // 25 小时

	_, err = store.ConsumeVerify(ctx, token)
	assert.True(t, errors.Is(err, ErrTokenInvalid))
}

func TestResetToken_OneTimeUseAndIndex(t *testing.T) {
	store, mr := newOneTimeStore(t)
	ctx := context.Background()

	token, err := store.IssueReset(ctx, 77)
	require.NoError(t, err)

	uid, err := store.ConsumeReset(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, uint64(77), uid)

	_, err = store.ConsumeReset(ctx, token)
	assert.True(t, errors.Is(err, ErrTokenInvalid))

	// 消费后索引中的成员应被移除；Redis 会自动删除空集合，所以键也应消失
	assert.False(t, mr.Exists("blog:auth:pwd_reset_user:77"), "索引成员清空后集合键应被删除")
}

func TestRevokeAllReset(t *testing.T) {
	store, _ := newOneTimeStore(t)
	ctx := context.Background()

	a, err := store.IssueReset(ctx, 88)
	require.NoError(t, err)
	b, err := store.IssueReset(ctx, 88)
	require.NoError(t, err)

	require.NoError(t, store.RevokeAllReset(ctx, 88))

	_, err = store.ConsumeReset(ctx, a)
	assert.Error(t, err, "改密后旧的未使用令牌必须失效")
	_, err = store.ConsumeReset(ctx, b)
	assert.Error(t, err)
}

func TestTokenKeysDoNotContainPlaintext(t *testing.T) {
	store, mr := newOneTimeStore(t)
	ctx := context.Background()

	token, err := store.IssueVerify(ctx, 99)
	require.NoError(t, err)

	for _, k := range mr.Keys() {
		assert.NotContains(t, k, token, "Redis 键不能包含令牌原文")
	}
}
