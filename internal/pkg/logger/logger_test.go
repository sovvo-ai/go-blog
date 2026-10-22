package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/pkg/logger"
)

func TestInit_JSONOutputAndLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, logger.Init("json", "info", false, &buf))

	logger.Debug("不应出现")
	logger.Info("出现", "key", "value")

	out := buf.String()
	assert.NotContains(t, out, "不应出现", "低于配置级别的日志必须被过滤")

	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &record))
	assert.Equal(t, "出现", record["msg"])
	assert.Equal(t, "value", record["key"])
}

func TestInit_RejectsInvalidLevel(t *testing.T) {
	err := logger.Init("json", "verbose", false, &bytes.Buffer{})
	require.Error(t, err)
	// 恢复到测试默认，避免污染后续用例
	require.NoError(t, logger.Init("text", "debug", false, &bytes.Buffer{}))
}

func TestWithRequestID_CarriedByContextLogger(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, logger.Init("json", "debug", false, &buf))

	ctx := logger.WithRequestID(context.Background(), "req-123456")
	logger.InfoContext(ctx, "带请求 ID")

	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &record))
	assert.Equal(t, "req-123456", record["request_id"])
	assert.Equal(t, "req-123456", logger.RequestIDFrom(ctx))
}

func TestRequestIDFrom_EmptyContext(t *testing.T) {
	//nolint:staticcheck // 故意传 nil，验证防御性分支
	assert.Empty(t, logger.RequestIDFrom(nil))
	assert.Empty(t, logger.RequestIDFrom(context.Background()))
}

func TestWith_AddsFixedFields(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, logger.Init("json", "debug", false, &buf))

	logger.With("component", "test").Info("fixed")

	assert.Contains(t, buf.String(), `"component":"test"`)
}
