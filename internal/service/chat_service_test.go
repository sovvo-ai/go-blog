package service

import (
	"strings"
	"testing"
	"time"

	"go-blog/internal/model"
	"go-blog/internal/repository"
)

func TestConversationKeyOrderInsensitive(t *testing.T) {
	a := repository.ConversationKey(12, 34)
	b := repository.ConversationKey(34, 12)
	if a != b {
		t.Fatalf("(A,B) 与 (B,A) 应映射到同一 key：%s vs %s", a, b)
	}
	if a != "c:12:34" {
		t.Fatalf("key 格式不符: %s", a)
	}
	// 大小 id 颠倒仍然有序
	if repository.ConversationKey(99, 1) != "c:1:99" {
		t.Fatalf("min:max 排序错误: %s", repository.ConversationKey(99, 1))
	}
}

func TestMessagePreview(t *testing.T) {
	if got := MessagePreview(model.MessageTypeImage, "chat/2026/10/1/abc.png"); got != "[图片]" {
		t.Fatalf("图片预览应为占位符: %q", got)
	}
	long := strings.Repeat("好", 200)
	got := MessagePreview(model.MessageTypeText, long)
	if n := len([]rune(got)); n != ChatPreviewMaxRunes {
		t.Fatalf("预览应截断到 %d 字，实际 %d", ChatPreviewMaxRunes, n)
	}
}

func TestConversationCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 21, 15, 30, 0, 123456789, time.Local)
	enc := encodeConversationCursor(at, 7001)
	gotAt, gotID, err := decodeConversationCursor(enc)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if !gotAt.Equal(at) || gotID != 7001 {
		t.Fatalf("round trip 不一致: %v %d", gotAt, gotID)
	}
	if _, _, err := decodeConversationCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("非法游标应报错")
	}
}

func TestSplitConversationKey(t *testing.T) {
	if a, b, ok := splitConversationKey("c:7:9"); !ok || a != 7 || b != 9 {
		t.Fatalf("解析错误: %d %d %v", a, b, ok)
	}
	if _, _, ok := splitConversationKey("garbage"); ok {
		t.Fatal("非法 key 应解析失败")
	}
}
