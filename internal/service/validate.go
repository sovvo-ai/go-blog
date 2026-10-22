package service

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"go-blog/internal/errs"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{4,20}$`)

// validateUsername 校验用户名：4–20 位字母、数字、下划线。
func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return badRequest("用户名需为 4–20 位字母、数字或下划线", "username")
	}
	return nil
}

// validateEmail 校验邮箱格式与长度。
// 使用 mail.ParseAddress 而不是宽松正则：它能挡住绝大多数畸形输入。
func validateEmail(email string) error {
	if len(email) > 128 {
		return badRequest("邮箱长度不能超过 128 字符", "email")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return badRequest("邮箱格式不正确", "email")
	}
	// ParseAddress 接受 a@b 这种无点域名，对面向公众的注册入口过宽，额外收紧
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return badRequest("邮箱格式不正确", "email")
	}
	domain := parts[1]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return badRequest("邮箱格式不正确", "email")
	}
	return nil
}

// validatePassword 校验密码：8–32 位，且必须同时包含字母与数字。
func validatePassword(pwd string) error {
	if len(pwd) < 8 || len(pwd) > 32 {
		return badRequest("密码长度需为 8–32 位", "password")
	}
	var hasLetter, hasDigit bool
	for _, r := range pwd {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return badRequest("密码需同时包含字母与数字", "password")
	}
	return nil
}

// normalizeEmail 统一小写并去空格，避免 Foo@x.com 与 foo@x.com 注册成两个账号。
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeAccount 登录标识归一化。
func normalizeAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

// badRequest 构造带字段级明细的参数错误，handler 会原样放进 data.errors。
func badRequest(msg, field string) error {
	return errs.BadRequest(msg).WithData(map[string]any{
		"errors": []map[string]string{{"field": field, "message": msg}},
	})
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,198}[a-z0-9]$|^[a-z0-9]$`)

// validateSlug 校验 slug 格式，规则与契约一致。
func validateSlug(s string) error {
	if !slugPattern.MatchString(s) {
		return badRequest("slug 只能由小写字母、数字和连字符组成，且不能以连字符开头或结尾", "slug")
	}
	return nil
}

// fieldErrors 构造 data.errors 结构，与 response 层约定一致。
func fieldErrors(field, msg string) map[string]any {
	return map[string]any{"errors": []map[string]string{{"field": field, "message": msg}}}
}

// isErr 是 errors.Is 的短写，避免在多处 import errors。
func isErr(err, target error) bool { return errors.Is(err, target) }
