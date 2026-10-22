package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setBaseEnv 设置最小可用的必需配置。
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BLOG_DB_DSN", "blog:pass@tcp(127.0.0.1:3306)/blog?charset=utf8mb4&parseTime=true")
	t.Setenv("BLOG_REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("BLOG_JWT_SECRET", "test_jwt_secret_at_least_32_characters")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, EnvDevelopment, cfg.App.Env)
	assert.Equal(t, ":8080", cfg.App.Addr)
	assert.Equal(t, "dev", cfg.App.Version)
	assert.Equal(t, 15, cfg.App.ShutdownTimeoutSec)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format)
	assert.Equal(t, 25, cfg.DB.MaxOpenConns)
	assert.Equal(t, 0, cfg.Redis.DB)
	assert.False(t, cfg.Search.Enabled())
}

func TestLoad_EnvOverridesDefaults(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("BLOG_APP_ADDR", ":9090")
	t.Setenv("BLOG_REDIS_PASSWORD", "s3cret")
	t.Setenv("BLOG_REDIS_DB", "2")
	t.Setenv("BLOG_LOG_LEVEL", "DEBUG")
	t.Setenv("BLOG_SEARCH_HOST", "http://127.0.0.1:7700")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.App.Addr)
	assert.Equal(t, "s3cret", cfg.Redis.Password, "Redis 密码必须可配置")
	assert.Equal(t, 2, cfg.Redis.DB, "Redis 库编号必须可配置")
	assert.Equal(t, "debug", cfg.Log.Level, "日志级别应归一化为小写")
	assert.True(t, cfg.Search.Enabled())
}

func TestValidate_RejectsIncompleteConfig(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "缺少 DSN",
			env:  map[string]string{"BLOG_REDIS_ADDR": "127.0.0.1:6380", "BLOG_DB_DSN": ""},
			want: "db.dsn",
		},
		{
			name: "缺少 Redis 地址",
			env:  map[string]string{"BLOG_DB_DSN": "u:p@tcp(x)/d", "BLOG_REDIS_ADDR": ""},
			want: "redis.addr",
		},
		{
			name: "非法运行环境",
			env: map[string]string{
				"BLOG_APP_ENV": "staging", "BLOG_DB_DSN": "u:p@tcp(x)/d", "BLOG_REDIS_ADDR": "r:6379",
			},
			want: "app.env",
		},
		{
			name: "非法日志级别",
			env: map[string]string{
				"BLOG_LOG_LEVEL": "trace", "BLOG_DB_DSN": "u:p@tcp(x)/d", "BLOG_REDIS_ADDR": "r:6379",
			},
			want: "log.level",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidate_ProductionRequiresRedisPassword(t *testing.T) {
	t.Setenv("BLOG_APP_ENV", "production")
	t.Setenv("BLOG_DB_DSN", "u:p@tcp(x)/d")
	t.Setenv("BLOG_REDIS_ADDR", "blog-redis:6379")
	t.Setenv("BLOG_REDIS_PASSWORD", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Redis 设置密码")
}

func TestLoad_ProductionForcesJSONLog(t *testing.T) {
	t.Setenv("BLOG_APP_ENV", "production")
	t.Setenv("BLOG_DB_DSN", "u:p@tcp(x)/d")
	t.Setenv("BLOG_REDIS_ADDR", "blog-redis:6379")
	t.Setenv("BLOG_REDIS_PASSWORD", "s3cret")
	t.Setenv("BLOG_JWT_SECRET", "test_jwt_secret_at_least_32_characters")
	t.Setenv("BLOG_LOG_FORMAT", "text")
	t.Setenv("BLOG_LOG_LEVEL", "debug")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "json", cfg.Log.Format, "生产环境强制 JSON 日志")
	assert.Equal(t, "info", cfg.Log.Level, "生产环境不允许 debug 级别")
}

func TestRedacted_HidesCredentials(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("BLOG_REDIS_PASSWORD", "super-secret-value")
	t.Setenv("BLOG_DB_DSN", "blog:db-password@tcp(127.0.0.1:3306)/blog")

	cfg, err := Load()
	require.NoError(t, err)

	flat := cfg.Redacted()
	assert.NotContains(t, flat["redis_secret"], "super-secret-value")
	assert.NotContains(t, flat["db_dsn"], "db-password")
	assert.Contains(t, flat["db_dsn"], "blog:***@tcp")
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# 注释行\n\nBLOG_TEST_A=plain\nexport BLOG_TEST_B=\"quoted value\"\nBLOG_TEST_C='single'\nBLOG_TEST_D=has=equals\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// 已存在的环境变量优先，不被文件覆盖
	t.Setenv("BLOG_TEST_A", "from-process")

	require.NoError(t, LoadDotEnv(path))

	assert.Equal(t, "from-process", os.Getenv("BLOG_TEST_A"))
	assert.Equal(t, "quoted value", os.Getenv("BLOG_TEST_B"))
	assert.Equal(t, "single", os.Getenv("BLOG_TEST_C"))
	assert.Equal(t, "has=equals", os.Getenv("BLOG_TEST_D"))
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	err := LoadDotEnv(filepath.Join(t.TempDir(), "not-exists.env"))
	require.Error(t, err)
}
