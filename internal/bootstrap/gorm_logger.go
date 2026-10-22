package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// gormSlogLogger 把 GORM 的 SQL 日志接到统一日志出口。
//
// 不直接用 GORM 默认 logger：它写 stdout、带 ANSI 颜色，
// 与 slog 的 JSON 输出混在一起会污染日志采集。
type gormSlogLogger struct {
	log   *slog.Logger
	level gormlogger.LogLevel
	slow  time.Duration
}

func newGormLogger(log *slog.Logger, production bool) gormlogger.Interface {
	level := gormlogger.Warn
	if production {
		level = gormlogger.Error
	}
	return &gormSlogLogger{
		log:   log.With(slog.String("component", "gorm")),
		level: level,
		slow:  200 * time.Millisecond,
	}
}

func (l *gormSlogLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

func (l *gormSlogLogger) Info(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Info {
		l.log.InfoContext(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l *gormSlogLogger) Warn(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Warn {
		l.log.WarnContext(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l *gormSlogLogger) Error(ctx context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Error {
		l.log.ErrorContext(ctx, fmt.Sprintf(msg, data...))
	}
}

// Trace 只记录错误与慢查询，不逐条打印 SQL。
func (l *gormSlogLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	switch {
	case err != nil && l.level >= gormlogger.Error:
		sql, rows := fc()
		l.log.ErrorContext(ctx, "sql_error",
			slog.String("sql", sql),
			slog.Int64("rows", rows),
			slog.Duration("elapsed", elapsed),
			slog.Any("error", err),
		)
	case elapsed > l.slow && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.log.WarnContext(ctx, "sql_slow",
			slog.String("sql", sql),
			slog.Int64("rows", rows),
			slog.Duration("elapsed", elapsed),
		)
	}
}

// 保证接口实现完整（gorm 后续新增方法时在此处编译报错，而不是运行期）。
var _ gormlogger.Interface = (*gormSlogLogger)(nil)
