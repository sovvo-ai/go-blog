package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/response"
)

// 依赖检查状态（TECH_DESIGN 14.3）。
const (
	checkOK      = "ok"
	checkFail    = "fail"
	checkSkipped = "skipped"
)

// DependencyCheck 描述一项依赖的健康检查。
// Required 为 true 时未通过即判定整体未就绪；Ping 为 nil 表示本阶段未启用该依赖。
type DependencyCheck struct {
	Name     string
	Required bool
	Ping     func(ctx context.Context) error
}

// HealthHandler 提供存活与就绪探针。
type HealthHandler struct {
	log     *slog.Logger
	checks  []DependencyCheck
	version string
	started time.Time
}

// NewHealthHandler 构造健康检查处理器。
func NewHealthHandler(log *slog.Logger, checks []DependencyCheck, version string) *HealthHandler {
	return &HealthHandler{
		log:     log,
		checks:  checks,
		version: version,
		started: time.Now(),
	}
}

type checkResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type readyPayload struct {
	Status    string        `json:"status"`
	Checks    []checkResult `json:"checks"`
	UptimeSec int64         `json:"uptime_sec"`
	Version   string        `json:"version"`
}

// Live 是存活探针：进程能响应就返回 200，不查依赖。
// 依赖故障时不应重启容器，所以这里刻意不做任何外部调用。
func (h *HealthHandler) Live(c *gin.Context) {
	response.OK(c, gin.H{
		"status":     "ok",
		"uptime_sec": int64(time.Since(h.started).Seconds()),
	})
}

// Ready 是就绪探针：并发检查全部依赖，任一必需项失败返回 503。
// 响应体列出每项状态与耗时，便于部署时快速定位是哪个依赖没起来。
func (h *HealthHandler) Ready(c *gin.Context) {
	results := make([]checkResult, len(h.checks))
	var wg sync.WaitGroup

	for i, chk := range h.checks {
		if chk.Ping == nil {
			results[i] = checkResult{Name: chk.Name, Status: checkSkipped, Required: chk.Required}
			continue
		}
		wg.Add(1)
		go func(i int, chk DependencyCheck) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()

			start := time.Now()
			err := chk.Ping(ctx)
			res := checkResult{
				Name:      chk.Name,
				Required:  chk.Required,
				Status:    checkOK,
				LatencyMS: time.Since(start).Milliseconds(),
			}
			if err != nil {
				res.Status = checkFail
				res.Error = err.Error()
			}
			results[i] = res
		}(i, chk)
	}
	wg.Wait()

	failed := ""
	for _, r := range results {
		if r.Status == checkFail && r.Required {
			failed = r.Name
			break
		}
	}

	payload := readyPayload{
		Status:    "ok",
		Checks:    results,
		UptimeSec: int64(time.Since(h.started).Seconds()),
		Version:   h.version,
	}
	if failed == "" {
		response.OK(c, payload)
		return
	}

	payload.Status = "unavailable"
	h.log.Error("readiness_failed", slog.String("dependency", failed))
	c.AbortWithStatusJSON(503, response.Envelope{
		Code:      errs.CodeNotReady,
		Message:   "依赖 " + failed + " 不可用",
		Data:      payload,
		RequestID: c.GetString("request_id"),
	})
}
