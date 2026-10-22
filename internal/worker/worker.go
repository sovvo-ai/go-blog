// Package worker 承载异步索引任务与周期对账。
//
// 职责边界：worker 只做"把数据库状态搬到索引"，不承载业务规则。
// 所有判断（该发布还是该删除）都由 service.IndexService 从数据库实时读取后决定。
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"go-blog/internal/service"
)

// 任务类型。
const (
	TaskArticleIndex         = "article:index"
	TaskArticleUnindex       = "article:unindex"
	TaskReconcileIncremental = "reconcile:index_incremental"
	TaskReconcileSample      = "reconcile:index_sample"
)

// 队列划分：索引同步影响用户可见性，独立队列避免被慢任务阻塞。
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
)

// ArticlePayload 是索引任务的载荷。
//
// 只放 ID：正文一律由 worker 从数据库重新读取。
// 若把内容塞进队列，积压期间文章被改过，索引就会写成旧版本。
type ArticlePayload struct {
	ArticleID uint64 `json:"article_id"`
}

// Queue 把任务投递到 Redis，实现 service.IndexQueue。
type Queue struct {
	client *asynq.Client
	log    *slog.Logger
}

// NewQueue 构造任务队列客户端。
func NewQueue(rdb *redis.Client, log *slog.Logger) *Queue {
	return &Queue{client: asynq.NewClientFromRedisClient(rdb), log: log}
}

// Close 释放连接。
func (q *Queue) Close() error { return q.client.Close() }

// EnqueueIndex 投递"重建索引"任务。
func (q *Queue) EnqueueIndex(ctx context.Context, articleID uint64) error {
	return q.enqueue(ctx, TaskArticleIndex, ArticlePayload{ArticleID: articleID}, QueueCritical, 5)
}

// EnqueueUnindex 投递"删除索引"任务。
func (q *Queue) EnqueueUnindex(ctx context.Context, articleID uint64) error {
	return q.enqueue(ctx, TaskArticleUnindex, ArticlePayload{ArticleID: articleID}, QueueCritical, 5)
}

func (q *Queue) enqueue(ctx context.Context, taskType string, payload ArticlePayload, queue string, retries int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化任务载荷失败: %w", err)
	}
	_, err = q.client.EnqueueContext(ctx,
		asynq.NewTask(taskType, body),
		asynq.Queue(queue),
		asynq.MaxRetry(retries),
		asynq.Timeout(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("投递任务失败: %w", err)
	}
	return nil
}

// Server 是 asynq 的消费端与定时调度器。
type Server struct {
	srv       *asynq.Server
	scheduler *asynq.Scheduler
	mux       *asynq.ServeMux
	index     *service.IndexService
	log       *slog.Logger
}

// NewServer 构造 worker 服务。
func NewServer(rdb *redis.Client, index *service.IndexService, log *slog.Logger) *Server {
	cfg := asynq.Config{
		// 索引任务优先处理，避免用户发布后长时间搜不到
		Queues: map[string]int{
			QueueCritical: 10,
			QueueDefault:  3,
		},
		Concurrency: 6,
		// 与 TECH_DESIGN 10.1 对齐：1s / 5s / 30s / 2m / 10m
		RetryDelayFunc: retryDelay,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Error("任务执行失败", "type", task.Type(), "payload", string(task.Payload()), "err", err)
		}),
	}

	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskArticleIndex, func(ctx context.Context, t *asynq.Task) error {
		id, err := decodeArticleID(t)
		if err != nil {
			// 载荷损坏重试也不会变好，直接丢弃
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		return index.Sync(ctx, id)
	})
	mux.HandleFunc(TaskArticleUnindex, func(ctx context.Context, t *asynq.Task) error {
		id, err := decodeArticleID(t)
		if err != nil {
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		return index.Unindex(ctx, id)
	})
	mux.HandleFunc(TaskReconcileIncremental, func(ctx context.Context, _ *asynq.Task) error {
		fixed, err := index.ReconcileIncremental(ctx, 200)
		log.Info("增量对账完成", "repaired", fixed, "err", err)
		return err
	})
	mux.HandleFunc(TaskReconcileSample, func(ctx context.Context, _ *asynq.Task) error {
		mismatched, err := index.CheckSample(ctx, 100)
		log.Info("抽样校验完成", "sampled", 100, "mismatched", mismatched, "err", err)
		return err
	})

	scheduler := asynq.NewSchedulerFromRedisClient(rdb, nil)

	return &Server{
		srv:       asynq.NewServerFromRedisClient(rdb, cfg),
		scheduler: scheduler,
		mux:       mux,
		index:     index,
		log:       log,
	}
}

// RegisterPeriodic 注册周期任务。
//
// 注册失败说明 cron 表达式有误，属于编码错误而非运行时故障，直接返回让人发现。
func (s *Server) RegisterPeriodic() error {
	if _, err := s.scheduler.Register("@every 10m", asynq.NewTask(TaskReconcileIncremental, nil), asynq.Queue(QueueDefault)); err != nil {
		return fmt.Errorf("注册增量对账任务失败: %w", err)
	}
	if _, err := s.scheduler.Register("0 3 * * *", asynq.NewTask(TaskReconcileSample, nil), asynq.Queue(QueueDefault)); err != nil {
		return fmt.Errorf("注册抽样校验任务失败: %w", err)
	}
	return nil
}

// Boot 先确保索引存在，再启动调度器与消费者。
func (s *Server) Boot() error {
	if err := s.index.EnsureIndex(context.Background()); err != nil {
		return fmt.Errorf("初始化检索索引失败: %w", err)
	}
	s.log.Info("search_index_ready", "ok", true)
	return s.Start()
}

// Start 启动调度器与消费者。
func (s *Server) Start() error {
	if err := s.scheduler.Start(); err != nil {
		return fmt.Errorf("启动调度器失败: %w", err)
	}
	if err := s.srv.Start(s.mux); err != nil {
		return fmt.Errorf("启动任务消费失败: %w", err)
	}
	return nil
}

// Shutdown 优雅停止。
func (s *Server) Shutdown() {
	s.scheduler.Shutdown()
	s.srv.Shutdown()
}

func decodeArticleID(t *asynq.Task) (uint64, error) {
	var p ArticlePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return 0, fmt.Errorf("解析载荷失败: %w", err)
	}
	if p.ArticleID == 0 {
		return 0, fmt.Errorf("载荷缺少 article_id")
	}
	return p.ArticleID, nil
}

// retryDelay 按固定档位退避，便于运维预估最长重试窗口。
func retryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	delays := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
	}
	if n <= 0 {
		return delays[0]
	}
	if n > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[n-1]
}
