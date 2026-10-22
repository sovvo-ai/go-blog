// Package password 提供 Argon2id 密码哈希与校验。
//
// 参数取 OWASP 推荐组合（m=19456 KiB、t=2、p=1）：
// 单次约 20–40ms，足以抵抗离线爆破，又不会把登录接口拖成瓶颈。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// 哈希参数。修改任何一项都会导致旧哈希无法校验，必须同步迁移。
const (
	memoryKiB  = 19456
	iterations = 2
	parallel   = 1
	saltLen    = 16
	keyLen     = 32
)

// ErrMismatch 表示密码与哈希不匹配。
var ErrMismatch = errors.New("密码不匹配")

// Hash 生成编码后的哈希串，格式：
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64(salt)>$<base64(key)>
func Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("密码不能为空")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallel, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, iterations, parallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify 校验明文密码与编码哈希是否匹配。
// 内部使用常量时间比较，避免通过响应时间推测哈希字节。
func Verify(plain, encoded string) error {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return err
	}

	got := argon2.IDKey([]byte(plain), salt, params.iterations, params.memory, params.parallel, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) == 1 {
		return nil
	}
	return ErrMismatch
}

type params struct {
	memory     uint32
	iterations uint32
	parallel   uint8
}

func decode(encoded string) (params, []byte, []byte, error) {
	var p params
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("密码哈希格式非法")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, fmt.Errorf("解析哈希版本失败: %w", err)
	}
	if version != argon2.Version {
		return p, nil, nil, fmt.Errorf("不支持的 argon2 版本: %d", version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallel); err != nil {
		return p, nil, nil, fmt.Errorf("解析哈希参数失败: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, fmt.Errorf("解析盐失败: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, fmt.Errorf("解析摘要失败: %w", err)
	}
	return p, salt, key, nil
}
