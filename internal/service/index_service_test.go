package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/model"
	"go-blog/internal/repository"
	"go-blog/internal/search"
)

var errRepoNotFound = errors.New("记录不存在")

func assertError(msg string) error { return errors.New(msg) }

// ---- 测试替身 ----

type fakeIndexStore struct {
	docs      map[uint64]*search.Doc
	exists    map[uint64]bool
	notFound  map[uint64]bool
	synced    map[uint64]time.Time
	failed    map[uint64]bool
	pending   []uint64
	sample    []model.Article
	published []uint64
}

func newFakeIndexStore() *fakeIndexStore {
	return &fakeIndexStore{
		docs:     map[uint64]*search.Doc{},
		exists:   map[uint64]bool{},
		notFound: map[uint64]bool{},
		synced:   map[uint64]time.Time{},
		failed:   map[uint64]bool{},
	}
}

func (f *fakeIndexStore) IndexDoc(_ context.Context, id uint64) (*search.Doc, bool, error) {
	if f.notFound[id] {
		return nil, false, repository.ErrNotFound
	}
	// 文档存在但不可公开时，IndexDoc 应返回 (nil, false, nil)；
	// ok 永远以"是否可公开"为准，而不是"键是否存在"。
	doc, _ := f.docs[id]
	return doc, doc != nil, nil
}

func (f *fakeIndexStore) MarkSearchSynced(_ context.Context, id uint64, at time.Time) error {
	f.synced[id] = at
	delete(f.failed, id)
	return nil
}

func (f *fakeIndexStore) MarkSearchFailed(_ context.Context, id uint64) error {
	f.failed[id] = true
	delete(f.synced, id)
	return nil
}

func (f *fakeIndexStore) PendingSearchIDs(_ context.Context, limit int) ([]uint64, error) {
	if len(f.pending) > limit {
		return f.pending[:limit], nil
	}
	return f.pending, nil
}

func (f *fakeIndexStore) SamplePublishedForCheck(_ context.Context, limit int) ([]model.Article, error) {
	if len(f.sample) > limit {
		return f.sample[:limit], nil
	}
	return f.sample, nil
}

func (f *fakeIndexStore) PublishedIDsPage(_ context.Context, afterID uint64, limit int) ([]uint64, error) {
	out := make([]uint64, 0, limit)
	for _, id := range f.published {
		if id > afterID && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

// fakeSearcher 记录调用并可注入故障。
type fakeSearcher struct {
	upserted []uint64
	deleted  []uint64
	docs     map[uint64]search.Doc

	upsertErr error
	deleteErr error
	queryErr  error
}

func (f *fakeSearcher) EnsureIndex(context.Context) error { return nil }

func (f *fakeSearcher) Upsert(_ context.Context, docs []search.Doc) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	for _, d := range docs {
		f.upserted = append(f.upserted, d.ID)
		if f.docs == nil {
			f.docs = map[uint64]search.Doc{}
		}
		f.docs[d.ID] = d
	}
	return nil
}

func (f *fakeSearcher) Delete(_ context.Context, ids []uint64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, ids...)
	for _, id := range ids {
		delete(f.docs, id)
	}
	return nil
}

func (f *fakeSearcher) Query(context.Context, string, int, int) (*search.Result, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return &search.Result{Hits: []search.Hit{{ID: 1, Highlight: "<em>x</em>"}}, EstimatedTotal: 1}, nil
}

func (f *fakeSearcher) Fetch(_ context.Context, ids []uint64) (map[uint64]search.Doc, error) {
	out := map[uint64]search.Doc{}
	for _, id := range ids {
		if d, ok := f.docs[id]; ok {
			out[id] = d
		}
	}
	return out, nil
}

func (f *fakeSearcher) Healthy(context.Context) error { return nil }

// ---- 用例 ----

func TestSync_PublishedArticle_Upserts(t *testing.T) {
	store := newFakeIndexStore()
	store.docs[7] = &search.Doc{ID: 7, Title: "并发"}
	fs := &fakeSearcher{}
	svc := NewIndexService(store, fs, slog.Default())

	require.NoError(t, svc.Sync(context.Background(), 7))
	assert.Contains(t, fs.upserted, uint64(7))
	assert.NotContains(t, fs.deleted, uint64(7))
	assert.Contains(t, store.synced, uint64(7))
}

func TestSync_DraftArticle_DeletesFromIndex(t *testing.T) {
	store := newFakeIndexStore()
	store.docs[7] = nil // 草稿：不可公开
	fs := &fakeSearcher{}
	svc := NewIndexService(store, fs, slog.Default())

	require.NoError(t, svc.Sync(context.Background(), 7))
	assert.Contains(t, fs.deleted, uint64(7))
	assert.Empty(t, fs.upserted)
	assert.Contains(t, store.synced, uint64(7))
}

func TestSync_ArticleGone_DeletesQuietly(t *testing.T) {
	store := newFakeIndexStore()
	store.notFound[9] = true
	fs := &fakeSearcher{}
	svc := NewIndexService(store, fs, slog.Default())

	require.NoError(t, svc.Sync(context.Background(), 9))
	assert.Contains(t, fs.deleted, uint64(9))
}

func TestSync_SearcherError_MarksFailedAndPropagates(t *testing.T) {
	store := newFakeIndexStore()
	store.docs[7] = &search.Doc{ID: 7, Title: "并发"}
	fs := &fakeSearcher{upsertErr: assertError("boom")}
	svc := NewIndexService(store, fs, slog.Default())

	err := svc.Sync(context.Background(), 7)
	require.Error(t, err, "错误必须上抛，asynq 才会重试")
	assert.Contains(t, store.failed, uint64(7))
	assert.NotContains(t, store.synced, uint64(7))
}

func TestReconcile_FixesPendingArticles(t *testing.T) {
	store := newFakeIndexStore()
	store.docs[1] = &search.Doc{ID: 1, Title: "a"}
	store.docs[2] = &search.Doc{ID: 2, Title: "b"}
	store.pending = []uint64{1, 2}
	fs := &fakeSearcher{}
	svc := NewIndexService(store, fs, slog.Default())

	fixed, err := svc.ReconcileIncremental(context.Background(), 200)
	require.NoError(t, err)
	assert.Equal(t, 2, fixed)
	assert.Contains(t, fs.upserted, uint64(1))
	assert.Contains(t, fs.upserted, uint64(2))
}

func TestReconcile_SingleFailureDoesNotAbort(t *testing.T) {
	store := newFakeIndexStore()
	store.docs[1] = &search.Doc{ID: 1, Title: "a"}
	store.docs[2] = &search.Doc{ID: 2, Title: "b"}
	store.pending = []uint64{1, 2}
	// 第一条命中已删除文章 → Sync 内部走 Delete 分支成功；
	// 用 upsertErr 只让可公开的文章失败
	fs := &fakeSearcher{upsertErr: assertError("boom")}
	svc := NewIndexService(store, fs, slog.Default())

	_, err := svc.ReconcileIncremental(context.Background(), 200)
	assert.NoError(t, err, "单条失败不应中断整轮对账")
}

func TestCheckSample_DetectsSilentDrift(t *testing.T) {
	store := newFakeIndexStore()
	store.sample = []model.Article{
		{ID: 1, Title: "旧标题"},
		{ID: 2, Title: "正常"},
	}
	fs := &fakeSearcher{docs: map[uint64]search.Doc{
		2: {ID: 2, Title: "正常"},
		// 文章 1 在索引里缺失
	}}
	svc := NewIndexService(store, fs, slog.Default())

	mismatched, err := svc.CheckSample(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, mismatched)
	assert.Contains(t, store.failed, uint64(1), "缺失文档应被重置为待同步")
	assert.NotContains(t, store.failed, uint64(2))
}

func TestRebuildAll_BatchesAndMarksSynced(t *testing.T) {
	store := newFakeIndexStore()
	store.published = []uint64{1, 2, 3}
	for id, title := range map[uint64]string{1: "a", 2: "b", 3: "c"} {
		store.docs[id] = &search.Doc{ID: id, Title: title}
	}
	fs := &fakeSearcher{}
	svc := NewIndexService(store, fs, slog.Default())

	total, err := svc.RebuildAll(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, fs.upserted, 3)
	assert.Len(t, store.synced, 3)
}

// ---- 搜索服务用例 ----

type fakeBriefStore struct {
	briefs map[uint64]repository.PublishedBrief
}

func (f *fakeBriefStore) PublishedBriefs(_ context.Context, ids []uint64) (map[uint64]repository.PublishedBrief, error) {
	out := map[uint64]repository.PublishedBrief{}
	for _, id := range ids {
		if b, ok := f.briefs[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}

func TestSearch_FiltersAndPreservesOrder(t *testing.T) {
	now := time.Now()
	fs := &fakeSearcher{}
	fs.docs = map[uint64]search.Doc{1: {}, 3: {}}
	// 索引顺序：3, 1（相关度排序）
	fs.queryErr = nil
	searcher := &orderedSearcher{inner: fs, order: []uint64{3, 1}, estimated: 10}
	store := &fakeBriefStore{briefs: map[uint64]repository.PublishedBrief{
		1: {ID: 1, Slug: "one", Title: "一篇", Summary: "s1", PublishedAt: &now},
		3: {ID: 3, Slug: "three", Title: "三篇", Summary: "s3", PublishedAt: &now},
	}}
	svc := NewSearchService(store, searcher, slog.Default())

	res, err := svc.Search(context.Background(), "并发", 1, 20)
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	assert.Equal(t, uint64(3), res.Items[0].ID, "顺序必须与索引一致")
	assert.Equal(t, uint64(1), res.Items[1].ID)
	assert.False(t, res.Degraded)
	assert.True(t, res.HasMore, "offset+hits(2) < min(estimated,1000) 时应提示可翻页")
}

func TestSearch_ExcludesUnpublished(t *testing.T) {
	now := time.Now()
	store := &fakeBriefStore{briefs: map[uint64]repository.PublishedBrief{
		1: {ID: 1, Slug: "one", Title: "公开", PublishedAt: &now},
		// 2 已下架：回表查不到
	}}
	searcher := &orderedSearcher{order: []uint64{1, 2}, estimated: 2}
	svc := NewSearchService(store, searcher, slog.Default())

	res, err := svc.Search(context.Background(), "x", 1, 20)
	require.NoError(t, err)
	require.Len(t, res.Items, 1, "已下架文章必须被过滤")
	assert.Equal(t, uint64(1), res.Items[0].ID)
}

func TestSearch_DegradedOnUnavailable(t *testing.T) {
	svc := NewSearchService(&fakeBriefStore{}, search.Unavailable{}, slog.Default())

	res, err := svc.Search(context.Background(), "x", 1, 20)
	require.NoError(t, err, "检索不可用不报错，降级返回")
	assert.True(t, res.Degraded)
	assert.Empty(t, res.Items)
}

func TestSearch_RejectsOutOfRangePage(t *testing.T) {
	svc := NewSearchService(&fakeBriefStore{}, &orderedSearcher{}, slog.Default())

	// 20 × 50 = 1000 恰好在上限内；21 × 50 = 1050 超限
	_, err := svc.Search(context.Background(), "x", 21, 50)
	require.Error(t, err, "offset+limit 超过 1000 必须拒绝")
}

func TestSearch_RejectsEmptyQuery(t *testing.T) {
	svc := NewSearchService(&fakeBriefStore{}, &orderedSearcher{}, slog.Default())

	_, err := svc.Search(context.Background(), "   ", 1, 20)
	require.Error(t, err)
	_, err = svc.Search(context.Background(), string(make([]rune, 101)), 1, 20)
	require.Error(t, err)
}

// orderedSearcher 按预置顺序返回命中，用于验证排序保持。
type orderedSearcher struct {
	inner     *fakeSearcher
	order     []uint64
	estimated int
}

func (o *orderedSearcher) EnsureIndex(context.Context) error { return nil }

func (o *orderedSearcher) Upsert(_ context.Context, docs []search.Doc) error {
	if o.inner != nil {
		return o.inner.Upsert(context.Background(), docs)
	}
	return nil
}

func (o *orderedSearcher) Delete(context.Context, []uint64) error { return nil }

func (o *orderedSearcher) Query(context.Context, string, int, int) (*search.Result, error) {
	hits := make([]search.Hit, len(o.order))
	for i, id := range o.order {
		hits[i] = search.Hit{ID: id, Highlight: "<em>并发</em>入门"}
	}
	return &search.Result{Hits: hits, EstimatedTotal: o.estimated}, nil
}

func (o *orderedSearcher) Fetch(context.Context, []uint64) (map[uint64]search.Doc, error) {
	return map[uint64]search.Doc{}, nil
}

func (o *orderedSearcher) Healthy(context.Context) error { return nil }
