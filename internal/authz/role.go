// Package authz 定义角色层级与权限判定（TECH_DESIGN 2.1.1 / 6.5）。
//
// V1.0 采用单一角色字段，不建 roles / permissions 表。
// 层级比较即权限判定：reader < author < admin。
package authz

import (
	"fmt"
	"strings"
)

// Role 是用户角色，数值与 users.role 一致。
type Role uint8

// 角色取值必须与 DATABASE.md 5.2 枚举一致。
const (
	RoleReader Role = 1
	RoleAuthor Role = 2
	RoleAdmin  Role = 3
)

// Level 返回层级数值，用于比较。
func (r Role) Level() int { return int(r) }

// String 返回契约中的角色名。
func (r Role) String() string {
	switch r {
	case RoleReader:
		return "reader"
	case RoleAuthor:
		return "author"
	case RoleAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// Valid 判断是否为已定义角色。
func (r Role) Valid() bool { return r >= RoleReader && r <= RoleAdmin }

// AtLeast 判断角色是否达到最低要求。
// 未知角色（0 或越界）一律不通过——默认拒绝是权限判定的安全基线。
func (r Role) AtLeast(min Role) bool {
	if !r.Valid() || !min.Valid() {
		return false
	}
	return r >= min
}

// ParseRole 把外部字符串转换为角色。
func ParseRole(s string) (Role, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "reader":
		return RoleReader, nil
	case "author":
		return RoleAuthor, nil
	case "admin":
		return RoleAdmin, nil
	default:
		return 0, fmt.Errorf("未知角色: %q", s)
	}
}
