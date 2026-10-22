// Package search 定义检索抽象与 Meilisearch 实现。
//
// 业务层只依赖 Searcher 接口：未配置检索服务时注入 Unavailable，
// 所有写入静默成功、查询返回 ErrUnavailable，由上层降级为"空结果 + degraded"。
package search

import (
	"context"
	"errors"
)

// ErrUnavailable 表示检索服务不可用（未配置或连接失败）。
var ErrUnavailable = errors.New("检索服务不可用")

// Doc 是写入索引的文档结构（TECH_DESIGN 9.1）。
//
// 刻意不含 view_count：浏览量走 Redis 缓冲，与索引快照必然不同步，
// 放进来会导致"详情页 3200 阅读、搜索却按 2800 排序"的可见矛盾。
type Doc struct {
	ID           uint64   `json:"id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Content      string   `json:"content"`
	AuthorID     uint64   `json:"author_id"`
	AuthorName   string   `json:"author_name"`
	CategoryID   uint64   `json:"category_id"`
	CategoryName string   `json:"category_name"`
	TagIDs       []uint64 `json:"tag_ids"`
	TagNames     []string `json:"tag_names"`
	PublishedAt  int64    `json:"published_at"`
}

// Hit 是检索命中的最小信息。
//
// slug / summary 等字段不在索引里，由调用方回表补齐——
// 索引只负责"哪些文档相关"，事实仍以数据库为准（TECH_DESIGN 9.4）。
type Hit struct {
	ID        uint64
	Highlight string
}

// Result 是一次检索的原始结果。
type Result struct {
	Hits []Hit
	// EstimatedTotal 是 Meilisearch 的估算值，不得用于计算总页数。
	EstimatedTotal int
}

// Searcher 是检索能力的唯一抽象。
type Searcher interface {
	// EnsureIndex 创建索引并下发设置，可重复调用。
	EnsureIndex(ctx context.Context) error
	// Upsert 覆盖写入文档，天然幂等。
	Upsert(ctx context.Context, docs []Doc) error
	// Delete 删除文档；文档不存在视为成功。
	Delete(ctx context.Context, ids []uint64) error
	// Query 执行检索。
	Query(ctx context.Context, q string, offset, limit int) (*Result, error)
	// Fetch 按 ID 取回索引中的文档，用于抽样对账。
	Fetch(ctx context.Context, ids []uint64) (map[uint64]Doc, error)
	// Healthy 探测检索服务可用性。
	Healthy(ctx context.Context) error
}

// Unavailable 是未启用检索服务时的占位实现。
//
// 写入静默成功是刻意的：文章发布不应因为"没配搜索"而失败。
// 查询明确返回 ErrUnavailable，避免把"没配搜索"伪装成"没有结果"。
type Unavailable struct{}

// EnsureIndex 实现 Searcher。
func (Unavailable) EnsureIndex(context.Context) error { return nil }

// Upsert 实现 Searcher。
func (Unavailable) Upsert(context.Context, []Doc) error { return nil }

// Delete 实现 Searcher。
func (Unavailable) Delete(context.Context, []uint64) error { return nil }

// Query 实现 Searcher。
func (Unavailable) Query(context.Context, string, int, int) (*Result, error) {
	return nil, ErrUnavailable
}

// Fetch 实现 Searcher。
func (Unavailable) Fetch(context.Context, []uint64) (map[uint64]Doc, error) {
	return nil, ErrUnavailable
}

// Healthy 实现 Searcher。
func (Unavailable) Healthy(context.Context) error { return ErrUnavailable }
