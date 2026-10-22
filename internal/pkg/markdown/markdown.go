// Package markdown 负责 Markdown 渲染与文章统计。
//
// 渲染策略：只允许标准 Markdown + GFM 扩展，**不开 raw HTML**。
// 打开 raw HTML 等于把 XSS 防线交给每个作者的自觉，博客开放投稿后必然出问题。
//
// 注意实际行为：goldmark 在未开启 WithUnsafe 时会把原始 HTML 整段**丢弃**
// （输出 <!-- raw HTML omitted -->），而不是转义成可见文本。
// 这是有意的取舍——宁可让作者发现"标签没生效"，也不让任何标签进入渲染管线。
package markdown

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Renderer 是线程安全的渲染器，进程内复用一个实例。
type Renderer struct {
	md goldmark.Markdown
}

// New 构造 Renderer。
func New() *Renderer {
	return &Renderer{md: goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, // 表格、删除线、任务列表、自动链接
			extension.Typographer,
		),
		goldmark.WithRendererOptions(
			// 关键：不开启 WithUnsafe，原始 HTML 会被转义成文本
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)}
}

// Render 把 Markdown 渲染为 HTML。
func (r *Renderer) Render(src string) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var (
	imageRe   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	linkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	codeFence = regexp.MustCompile("(?s)```.*?```")
	htmlTagRe = regexp.MustCompile(`<[^>]*>`)
	mdMarks   = regexp.MustCompile("[#>*_`~\\-]+")
	spacesRe  = regexp.MustCompile(`\s+`)
)

// PlainText 把 Markdown 粗略还原为纯文本，用于摘要与字数统计。
// 不求精确：摘要只需要"读起来像正文"，不需要与渲染结果逐字对应。
func PlainText(src string) string {
	s := codeFence.ReplaceAllString(src, " ")
	s = imageRe.ReplaceAllString(s, " ")
	s = linkRe.ReplaceAllString(s, "$1")
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = mdMarks.ReplaceAllString(s, " ")
	return strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
}

// Summarize 截取正文前 n 个字符作为摘要。按 rune 截断，避免切断多字节字符。
func Summarize(src string, n int) string {
	text := PlainText(src)
	if n <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
}

// Stats 统计字数与预计阅读时长。
//
// 中英混排分开算：CJK 按"字"，西文按"词"；
// 阅读速度取 350 字/分（CJK）与 200 词/分（西文），结果向上取整且至少 1 分钟。
func Stats(src string) (wordCount, readingTime int) {
	text := PlainText(src)

	var cjk, latinWords int
	inWord := false
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			cjk++
			inWord = false
		case unicode.IsLetter(r), unicode.IsDigit(r):
			if !inWord {
				latinWords++
				inWord = true
			}
		default:
			inWord = false
		}
	}

	wordCount = cjk + latinWords
	minutes := float64(cjk)/350.0 + float64(latinWords)/200.0
	readingTime = int(minutes + 0.999) // 向上取整
	if readingTime < 1 {
		readingTime = 1
	}
	return wordCount, readingTime
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify 由标题生成 slug。
//
// 只保留 ASCII 字母数字：中文标题会得到空串，调用方需回退到时间戳 slug。
// 不引入拼音库——多音字会让 slug 变成猜谜，而 slug 的可读性远不如稳定性重要。
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugInvalid.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 200 {
		s = strings.Trim(s[:200], "-")
	}
	return s
}
