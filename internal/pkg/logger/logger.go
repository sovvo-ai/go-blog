// Package logger 是全项目唯一的日志入口（TECH_DESIGN 2.2）。
//
// 规则：
//   - 业务代码只调用 Debug / Info / Warn / Error 与 With
//   - 禁止在业务包内自建 slog.Logger、log.Printf、fmt.Println
//   - 请求级字段通过 *Context 变体自动携带（request_id）
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// contextKey 避免与其他包的 context key 冲突。
type contextKey struct{ name string }

var requestIDKey = contextKey{name: "request_id"}

var (
	base   = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	level  = new(slog.LevelVar)
	source bool
)

// Init 初始化全局 logger。只允许在 cmd/server/main.go 调用一次。
// format: json | text；level: debug | info | warn | error。
func Init(cfgFormat, cfgLevel string, cfgSource bool, w io.Writer) error {
	lv, err := parseLevel(cfgLevel)
	if err != nil {
		return err
	}
	level.Set(lv)
	source = cfgSource

	if w == nil {
		// 生产写 stdout 交给容器日志采集，开发写 stderr 不干扰业务输出。
		if cfgFormat == "json" {
			w = os.Stdout
		} else {
			w = os.Stderr
		}
	}

	opts := &slog.HandlerOptions{Level: level, AddSource: source}
	var h slog.Handler
	if cfgFormat == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	base = slog.New(h)
	slog.SetDefault(base)
	return nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("日志级别非法: %q", s)
	}
}

// L 返回全局 logger，仅在装配处使用。
func L() *slog.Logger { return base }

// With 返回附带固定字段的 logger。
func With(args ...any) *slog.Logger { return base.With(args...) }

// WithRequestID 把请求 ID 写入 context，供下游 *Context 方法读取。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom 从 context 提取请求 ID，不存在时返回空串。
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// FromContext 返回携带 request_id 的 logger。
func FromContext(ctx context.Context) *slog.Logger {
	if id := RequestIDFrom(ctx); id != "" {
		return base.With(slog.String("request_id", id))
	}
	return base
}

// Debug 输出调试日志。
func Debug(msg string, args ...any) { base.Debug(msg, args...) }

// Info 输出提示日志。
func Info(msg string, args ...any) { base.Info(msg, args...) }

// Warn 输出警告日志。
func Warn(msg string, args ...any) { base.Warn(msg, args...) }

// Error 输出错误日志。
func Error(msg string, args ...any) { base.Error(msg, args...) }

// DebugContext 输出带 request_id 的调试日志。
func DebugContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).Debug(msg, args...)
}

// InfoContext 输出带 request_id 的提示日志。
func InfoContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).Info(msg, args...)
}

// WarnContext 输出带 request_id 的警告日志。
func WarnContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).Warn(msg, args...)
}

// ErrorContext 输出带 request_id 的错误日志。
func ErrorContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).Error(msg, args...)
}
