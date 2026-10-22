package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go-blog/internal/model"
	"go-blog/internal/repository"
	"go-blog/internal/search"
)

// IndexStore 是索引同步所需的数据能力。
type IndexStore interface {
	// IndexDoc 组装索引文档；第二返回值为"是否应存在于索引中"。
	IndexDoc(ctx context.Context, id uint64) (*search.Doc, bool, error)
	MarkSearchSynced(ctx context.Context, id uint64, at time.Time) error
	MarkSearchFailed(ctx context.Context, id uint64) error
	PendingSearchIDs(ctx context.Context, limit int) ([]uint64, error)
	SamplePublishedForCheck(ctx context.Context, limit int) ([]model.Article, error)
	PublishedIDsPage(ctx context.Context, afterID uint64, limit int) ([]uint64, error)
}

// IndexService 负责把数据库的文章状态同步到检索索引。
//
// 一条硬规则：**内容永远从数据库重新读取，不接受队列 payload 里的正文**。
// payload 只放 ID，否则队列积压期间文章被改过，索引就会写成旧版本。
type IndexService struct {
	store    IndexStore
	searcher search.Searcher
	log      *slog.Logger
	now      func() time.Time
}

// NewIndexService 构造 IndexService。
func NewIndexService(store IndexStore, searcher search.Searcher, log *slog.Logger) *IndexService {
	return &IndexService{store: store, searcher: searcher, log: log, now: time.Now}
}

// EnsureIndex 创建索引并下发设置，幂等。
// 必须在进程启动时调用：索引不存在时写入会失败，而查询只会默默返回空。
func (s *IndexService) EnsureIndex(ctx context.Context) error {
	return s.searcher.EnsureIndex(ctx)
}

// Sync 同步单篇文章。
//
// 三种情况统一在这里处理，避免"该删还是该写"的判断散落多处：
//   - 文章不存在或不可公开 → 从索引删除
//   - 公开 → 覆盖写入
//
// 返回 error 让 asynq 决定是否重试；重试耗尽后由对账任务兜底。
func (s *IndexService) Sync(ctx context.Context, articleID uint64) error {
	doc, shouldIndex, err := s.store.IndexDoc(ctx, articleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// 文章已被物理删除，索引里也不该留着
			if derr := s.searcher.Delete(ctx, []uint64{articleID}); derr != nil {
				return s.fail(ctx, articleID, derr)
			}
			return nil
		}
		return s.fail(ctx, articleID, err)
	}

	if !shouldIndex {
		if derr := s.searcher.Delete(ctx, []uint64{articleID}); derr != nil {
			return s.fail(ctx, articleID, derr)
		}
		return s.synced(ctx, articleID)
	}

	if uerr := s.searcher.Upsert(ctx, []search.Doc{*doc}); uerr != nil {
		return s.fail(ctx, articleID, uerr)
	}
	return s.synced(ctx, articleID)
}

// Unindex 从索引删除文档；文档不存在视为成功。
func (s *IndexService) Unindex(ctx context.Context, articleID uint64) error {
	if err := s.searcher.Delete(ctx, []uint64{articleID}); err != nil {
		return s.fail(ctx, articleID, err)
	}
	// 删除成功后必须落"已同步"状态，否则对账任务会把它当成
	// 待同步反复处理，空转一个对账周期。
	return s.synced(ctx, articleID)
}

// ReconcileIncremental 扫描待同步文章并重新入索引。
//
// 这是"任务投递失败"的兜底：即使 enqueue 失败、状态写不进去，
// 只要 search_sync_status 或 updated_at 有痕迹，这里就能重新发现。
func (s *IndexService) ReconcileIncremental(ctx context.Context, limit int) (int, error) {
	ids, err := s.store.PendingSearchIDs(ctx, limit)
	if err != nil {
		return 0, err
	}
	fixed := 0
	for _, id := range ids {
		if err := s.Sync(ctx, id); err != nil {
			// 单条失败不中断整轮，继续处理其余文章
			s.log.Warn("对账同步失败", "article_id", id, "err", err)
			continue
		}
		fixed++
	}
	return fixed, nil
}

// CheckSample 抽样比对索引与数据库的标题，发现静默漂移。
//
// 为什么需要它：任务可能"报成功但索引写入静默失败"，
// 这类问题只有实际读取索引内容才能发现（TECH_DESIGN 9.3）。
func (s *IndexService) CheckSample(ctx context.Context, limit int) (int, error) {
	rows, err := s.store.SamplePublishedForCheck(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	ids := make([]uint64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	indexed, err := s.searcher.Fetch(ctx, ids)
	if err != nil {
		return 0, err
	}

	mismatched := 0
	for _, row := range rows {
		doc, ok := indexed[row.ID]
		if ok && doc.Title == row.Title {
			continue
		}
		mismatched++
		// 重置为待同步，交给增量对账重建
		if merr := s.store.MarkSearchFailed(ctx, row.ID); merr != nil {
			s.log.Warn("标记抽样异常失败", "article_id", row.ID, "err", merr)
		}
	}
	return mismatched, nil
}

// RebuildAll 全量重建索引，分批读取避免一次性加载全部文章。
func (s *IndexService) RebuildAll(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 200
	}
	var afterID uint64
	total := 0
	for {
		ids, err := s.store.PublishedIDsPage(ctx, afterID, batch)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}

		docs := make([]search.Doc, 0, len(ids))
		for _, id := range ids {
			doc, ok, derr := s.store.IndexDoc(ctx, id)
			if derr != nil || !ok {
				s.log.Warn("重建时跳过文章", "article_id", id, "err", derr, "public", ok)
				continue
			}
			docs = append(docs, *doc)
		}
		if err := s.searcher.Upsert(ctx, docs); err != nil {
			return total, err
		}
		for _, id := range ids {
			if merr := s.store.MarkSearchSynced(ctx, id, s.now()); merr != nil {
				s.log.Warn("标记同步状态失败", "article_id", id, "err", merr)
			}
		}
		total += len(docs)
		afterID = ids[len(ids)-1]
	}
}

func (s *IndexService) synced(ctx context.Context, id uint64) error {
	if err := s.store.MarkSearchSynced(ctx, id, s.now()); err != nil {
		// 索引已写好，仅状态写失败：不返回错误，交给对账任务修正，
		// 否则会触发无意义的重试并重复写索引。
		s.log.Warn("标记索引同步状态失败", "article_id", id, "err", err)
	}
	return nil
}

func (s *IndexService) fail(ctx context.Context, id uint64, cause error) error {
	if err := s.store.MarkSearchFailed(ctx, id); err != nil {
		s.log.Warn("标记索引失败状态出错", "article_id", id, "err", err)
	}
	s.log.Error("索引同步失败", "article_id", id, "err", cause)
	return cause
}
