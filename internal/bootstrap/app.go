package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/config"
	"go-blog/internal/worker"
)

// App 持有进程级资源与 HTTP 服务。
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	engine *gin.Engine
	server *http.Server
	// worker 为 nil 表示未启用检索服务，不启动后台消费。
	worker *worker.Server
}

// NewApp 构造应用。依赖已由装配层准备好，这里只负责生命周期。
func NewApp(cfg *config.Config, log *slog.Logger, engine *gin.Engine, w *worker.Server) *App {
	return &App{
		cfg:    cfg,
		log:    log,
		engine: engine,
		worker: w,
		server: &http.Server{
			Addr:    cfg.App.Addr,
			Handler: engine,
			// 防慢速头攻击
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
			// 刻意不设 WriteTimeout：阶段 6 的 WebSocket 与 SSE 都是长连接，
			// 一旦设置会在长连接上强制断开。改用业务层超时控制。
			WriteTimeout: 0,
		},
	}
}

// Run 启动 HTTP 服务并阻塞，直到收到退出信号或监听失败。
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	// 后台任务先起：HTTP 一旦对外可用，内容变更就会投递索引任务，
	// 消费端没起来会让任务在队列里堆积。
	if a.worker != nil {
		if err := a.worker.Boot(); err != nil {
			return err
		}
		a.log.Info("worker_started")
	}

	go func() {
		a.log.Info("server_starting",
			slog.String("addr", a.cfg.App.Addr),
			slog.String("env", a.cfg.App.Env),
			slog.String("version", a.cfg.App.Version),
		)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		timeout := time.Duration(a.cfg.App.ShutdownTimeoutSec) * time.Second
		a.log.Info("server_shutting_down", slog.Duration("timeout", timeout))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// 先停 HTTP 再停 worker：避免停 worker 后仍有请求投递新任务
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.log.Error("server_shutdown_failed", slog.Any("error", err))
			return err
		}
		if a.worker != nil {
			a.worker.Shutdown()
			a.log.Info("worker_stopped")
		}
		a.log.Info("server_stopped")
		return nil
	}
}
