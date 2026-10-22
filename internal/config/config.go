// Package config 负责应用配置的加载、默认值与校验。
//
// 优先级：环境变量 > 配置文件 > 内置默认值。
// 环境变量统一前缀 BLOG_，层级以下划线连接：
//
//	app.addr      -> BLOG_APP_ADDR
//	db.dsn        -> BLOG_DB_DSN
//	redis.password -> BLOG_REDIS_PASSWORD
//
// 凭据只从环境变量或未提交的 .env 注入，源码与默认值中不得出现真实密码。
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// EnvPrefix 是所有环境变量的统一前缀。
const EnvPrefix = "BLOG"

// 运行环境取值。
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// Config 是应用配置的根结构。
type Config struct {
	App      App      `mapstructure:"app"`
	Log      Log      `mapstructure:"log"`
	DB       DB       `mapstructure:"db"`
	Redis    Redis    `mapstructure:"redis"`
	Search   Search   `mapstructure:"search"`
	JWT      JWT      `mapstructure:"jwt"`
	Cookie   Cookie   `mapstructure:"cookie"`
	Mail     Mail     `mapstructure:"mail"`
	Storage  Storage  `mapstructure:"storage"`
	Security Security `mapstructure:"security"`
}

// App 是进程级配置。
type App struct {
	Env                string `mapstructure:"env"`
	Addr               string `mapstructure:"addr"`
	Version            string `mapstructure:"version"`
	ShutdownTimeoutSec int    `mapstructure:"shutdown_timeout_sec"`
}

// IsProduction 用于决定是否启用严格校验与安全默认值。
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// Log 是日志配置，对应 TECH_DESIGN 2.2 日志规范。
type Log struct {
	Level  string `mapstructure:"level"`  // debug | info | warn | error
	Format string `mapstructure:"format"` // json（生产） | text（开发）
	Source bool   `mapstructure:"source"` // 是否输出调用位置，生产关闭
}

// DB 是 MySQL 连接配置。
type DB struct {
	DSN                string `mapstructure:"dsn"`
	MaxOpenConns       int    `mapstructure:"max_open_conns"`
	MaxIdleConns       int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeMin int    `mapstructure:"conn_max_lifetime_min"`
}

// Redis 是 Redis 连接配置。地址、密码、库编号都可配，禁止硬编码。
type Redis struct {
	Addr           string `mapstructure:"addr"`
	Password       string `mapstructure:"password"`
	DB             int    `mapstructure:"db"`
	PoolSize       int    `mapstructure:"pool_size"`
	DialTimeoutSec int    `mapstructure:"dial_timeout_sec"`
}

// Search 是 Meilisearch 配置。Host 为空表示未启用，/readyz 会跳过该项。
type Search struct {
	Host    string `mapstructure:"host"`
	APIKey  string `mapstructure:"api_key"`
	Index   string `mapstructure:"index"`
	Timeout int    `mapstructure:"timeout_sec"` // 单次检索超时（秒）
}

// Enabled 判断检索服务是否已配置。
func (s Search) Enabled() bool { return strings.TrimSpace(s.Host) != "" }

// JWT 是访问令牌的签名配置（TECH_DESIGN 6.1）。
type JWT struct {
	Secret       string `mapstructure:"secret"`         // 必填，至少 32 字符
	Issuer       string `mapstructure:"issuer"`         // 签发方标识
	AccessTTLMin int    `mapstructure:"access_ttl_min"` // 访问令牌有效期（分钟）
}

// Cookie 是认证 Cookie 的属性配置。
type Cookie struct {
	Secure   bool   `mapstructure:"secure"`    // 生产必须为 true
	SameSite string `mapstructure:"same_site"` // lax（默认）/ strict
}

// Mail 是邮件发送配置。driver=console 只把链接打到日志，用于本地与测试。
type Mail struct {
	Driver        string `mapstructure:"driver"` // console | smtp
	From          string `mapstructure:"from"`
	SMTPHost      string `mapstructure:"smtp_host"`
	SMTPPort      int    `mapstructure:"smtp_port"`
	SMTPUser      string `mapstructure:"smtp_user"`
	SMTPPass      string `mapstructure:"smtp_pass"`
	VerifyURLBase string `mapstructure:"verify_url_base"` // 验证链接前缀
	ResetURLBase  string `mapstructure:"reset_url_base"`  // 重置链接前缀
}

// Storage 是对象存储配置。
//
// V1.0 只实现 local 驱动；s3 保留配置位但启动即报错，
// 避免"配了 s3 却悄悄写本地盘"这种最难排查的问题。
type Storage struct {
	Driver        string `mapstructure:"driver"`          // local | s3
	LocalRoot     string `mapstructure:"local_root"`      // local 驱动的根目录
	PublicBaseURL string `mapstructure:"public_base_url"` // 对外访问前缀，不含末尾斜杠
	MaxImageMB    int    `mapstructure:"max_image_mb"`    // 单张图片大小上限
}

// Security 是认证相关的限流与锁定参数。
type Security struct {
	LoginFailLimit          int `mapstructure:"login_fail_limit"`           // 连续失败上限
	LoginLockMin            int `mapstructure:"login_lock_min"`             // 锁定时长（分钟）
	VerifyResendIntervalSec int `mapstructure:"verify_resend_interval_sec"` // 验证邮件最小间隔
	VerifyResendDailyLimit  int `mapstructure:"verify_resend_daily_limit"`  // 验证邮件每日上限
	ResetResendIntervalSec  int `mapstructure:"reset_resend_interval_sec"`  // 重置邮件最小间隔
	ResetResendDailyLimit   int `mapstructure:"reset_resend_daily_limit"`   // 重置邮件每日上限
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.env", EnvDevelopment)
	v.SetDefault("app.addr", ":8080")
	v.SetDefault("app.version", "dev")
	v.SetDefault("app.shutdown_timeout_sec", 15)

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("log.source", false)

	v.SetDefault("db.dsn", "")
	v.SetDefault("db.max_open_conns", 25)
	v.SetDefault("db.max_idle_conns", 10)
	v.SetDefault("db.conn_max_lifetime_min", 30)

	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 20)
	v.SetDefault("redis.dial_timeout_sec", 3)

	v.SetDefault("search.host", "")
	v.SetDefault("search.api_key", "")
	v.SetDefault("search.index", "articles")
	v.SetDefault("search.timeout_sec", 3)

	v.SetDefault("jwt.secret", "")
	v.SetDefault("jwt.issuer", "go-blog")
	v.SetDefault("jwt.access_ttl_min", 15)

	v.SetDefault("cookie.secure", false)
	v.SetDefault("cookie.same_site", "lax")

	v.SetDefault("mail.driver", "console")
	v.SetDefault("mail.from", "no-reply@blog.local")
	v.SetDefault("mail.smtp_host", "")
	v.SetDefault("mail.smtp_port", 587)
	v.SetDefault("mail.smtp_user", "")
	v.SetDefault("mail.smtp_pass", "")
	v.SetDefault("mail.verify_url_base", "http://localhost:3000/verify-email")
	v.SetDefault("mail.reset_url_base", "http://localhost:3000/reset-password")

	v.SetDefault("storage.driver", "local")
	v.SetDefault("storage.local_root", "var/storage")
	v.SetDefault("storage.public_base_url", "http://127.0.0.1:8080/media")
	v.SetDefault("storage.max_image_mb", 10)

	v.SetDefault("security.login_fail_limit", 5)
	v.SetDefault("security.login_lock_min", 15)
	v.SetDefault("security.verify_resend_interval_sec", 60)
	v.SetDefault("security.verify_resend_daily_limit", 10)
	v.SetDefault("security.reset_resend_interval_sec", 60)
	v.SetDefault("security.reset_resend_daily_limit", 5)
}

// Load 仅使用环境变量与默认值加载配置。
func Load() (*Config, error) { return LoadFile("") }

// LoadFile 在 Load 的基础上叠加配置文件，环境变量仍然优先。
// file 为空表示不读文件。
func LoadFile(file string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)

	if file != "" {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("读取配置文件 %s 失败: %w", file, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.normalize()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) normalize() {
	c.App.Env = strings.ToLower(strings.TrimSpace(c.App.Env))
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	c.Log.Format = strings.ToLower(strings.TrimSpace(c.Log.Format))
	c.DB.DSN = strings.TrimSpace(c.DB.DSN)
	c.Redis.Addr = strings.TrimSpace(c.Redis.Addr)
	c.JWT.Secret = strings.TrimSpace(c.JWT.Secret)
	c.Mail.Driver = strings.ToLower(strings.TrimSpace(c.Mail.Driver))
	c.Cookie.SameSite = strings.ToLower(strings.TrimSpace(c.Cookie.SameSite))
	c.Search.Host = strings.TrimRight(strings.TrimSpace(c.Search.Host), "/")
	c.Search.Index = strings.TrimSpace(c.Search.Index)
	c.Storage.Driver = strings.ToLower(strings.TrimSpace(c.Storage.Driver))
	c.Storage.LocalRoot = strings.TrimSpace(c.Storage.LocalRoot)
	c.Storage.PublicBaseURL = strings.TrimRight(strings.TrimSpace(c.Storage.PublicBaseURL), "/")

	// 生产环境默认 JSON 日志，便于采集端解析。
	if c.App.IsProduction() && c.Log.Format == "text" {
		c.Log.Format = "json"
	}
	if c.App.IsProduction() && c.Log.Level == "debug" {
		c.Log.Level = "info"
	}
	// 生产环境禁止关闭 Secure，否则凭证会以明文 HTTP 传输
	if c.App.IsProduction() {
		c.Cookie.Secure = true
	}
}

// Validate 校验配置的完整性与安全底线。
// 原则：启动时就失败，不要等到第一个请求进来才 panic。
func (c *Config) Validate() error {
	switch c.App.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		return fmt.Errorf("app.env 取值非法: %q（允许 %s / %s / %s）",
			c.App.Env, EnvDevelopment, EnvTest, EnvProduction)
	}

	if c.App.Addr == "" {
		return errors.New("app.addr 不能为空")
	}
	if c.App.ShutdownTimeoutSec <= 0 {
		return errors.New("app.shutdown_timeout_sec 必须大于 0")
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level 取值非法: %q", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		return fmt.Errorf("log.format 取值非法: %q", c.Log.Format)
	}

	if c.DB.DSN == "" {
		return errors.New("db.dsn 不能为空（环境变量 BLOG_DB_DSN）")
	}
	if c.DB.MaxOpenConns <= 0 {
		return errors.New("db.max_open_conns 必须大于 0")
	}
	if c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		return errors.New("db.max_idle_conns 不能大于 db.max_open_conns")
	}

	if c.Redis.Addr == "" {
		return errors.New("redis.addr 不能为空（环境变量 BLOG_REDIS_ADDR）")
	}
	if c.Redis.DB < 0 {
		return errors.New("redis.db 不能为负数")
	}

	if c.App.IsProduction() && c.Redis.Password == "" {
		return errors.New("生产环境必须为 Redis 设置密码（BLOG_REDIS_PASSWORD）")
	}
	if c.App.IsProduction() && c.Search.Enabled() && c.Search.APIKey == "" {
		return errors.New("生产环境启用检索服务时必须配置 search.api_key")
	}

	if len(c.JWT.Secret) < 32 {
		return fmt.Errorf("jwt.secret 至少 32 字符（当前 %d）", len(c.JWT.Secret))
	}
	if c.JWT.AccessTTLMin <= 0 {
		return errors.New("jwt.access_ttl_min 必须大于 0")
	}

	switch c.Cookie.SameSite {
	case "lax", "strict":
	default:
		return fmt.Errorf("cookie.same_site 取值非法: %q", c.Cookie.SameSite)
	}

	switch c.Mail.Driver {
	case "console":
	case "smtp":
		if c.Mail.SMTPHost == "" {
			return errors.New("mail.driver=smtp 时必须配置 mail.smtp_host")
		}
		if c.Mail.SMTPPort <= 0 {
			return errors.New("mail.smtp_port 必须大于 0")
		}
	default:
		return fmt.Errorf("mail.driver 取值非法: %q（console / smtp）", c.Mail.Driver)
	}
	if c.Mail.VerifyURLBase == "" || c.Mail.ResetURLBase == "" {
		return errors.New("mail.verify_url_base 与 mail.reset_url_base 不能为空")
	}

	switch c.Storage.Driver {
	case "local":
		if c.Storage.LocalRoot == "" {
			return errors.New("storage.driver=local 时必须配置 storage.local_root")
		}
	case "s3":
		// 显式失败：S3/MinIO 驱动尚未实现，不允许静默回退到本地磁盘
		return errors.New("storage.driver=s3 尚未实现（V1.0 仅支持 local），请改用 local 或等待部署阶段补齐")
	default:
		return fmt.Errorf("storage.driver 取值非法: %q（local / s3）", c.Storage.Driver)
	}
	if c.Search.Enabled() && c.Search.Index == "" {
		return errors.New("启用检索服务时必须配置 search.index")
	}
	if c.Search.Enabled() && c.App.IsProduction() && c.Search.APIKey == "" {
		return errors.New("生产环境启用检索服务时必须配置 search.api_key")
	}
	if c.Search.Timeout <= 0 || c.Search.Timeout > 30 {
		return errors.New("search.timeout_sec 需在 1–30 之间")
	}

	if c.Storage.PublicBaseURL == "" {
		return errors.New("storage.public_base_url 不能为空")
	}
	if c.Storage.MaxImageMB <= 0 || c.Storage.MaxImageMB > 50 {
		return errors.New("storage.max_image_mb 需在 1–50 之间")
	}

	if c.Security.LoginFailLimit <= 0 || c.Security.LoginLockMin <= 0 {
		return errors.New("security.login_fail_limit 与 security.login_lock_min 必须大于 0")
	}
	return nil
}

// Redacted 返回可安全写入日志的配置快照，凭据一律打码。
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"env":           c.App.Env,
		"addr":          c.App.Addr,
		"db_dsn":        maskDSN(c.DB.DSN),
		"redis_addr":    c.Redis.Addr,
		"redis_db":      c.Redis.DB,
		"redis_secret":  mask(c.Redis.Password),
		"search_host":   c.Search.Host,
		"log_level":     c.Log.Level,
		"log_format":    c.Log.Format,
		"jwt_secret":    mask(c.JWT.Secret),
		"cookie_secure": c.Cookie.Secure,
		"mail_driver":   c.Mail.Driver,
	}
}

// mask 只保留长度信息，不泄露任何字符。
func mask(s string) string {
	if s == "" {
		return "(empty)"
	}
	return fmt.Sprintf("(set,%d chars)", len(s))
}

// maskDSN 隐去 DSN 中的密码段：user:pass@tcp(...) -> user:***@tcp(...)
func maskDSN(dsn string) string {
	at := strings.LastIndex(dsn, "@")
	if at < 0 {
		return dsn
	}
	head := dsn[:at]
	colon := strings.Index(head, ":")
	if colon < 0 {
		return dsn
	}
	return head[:colon] + ":***" + dsn[at:]
}
