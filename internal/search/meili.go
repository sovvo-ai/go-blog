package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"
)

// 索引字段名集中定义，避免散落字符串拼错。
const (
	fieldID          = "id"
	fieldTitle       = "title"
	fieldSummary     = "summary"
	fieldContent     = "content"
	fieldAuthorID    = "author_id"
	fieldCategoryID  = "category_id"
	fieldTagIDs      = "tag_ids"
	fieldTagNames    = "tag_names"
	fieldPublishedAt = "published_at"

	highlightPreTag  = "<em>"
	highlightPostTag = "</em>"

	// 正文片段长度（字符），只用于生成搜索结果的摘要行
	highlightSnippet = 80

	taskWaitInterval = 50 * time.Millisecond
	taskWaitTimeout  = 15 * time.Second
)

// Meili 是基于 Meilisearch 的 Searcher 实现。
type Meili struct {
	client meilisearch.ServiceManager
	index  string
}

// NewMeili 构造 Meilisearch 检索器。
func NewMeili(host, apiKey, index string) *Meili {
	opts := make([]meilisearch.Option, 0, 1)
	if apiKey != "" {
		opts = append(opts, meilisearch.WithAPIKey(apiKey))
	}
	return &Meili{
		client: meilisearch.New(strings.TrimRight(host, "/"), opts...),
		index:  index,
	}
}

// Healthy 探测服务可用性。
func (m *Meili) Healthy(ctx context.Context) error {
	resp, err := m.client.HealthWithContext(ctx)
	if err != nil {
		return err
	}
	if resp.Status != "available" {
		return fmt.Errorf("检索服务状态异常: %s", resp.Status)
	}
	return nil
}

// EnsureIndex 创建索引（已存在则跳过）并下发设置。
func (m *Meili) EnsureIndex(ctx context.Context) error {
	if _, err := m.client.GetIndexWithContext(ctx, m.index); err != nil {
		// 只有"索引不存在"才创建；其余错误直接返回，
		// 否则连接失败会被误判为"缺索引"，进而掩盖真实故障
		if !isIndexNotFound(err) {
			return fmt.Errorf("查询索引失败: %w", err)
		}
		task, cerr := m.client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
			Uid:        m.index,
			PrimaryKey: fieldID,
		})
		if cerr != nil && !isIndexAlreadyExists(cerr) {
			return fmt.Errorf("创建索引失败: %w", cerr)
		}
		if task != nil {
			if err := m.wait(ctx, task.TaskUID); err != nil {
				return err
			}
		}
	}

	// 设置可重复下发，幂等
	task, err := m.client.Index(m.index).UpdateSettingsWithContext(ctx, &meilisearch.Settings{
		// 顺序即权重：标题最高，正文最低
		SearchableAttributes: []string{fieldTitle, fieldSummary, fieldContent, fieldTagNames},
		FilterableAttributes: []string{fieldAuthorID, fieldCategoryID, fieldTagIDs, fieldPublishedAt},
		// V1.0 只允许按发布时间排序
		SortableAttributes: []string{fieldPublishedAt},
	})
	if err != nil {
		return fmt.Errorf("下发索引设置失败: %w", err)
	}
	return m.wait(ctx, task.TaskUID)
}

// Upsert 覆盖写入文档。
func (m *Meili) Upsert(ctx context.Context, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	task, err := m.client.Index(m.index).AddDocumentsWithContext(ctx, docs, &meilisearch.DocumentOptions{
		PrimaryKey: strPtr(fieldID),
	})
	if err != nil {
		return fmt.Errorf("写入索引失败: %w", err)
	}
	return m.wait(ctx, task.TaskUID)
}

// Delete 删除文档；文档不存在视为成功。
func (m *Meili) Delete(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.FormatUint(id, 10)
	}
	task, err := m.client.Index(m.index).DeleteDocumentsWithContext(ctx, strIDs, nil)
	if err != nil {
		return fmt.Errorf("删除索引文档失败: %w", err)
	}
	return m.wait(ctx, task.TaskUID)
}

// Query 执行检索。
func (m *Meili) Query(ctx context.Context, q string, offset, limit int) (*Result, error) {
	req := &meilisearch.SearchRequest{
		Offset:               int64(offset),
		Limit:                int64(limit),
		AttributesToRetrieve: []string{fieldID},
		// 高亮标题与摘要；正文只用于生成片段，不整体返回
		AttributesToHighlight: []string{fieldTitle, fieldSummary, fieldContent},
		AttributesToCrop:      []string{fieldContent},
		CropLength:            highlightSnippet,
		HighlightPreTag:       highlightPreTag,
		HighlightPostTag:      highlightPostTag,
		// 相关度优先，同相关度内按发布时间倒序
		Sort: []string{fieldPublishedAt + ":desc"},
	}

	resp, err := m.client.Index(m.index).SearchWithContext(ctx, q, req)
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	out := &Result{
		EstimatedTotal: int(resp.EstimatedTotalHits),
		Hits:           make([]Hit, 0, len(resp.Hits)),
	}
	for _, raw := range resp.Hits {
		hit, ok := parseHit(raw)
		if !ok {
			// 单条脏数据不应让整次检索失败
			continue
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

// Fetch 按 ID 取回索引文档，供抽样对账比对标题。
func (m *Meili) Fetch(ctx context.Context, ids []uint64) (map[uint64]Doc, error) {
	if len(ids) == 0 {
		return map[uint64]Doc{}, nil
	}
	req := &meilisearch.SearchRequest{
		Limit:                int64(len(ids)),
		Filter:               buildIDFilter(ids),
		AttributesToRetrieve: []string{fieldID, fieldTitle},
	}
	resp, err := m.client.Index(m.index).SearchWithContext(ctx, "", req)
	if err != nil {
		return nil, fmt.Errorf("读取索引文档失败: %w", err)
	}

	out := make(map[uint64]Doc, len(resp.Hits))
	for _, raw := range resp.Hits {
		var doc struct {
			ID    uint64 `json:"id"`
			Title string `json:"title"`
		}
		if err := raw.DecodeInto(&doc); err != nil || doc.ID == 0 {
			continue
		}
		out[doc.ID] = Doc{ID: doc.ID, Title: doc.Title}
	}
	return out, nil
}

// wait 等待异步任务完成。
//
// Meilisearch 的写操作是异步的：不等待就返回"成功"，
// 索引实际尚未生效，紧接着的搜索会查不到——这类"看起来成功"的返回最难排查。
func (m *Meili) wait(ctx context.Context, taskUID int64) error {
	if taskUID == 0 {
		return nil
	}
	wctx, cancel := context.WithTimeout(ctx, taskWaitTimeout)
	defer cancel()
	task, err := m.client.WaitForTaskWithContext(wctx, taskUID, taskWaitInterval)
	if err != nil {
		return fmt.Errorf("等待索引任务失败: %w", err)
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("索引任务未成功: status=%s err=%v", task.Status, task.Error)
	}
	return nil
}

// parseHit 从原始命中里提取 ID 与高亮片段。
func parseHit(raw meilisearch.Hit) (Hit, bool) {
	var id uint64
	rawID, ok := raw[fieldID]
	if !ok {
		return Hit{}, false
	}
	if err := json.Unmarshal(rawID, &id); err != nil || id == 0 {
		return Hit{}, false
	}

	hit := Hit{ID: id}
	rawFormatted, ok := raw["_formatted"]
	if !ok {
		return hit, true
	}
	var f struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rawFormatted, &f); err != nil {
		return hit, true
	}

	// 优先级：标题命中 > 摘要命中 > 正文片段
	switch {
	case strings.Contains(f.Title, highlightPreTag):
		hit.Highlight = f.Title
	case strings.Contains(f.Summary, highlightPreTag):
		hit.Highlight = f.Summary
	case f.Content != "":
		hit.Highlight = f.Content
	}
	return hit, true
}

func buildIDFilter(ids []uint64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(id, 10)
	}
	return fieldID + " IN [" + strings.Join(parts, ", ") + "]"
}

func strPtr(s string) *string { return &s }

// isIndexNotFound 判断错误是否为"索引不存在"。
func isIndexNotFound(err error) bool {
	var me *meilisearch.Error
	if errors.As(err, &me) {
		return me.MeilisearchApiError.Code == "index_not_found"
	}
	return strings.Contains(err.Error(), "index_not_found")
}

func isIndexAlreadyExists(err error) bool {
	return strings.Contains(err.Error(), "index_already_exists")
}
