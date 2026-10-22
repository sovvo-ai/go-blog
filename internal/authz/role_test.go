package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/authz"
)

func TestAtLeast(t *testing.T) {
	cases := []struct {
		role authz.Role
		min  authz.Role
		want bool
	}{
		{authz.RoleReader, authz.RoleReader, true},
		{authz.RoleReader, authz.RoleAuthor, false},
		{authz.RoleAuthor, authz.RoleReader, true},
		{authz.RoleAuthor, authz.RoleAuthor, true},
		{authz.RoleAuthor, authz.RoleAdmin, false},
		{authz.RoleAdmin, authz.RoleAuthor, true},
		{authz.RoleAdmin, authz.RoleAdmin, true},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.role.AtLeast(c.min), "%s >= %s", c.role, c.min)
	}
}

func TestAtLeast_UnknownRoleDenied(t *testing.T) {
	var zero authz.Role
	assert.False(t, zero.AtLeast(authz.RoleReader), "未设置角色必须被拒绝")
	assert.False(t, authz.Role(9).AtLeast(authz.RoleReader), "越界角色必须被拒绝")
	assert.False(t, authz.RoleAdmin.AtLeast(zero), "非法最低要求也必须拒绝")
}

func TestParseRole(t *testing.T) {
	for name, want := range map[string]authz.Role{
		"reader":  authz.RoleReader,
		"AUTHOR":  authz.RoleAuthor,
		" admin ": authz.RoleAdmin,
	} {
		got, err := authz.ParseRole(name)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	_, err := authz.ParseRole("root")
	assert.Error(t, err)
}

func TestRole_String(t *testing.T) {
	assert.Equal(t, "reader", authz.RoleReader.String())
	assert.Equal(t, "author", authz.RoleAuthor.String())
	assert.Equal(t, "admin", authz.RoleAdmin.String())
	assert.Equal(t, "unknown", authz.Role(0).String())
}

// 角色数值必须与 DATABASE.md 5.2 的枚举一致，否则鉴权会整体错位。
func TestRole_MatchesDatabaseEnum(t *testing.T) {
	assert.Equal(t, 1, int(authz.RoleReader))
	assert.Equal(t, 2, int(authz.RoleAuthor))
	assert.Equal(t, 3, int(authz.RoleAdmin))
}
