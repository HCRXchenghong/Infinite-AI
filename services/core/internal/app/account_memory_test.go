package app

import (
	"strings"
	"testing"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func TestNormalizeAccountMemoryContent(t *testing.T) {
	long := strings.Repeat("记忆", 120)
	got := normalizeAccountMemoryContent("  我喜欢海盐柚子\t7391 \n")
	if got != "我喜欢海盐柚子 7391" {
		t.Fatalf("normalizeAccountMemoryContent() = %q, want normalized Chinese memory", got)
	}

	truncated := normalizeAccountMemoryContent(long)
	if !strings.HasSuffix(truncated, "...") {
		t.Fatalf("expected long memory to end with ellipsis, got %q", truncated)
	}
	if len([]rune(strings.TrimSuffix(truncated, "..."))) != 180 {
		t.Fatalf("expected truncated memory to keep 180 runes, got %d", len([]rune(strings.TrimSuffix(truncated, "..."))))
	}
}

func TestBuildAccountMemorySystemMessage(t *testing.T) {
	message, ok := buildAccountMemorySystemMessage([]store.Message{
		{Role: "user", Content: "   "},
		{Role: "user", Content: "我的跨对话记忆口令是海盐柚子 7391"},
		{Role: "assistant", Content: "助手回复不应成为账号记忆"},
	})
	if !ok {
		t.Fatalf("expected account memory message")
	}
	if message.Role != "system" {
		t.Fatalf("message.Role = %q, want system", message.Role)
	}
	if !strings.Contains(message.Content, "账号级记忆") {
		t.Fatalf("expected Chinese account memory instruction, got %q", message.Content)
	}
	if !strings.Contains(message.Content, "我的跨对话记忆口令是海盐柚子 7391") {
		t.Fatalf("expected memory content to be included, got %q", message.Content)
	}
	if strings.Contains(message.Content, "助手回复不应成为账号记忆") {
		t.Fatalf("assistant messages should not be included in account memory: %q", message.Content)
	}
	if strings.Contains(message.Content, "   \n") {
		t.Fatalf("blank memory should not be included: %q", message.Content)
	}
}
