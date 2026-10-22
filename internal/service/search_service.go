package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go-blog/internal/errs"
	"go-blog/internal/repository"
	"go-blog/internal/search"
)

// SearchBriefStore 提供搜索结果回表所需的数据。
type SearchBriefStore interface {
	PublishedBriefs(ctx context.Context, ids []uint64) (map[uint64]repository.PublishedBrief, error)
}

// SearchHit 是一条搜索结果。
type SearchHit struct {
	ID          uint64
	Slug        string
	Title       string
	Highlight   string
	Summary     string
	PublishedAt *time.Time
}

// SearchResult 是一次搜索的完整结果。
type SearchResult struct {
	Items          []SearchHit
	Page           int
	PageSize       int
	EstimatedTotal int
	HasMore        bool
	// Degraded 表示检索服务不可用，结果为空但请求成功。
	Degraded bool
}

// 搜索分页上限，与 Meilisearch 的 maxTotalHits 默认值一致（TECH_DESIGN 9.6）。
const maxSearchOffset = 1000

// SearchService 负责检索与结果校验。
type SearchService struct {
	store    SearchBriefStore
	searcher search.Searcher
	log      *slog.Logger
}

// NewSearchService 构造 SearchService。
func NewSearchService(store SearchBriefStore, searcher search.Searcher, log *slog.Logger) *SearchService {
	return &SearchService{store: store, searcher: searcher, log: log}
}

// Search 执行检索。
//
// 检索服务不可用时返回空结果 + Degraded，不返回 5xx：
// 搜索挂了不该让整个站点报错，用户仍能浏览列表页。
func (s *SearchService) Search(ctx context.Context, q string, page, pageSize int) (*SearchResult, error) {
	q = strings.TrimSpace(q)
	if n := len([]rune(q)); n == 0 || n > 100 {
		return nil, badRequest("关键词长度需在 1–100 字符之间", "q")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}

	offset := (page - 1) * pageSize
	if offset+pageSize > maxSearchOffset {
		return nil, badRequest("搜索结果最多可翻到 1000 条，请使用更精确的关键词", "page")
	}

	res, err := s.searcher.Query(ctx, q, offset, pageSize)
	if err != nil {
		s.log.Warn("检索服务不可用，降级为空结果", "q", q, "err", err)
		return &SearchResult{
			Items:    []SearchHit{},
			Page:     page,
			PageSize: pageSize,
			Degraded: true,
		}, nil
	}

	ids := make([]uint64, 0, len(res.Hits))
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	briefs, err := s.store.PublishedBriefs(ctx, ids)
	if err != nil {
		return nil, errs.Internal(err)
	}

	// 按索引给出的顺序拼装，同时过滤掉已删除/已下架的文章。
	// 索引是异步的，只有这一步能确定性地保证"搜索结果都能打开"（TECH_DESIGN 9.4）。
	items := make([]SearchHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		brief, ok := briefs[h.ID]
		if !ok {
			continue
		}
		highlight := h.Highlight
		if highlight == "" {
			highlight = brief.Title
		}
		// 与文章详情接口一致：契约要求 UTC
		var publishedAt *time.Time
		if brief.PublishedAt != nil {
			t := brief.PublishedAt.UTC()
			publishedAt = &t
		}
		items = append(items, SearchHit{
			ID:          brief.ID,
			Slug:        brief.Slug,
			Title:       brief.Title,
			Highlight:   highlight,
			Summary:     brief.Summary,
			PublishedAt: publishedAt,
		})
	}

	// 过滤后这一页可能不满：不补齐、不重试，offset 严格递增（API_SPEC 4.4）。

	// has_more 必须用"过滤前的原始命中数" len(res.Hits)，而不是过滤后的 len(items)。
	// 原因：estimated_total 也是索引口径（未过滤），两者必须同源才能比较。
	// 若用 len(items)，当本页被回表过滤掉几条时，会提前把 has_more 判成 false，
	// 反而漏掉 offset 之后仍然存在的可公开文章——这是比"下一页可能为空"更糟的错误。
	// 前端"加载更多"应同时判断 has_more 与 items 非空（见 FRONTEND_DESIGN 5.x）。
	limit := res.EstimatedTotal
	if limit > maxSearchOffset {
		limit = maxSearchOffset
	}
	return &SearchResult{
		Items:          items,
		Page:           page,
		PageSize:       pageSize,
		EstimatedTotal: res.EstimatedTotal,
		HasMore:        offset+len(res.Hits) < limit,
	}, nil
}

// ErrSearchDisabled 用于上层的可用性判断。
var ErrSearchDisabled = errors.New("检索服务未启用")
