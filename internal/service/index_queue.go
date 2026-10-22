package service

import "context"

// IndexQueue 把索引同步请求投递到异步队列。
//
// 定义在 service 侧，实现放在 worker 侧：业务只关心"要同步哪篇文章"，
// 不关心队列是 asynq、Redis Stream 还是别的。
type IndexQueue interface {
	EnqueueIndex(ctx context.Context, articleID uint64) error
	EnqueueUnindex(ctx context.Context, articleID uint64) error
}

// NopIndexQueue 是不启用异步索引时的占位实现。
//
// 不在这里静默吞掉问题：投递成功与否由调用方记日志，
// 真正的兜底是 search_sync_status 字段 + 对账任务。
type NopIndexQueue struct{}

// EnqueueIndex 实现 IndexQueue。
func (NopIndexQueue) EnqueueIndex(context.Context, uint64) error { return nil }

// EnqueueUnindex 实现 IndexQueue。
func (NopIndexQueue) EnqueueUnindex(context.Context, uint64) error { return nil }
