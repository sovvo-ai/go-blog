package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/pkg/ratelimit"
)

func newLimiter(t *testing.T) (*ratelimit.Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return ratelimit.New(rdb), mr
}

func TestAllow_WithinAndBeyondLimit(t *testing.T) {
	l, _ := newLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ok, _, err := l.Allow(ctx, "k", 3, time.Minute)
		require.NoError(t, err)
		assert.True(t, ok, "第 %d 次应放行", i+1)
	}

	ok, retry, err := l.Allow(ctx, "k", 3, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "超过上限必须拒绝")
	assert.Greater(t, retry, time.Duration(0), "拒绝时应给出重试等待时间")
}

func TestAllow_WindowResets(t *testing.T) {
	l, mr := newLimiter(t)
	ctx := context.Background()

	ok, _, err := l.Allow(ctx, "k", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	ok, _, _ = l.Allow(ctx, "k", 1, time.Minute)
	require.False(t, ok)

	mr.FastForward(61 * time.Second)

	ok, _, err = l.Allow(ctx, "k", 1, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "窗口过期后应重新放行")
}

func TestReserve_Cooldown(t *testing.T) {
	l, mr := newLimiter(t)
	ctx := context.Background()

	ok, _, err := l.Reserve(ctx, "cd", 60*time.Second)
	require.NoError(t, err)
	assert.True(t, ok, "首次应抢占成功")

	ok, retry, err := l.Reserve(ctx, "cd", 60*time.Second)
	require.NoError(t, err)
	assert.False(t, ok, "冷却期内必须拒绝")
	assert.Greater(t, retry, time.Duration(0))

	mr.FastForward(61 * time.Second)
	ok, _, err = l.Reserve(ctx, "cd", 60*time.Second)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIncrValueReset(t *testing.T) {
	l, mr := newLimiter(t)
	ctx := context.Background()

	v, err := l.Incr(ctx, "fail", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), v)

	v, err = l.Incr(ctx, "fail", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(2), v)

	got, err := l.Value(ctx, "fail")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got)

	require.NoError(t, l.Reset(ctx, "fail"))

	got, err = l.Value(ctx, "fail")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got)

	// TTL 必须存在，否则计数永不过期会导致账号被永久锁定
	_, err = l.Incr(ctx, "fail2", time.Minute)
	require.NoError(t, err)
	ttl, err := l.TTL(ctx, "fail2")
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.False(t, mr.Exists("no-such-key"))
}

func TestValue_MissingKeyIsZero(t *testing.T) {
	l, _ := newLimiter(t)

	v, err := l.Value(context.Background(), "missing")
	require.NoError(t, err)
	assert.Equal(t, int64(0), v)
}
