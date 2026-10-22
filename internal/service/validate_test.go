package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateUsername(t *testing.T) {
	for _, ok := range []string{"abcd", "user_01", "A1_b2", "zhangsan"} {
		assert.NoError(t, validateUsername(ok), ok)
	}
	for _, bad := range []string{"", "abc", "has space", "中文名字", "user-name", "toolongusernamethatisover20"} {
		assert.Error(t, validateUsername(bad), bad)
	}
}

func TestValidatePassword(t *testing.T) {
	assert.NoError(t, validatePassword("abc12345"))
	assert.NoError(t, validatePassword("A1"+strings.Repeat("x", 30)))

	for _, bad := range []string{
		"",                                   // 空
		"abc1234",                            // 太短
		"abcdefgh",                           // 只有字母
		"12345678",                           // 只有数字
		"abc12345" + strings.Repeat("d", 25), // 超长
	} {
		assert.Error(t, validatePassword(bad), bad)
	}
}

func TestValidateEmail(t *testing.T) {
	for _, ok := range []string{"a@b.com", "zhang.san+tag@example.co.uk"} {
		assert.NoError(t, validateEmail(ok), ok)
	}
	for _, bad := range []string{"", "no-at-sign", "a@b", "a b@c.com", "@b.com"} {
		assert.Error(t, validateEmail(bad), bad)
	}
}

func TestNormalizeEmailAndAccount(t *testing.T) {
	assert.Equal(t, "a@b.com", normalizeEmail("  A@B.com "))
	assert.Equal(t, "zhangsan", normalizeAccount(" ZhangSan "))
}
