package markdown_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/pkg/markdown"
)

func TestRender_BasicMarkdown(t *testing.T) {
	r := markdown.New()
	html, err := r.Render("# 标题\n\n这是**加粗**文本。\n\n- 一\n- 二")
	require.NoError(t, err)

	assert.Contains(t, html, "<h1>标题</h1>")
	assert.Contains(t, html, "<strong>加粗</strong>")
	assert.Contains(t, html, "<ul>")
	assert.Contains(t, html, "<li>一</li>")
}

// 这是安全底线：作者写的原始 HTML 必须被转义，不能原样输出。
func TestRender_EscapesRawHTML(t *testing.T) {
	r := markdown.New()
	html, err := r.Render("正常文本\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>")
	require.NoError(t, err)

	assert.NotContains(t, html, "<script>", "script 标签不能进入渲染结果")
	assert.NotContains(t, html, "onerror=", "事件属性不能被保留")
	// goldmark 未开启 WithUnsafe 时的行为是整段丢弃，不是转义
	assert.Contains(t, html, "raw HTML omitted")
}

func TestRender_GFMTable(t *testing.T) {
	r := markdown.New()
	html, err := r.Render("| a | b |\n|---|---|\n| 1 | 2 |")
	require.NoError(t, err)

	assert.Contains(t, html, "<table>", "GFM 表格扩展应生效")
}

func TestPlainText(t *testing.T) {
	src := "# 标题\n\n这是[链接](https://example.com)与![图](a.png)。\n\n```go\n代码块内容\n```"
	got := markdown.PlainText(src)

	assert.Contains(t, got, "标题")
	assert.Contains(t, got, "链接")
	assert.NotContains(t, got, "https://example.com", "链接地址应被去掉，只留文字")
	assert.NotContains(t, got, "a.png", "图片应被整段去掉")
	assert.NotContains(t, got, "代码块内容", "代码块不计入正文")
	assert.NotContains(t, got, "#", "Markdown 标记应被清理")
}

func TestSummarize(t *testing.T) {
	src := "第一段内容。第二段内容。第三段内容。"

	// 短文本原样返回
	short := markdown.Summarize("很短", 50)
	assert.Equal(t, "很短", short)

	// 超长截断并加省略号
	long := markdown.Summarize(src, 5)
	assert.True(t, strings.HasSuffix(long, "…"), "截断后应有省略号: %s", long)
	assert.LessOrEqual(t, len([]rune(strings.TrimSuffix(long, "…"))), 5, "按字符截断，不能切断多字节字符")

	// n<=0 返回空串
	assert.Equal(t, "", markdown.Summarize(src, 0))
}

func TestStats_ChineseText(t *testing.T) {
	// 14 个汉字，按 350 字/分算不足 1 分钟，应向上取整为 1
	const text = "这是一段用于测试字数统计的中文正文内容示例文本"
	words, minutes := markdown.Stats(text)
	assert.Equal(t, utf8.RuneCountInString(text), words, "纯汉字文本按字数统计")
	assert.Equal(t, 1, minutes, "不足一分钟按一分钟算")
}

func TestStats_MixedText(t *testing.T) {
	words, minutes := markdown.Stats("Go 是一门编译型语言，goroutine 很轻量")
	// "是一门编译型语言" 8 字 + "很轻量" 3 字 = 11，西文词 Go / goroutine 共 2 个
	assert.Equal(t, 11+2, words)
	assert.GreaterOrEqual(t, minutes, 1)
}

func TestStats_EmptyContentStillOneMinute(t *testing.T) {
	words, minutes := markdown.Stats("   ")
	assert.Equal(t, 0, words)
	assert.Equal(t, 1, minutes, "阅读时长至少 1 分钟，避免前端显示 0")
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":        "hello-world",
		"  Go   Concurrency": "go-concurrency",
		"A_B.C":              "a-b-c",
		"already-slug":       "already-slug",
		"!!!":                "",
		"中文标题":               "",
		"Go 并发模型详解":          "go",
	}
	for in, want := range cases {
		assert.Equal(t, want, markdown.Slugify(in), in)
	}
}

func TestSlugify_LengthCap(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := markdown.Slugify(long)
	assert.LessOrEqual(t, len(got), 200, "slug 长度必须落在契约上限内")
}
