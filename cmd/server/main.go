// Command server 是博客服务的唯一入口。
//
// 启动顺序：加载环境文件 → 加载配置 → 初始化日志 → wire 装配依赖 → 启动 HTTP 服务。
// 任何一步失败都直接退出，不带病启动。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	// 内嵌 IANA 时区库：运行镜像基于 alpine，不装 tzdata，
	// 否则 TZ=Asia/Shanghai 会静默退化为 UTC，导致时间整体偏 8 小时。
	_ "time/tzdata"

	"go-blog/internal/bootstrap"
	"go-blog/internal/config"
	"go-blog/internal/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		// 此时 logger 可能尚未初始化，直接写 stderr。
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		envFile    string
		configFile string
	)
	flag.StringVar(&envFile, "env-file", "", "本地环境文件路径，如 deploy/.env（仅开发使用）")
	flag.StringVar(&configFile, "config", "", "配置文件路径（可选，环境变量优先）")
	flag.Parse()

	if envFile != "" {
		if err := config.LoadDotEnv(envFile); err != nil {
			return err
		}
	}

	cfg, err := config.LoadFile(configFile)
	if err != nil {
		return err
	}

	if err := logger.Init(cfg.Log.Format, cfg.Log.Level, cfg.Log.Source, nil); err != nil {
		return err
	}
	log := logger.L()
	log.Info("config_loaded", "config", cfg.Redacted())

	app, cleanup, err := bootstrap.InitApp(cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.Run(ctx)
}
