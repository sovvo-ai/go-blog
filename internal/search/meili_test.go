package search

import (
	"encoding/json"
	"testing"

	"github.com/meilisearch/meilisearch-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHit_ExtractsIDAndHighlight(t *testing.T) {
	raw := meilisearch.Hit{
		"id": mustJSON(uint64(7)),
		"_formatted": mustJSON(map[string]any{
			"title":   "Go <em>并发</em>模型",
			"summary": "没有命中",
			"content": "也没有",
		}),
	}
	hit, ok := parseHit(raw)
	require.True(t, ok)
	assert.Equal(t, uint64(7), hit.ID)
	assert.Equal(t, "Go <em>并发</em>模型", hit.Highlight)
}

func TestParseHit_FallsBackToSummaryThenContent(t *testing.T) {
	// 标题未命中，摘要命中
	hit, ok := parseHit(meilisearch.Hit{
		"id": mustJSON(uint64(1)),
		"_formatted": mustJSON(map[string]any{
			"title":   "干净标题",
			"summary": "摘要里有 <em>关键词</em>",
			"content": "",
		}),
	})
	require.True(t, ok)
	assert.Equal(t, "摘要里有 <em>关键词</em>", hit.Highlight)

	// 都没命中，用正文片段
	hit, ok = parseHit(meilisearch.Hit{
		"id": mustJSON(uint64(2)),
		"_formatted": mustJSON(map[string]any{
			"title":   "干净标题",
			"summary": "干净摘要",
			"content": "…正文片段…",
		}),
	})
	require.True(t, ok)
	assert.Equal(t, "…正文片段…", hit.Highlight)
}

func TestParseHit_RejectsMissingOrInvalidID(t *testing.T) {
	_, ok := parseHit(meilisearch.Hit{})
	assert.False(t, ok, "缺少 id 必须跳过")

	_, ok = parseHit(meilisearch.Hit{"id": mustJSON("abc")})
	assert.False(t, ok, "非法 id 必须跳过")
}

func TestParseHit_NoFormattedBlock(t *testing.T) {
	hit, ok := parseHit(meilisearch.Hit{"id": mustJSON(uint64(3))})
	require.True(t, ok, "没有高亮块也应返回命中，只是 Highlight 为空")
	assert.Equal(t, "", hit.Highlight)
}

func TestBuildIDFilter(t *testing.T) {
	assert.Equal(t, "id IN [1, 2, 3]", buildIDFilter([]uint64{1, 2, 3}))
}

func TestIsIndexNotFound(t *testing.T) {
	assert.True(t, isIndexNotFound(errString("... code: index_not_found ...")))
	assert.False(t, isIndexNotFound(errString("invalid_api_key")))
}

type errString string

func (e errString) Error() string { return string(e) }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
