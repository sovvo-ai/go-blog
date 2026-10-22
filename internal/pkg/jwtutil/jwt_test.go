package jwtutil_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/pkg/jwtutil"
)

const secret = "unit_test_secret_at_least_32_characters"

func TestNew_RejectsWeakSecret(t *testing.T) {
	_, err := jwtutil.New("short", "go-blog", time.Minute)
	assert.Error(t, err)

	_, err = jwtutil.New(secret, "go-blog", 0)
	assert.Error(t, err)
}

func TestIssueAndParse(t *testing.T) {
	m, err := jwtutil.New(secret, "go-blog", 15*time.Minute)
	require.NoError(t, err)

	now := time.Now()
	token, exp, err := m.Issue(1024, now)
	require.NoError(t, err)
	assert.WithinDuration(t, now.Add(15*time.Minute), exp, time.Second)

	claims, err := m.Parse(token)
	require.NoError(t, err)

	uid, err := claims.UIDValue()
	require.NoError(t, err)
	assert.Equal(t, uint64(1024), uid)
	assert.Equal(t, "go-blog", claims.Issuer)
}

func TestParse_ExpiredToken(t *testing.T) {
	m, err := jwtutil.New(secret, "go-blog", time.Minute)
	require.NoError(t, err)

	// 签发一个已经过期的令牌
	token, _, err := m.Issue(1, time.Now().Add(-2*time.Hour))
	require.NoError(t, err)

	_, err = m.Parse(token)
	assert.Error(t, err, "过期令牌必须被拒绝")
}

func TestParse_RejectsWrongSecret(t *testing.T) {
	issuer, err := jwtutil.New(secret, "go-blog", time.Minute)
	require.NoError(t, err)
	verifier, err := jwtutil.New("another_secret_at_least_32_characters", "go-blog", time.Minute)
	require.NoError(t, err)

	token, _, err := issuer.Issue(1, time.Now())
	require.NoError(t, err)

	_, err = verifier.Parse(token)
	assert.Error(t, err, "换密钥后旧令牌必须失效")
}

func TestParse_RejectsWrongIssuer(t *testing.T) {
	issuer, err := jwtutil.New(secret, "someone-else", time.Minute)
	require.NoError(t, err)
	verifier, err := jwtutil.New(secret, "go-blog", time.Minute)
	require.NoError(t, err)

	token, _, err := issuer.Issue(1, time.Now())
	require.NoError(t, err)

	_, err = verifier.Parse(token)
	assert.Error(t, err)
}

// TestParse_RejectsAlgorithmConfusion 覆盖 alg 混淆攻击：
// 攻击者把头部改成 alg=none 或 RS256，试图绕过签名校验。
func TestParse_RejectsAlgorithmConfusion(t *testing.T) {
	m, err := jwtutil.New(secret, "go-blog", time.Minute)
	require.NoError(t, err)

	// 用 none 算法伪造
	claims := jwt.MapClaims{
		"uid": "1", "iss": "go-blog",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = m.Parse(unsigned)
	assert.Error(t, err, "alg=none 必须被拒绝")
}

func TestParse_Garbage(t *testing.T) {
	m, err := jwtutil.New(secret, "go-blog", time.Minute)
	require.NoError(t, err)

	for _, s := range []string{"", "abc", "a.b.c"} {
		_, err := m.Parse(s)
		assert.Error(t, err, "非法令牌必须报错: %q", s)
	}
}
