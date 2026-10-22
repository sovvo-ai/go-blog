// Package jwtutil 负责访问令牌的签发与校验（TECH_DESIGN 6.2）。
//
// 令牌只装 uid 与少量标准声明，不放角色与状态：
// 角色和状态每次请求实时读取缓存，改了立即生效，不必等令牌过期。
package jwtutil

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 是访问令牌的声明集合。
type Claims struct {
	jwt.RegisteredClaims
	UID string `json:"uid"`
}

// Manager 签发与校验访问令牌。
type Manager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// New 构造 Manager。secret 长度不足时直接报错，避免弱密钥上线。
func New(secret, issuer string, ttl time.Duration) (*Manager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT 密钥至少 32 字符，当前 %d", len(secret))
	}
	if ttl <= 0 {
		return nil, errors.New("访问令牌有效期必须大于 0")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, ttl: ttl}, nil
}

// TTL 返回访问令牌有效期。
func (m *Manager) TTL() time.Duration { return m.ttl }

// Issue 签发访问令牌，返回令牌串与过期时间。
func (m *Manager) Issue(uid uint64, now time.Time) (string, time.Time, error) {
	exp := now.Add(m.ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   strconv.FormatUint(uid, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)), // 容忍轻微时钟漂移
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        strconv.FormatInt(now.UnixNano(), 36),
		},
		UID: strconv.FormatUint(uid, 10),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发访问令牌失败: %w", err)
	}
	return signed, exp, nil
}

// Parse 校验令牌并返回声明。
// 显式限制签名算法为 HS256，防止 alg 混淆攻击（把 alg 改成 none 或 RS256）。
func (m *Manager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("不支持的签名算法: %s", t.Method.Alg())
		}
		return m.secret, nil
	},
		jwt.WithIssuer(m.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, err
	}
	if claims.UID == "" {
		return nil, errors.New("令牌缺少 uid")
	}
	return claims, nil
}

// UID 把声明中的 uid 还原为数值。
func (c *Claims) UIDValue() (uint64, error) {
	return strconv.ParseUint(c.UID, 10, 64)
}
