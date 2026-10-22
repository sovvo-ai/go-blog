package password_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/pkg/password"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := password.Hash("abc12345")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))
	assert.NotContains(t, hash, "abc12345", "哈希串中不得出现明文")

	require.NoError(t, password.Verify("abc12345", hash))
}

func TestVerify_WrongPassword(t *testing.T) {
	hash, err := password.Hash("abc12345")
	require.NoError(t, err)

	err = password.Verify("abc12346", hash)
	assert.ErrorIs(t, err, password.ErrMismatch)
}

func TestHash_UniqueSalt(t *testing.T) {
	h1, err := password.Hash("same-password")
	require.NoError(t, err)
	h2, err := password.Hash("same-password")
	require.NoError(t, err)

	assert.NotEqual(t, h1, h2, "相同密码必须产生不同哈希（随机盐）")
	require.NoError(t, password.Verify("same-password", h1))
	require.NoError(t, password.Verify("same-password", h2))
}

func TestHash_EmptyPassword(t *testing.T) {
	_, err := password.Hash("")
	assert.Error(t, err)
}

func TestVerify_MalformedHash(t *testing.T) {
	cases := []string{
		"",
		"plain-text",
		"$argon2id$v=19$m=19456,t=2,p=1$onlyfive",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=16$m=19456,t=2,p=1$c2FsdA$a2V5",
	}
	for _, c := range cases {
		assert.Error(t, password.Verify("whatever", c), "畸形哈希必须报错: %q", c)
	}
}
