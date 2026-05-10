package app

import "testing"

func TestNormalizeProviderRole(t *testing.T) {
	tests := map[string]string{
		"assistant": "assistant",
		"ai":        "assistant",
		"AI":        "assistant",
		"system":    "system",
		"developer": "developer",
		"user":      "user",
		"":          "user",
		"custom":    "user",
	}

	for input, want := range tests {
		if got := normalizeProviderRole(input); got != want {
			t.Fatalf("normalizeProviderRole(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSplitAnthropicSystemPromptNormalizesMessages(t *testing.T) {
	systemPrompt, messages := splitAnthropicSystemPrompt([]aiMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "ai", Content: "assistant reply"},
		{Role: "developer", Content: "developer hint"},
		{Role: "user", Content: "user question"},
	})

	if systemPrompt != "system prompt" {
		t.Fatalf("systemPrompt = %q, want %q", systemPrompt, "system prompt")
	}
	if len(messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(messages))
	}

	if messages[0].Role != "assistant" {
		t.Fatalf("messages[0].Role = %q, want assistant", messages[0].Role)
	}
	if messages[1].Role != "user" {
		t.Fatalf("messages[1].Role = %q, want user", messages[1].Role)
	}
	if messages[2].Role != "user" {
		t.Fatalf("messages[2].Role = %q, want user", messages[2].Role)
	}
}

func TestParseVisibleReasoningEnvelope(t *testing.T) {
	result := parseVisibleReasoningEnvelope(`
<infinite_thinking>
先核对配置是否真正保存，再判断是前端状态问题、BFF 鉴权问题，还是核心服务落库失败。
</infinite_thinking>
<infinite_answer>
可以先从保存接口响应、数据库记录和浏览器控制台三处排查。
</infinite_answer>
`)

	if result.ReasoningContent == "" {
		t.Fatalf("expected reasoning content to be parsed")
	}
	if result.Content != "可以先从保存接口响应、数据库记录和浏览器控制台三处排查。" {
		t.Fatalf("unexpected answer content: %q", result.Content)
	}
}

func TestParseVisibleReasoningEnvelopeFallback(t *testing.T) {
	result := parseVisibleReasoningEnvelope("直接给出最终回答，没有标签。")
	if result.ReasoningContent != "" {
		t.Fatalf("unexpected reasoning content: %q", result.ReasoningContent)
	}
	if result.Content != "直接给出最终回答，没有标签。" {
		t.Fatalf("unexpected answer content: %q", result.Content)
	}
}

func TestParseVisibleReasoningEnvelopeFallbackStripsThinkingBlock(t *testing.T) {
	result := parseVisibleReasoningEnvelope(`
<infinite_thinking>
先检查会话恢复，再检查流式结果是否串到了其他聊天窗口。
</infinite_thinking>
最终只保留这段正常回答。
`)
	if result.ReasoningContent != "先检查会话恢复，再检查流式结果是否串到了其他聊天窗口。" {
		t.Fatalf("unexpected reasoning content: %q", result.ReasoningContent)
	}
	if result.Content != "最终只保留这段正常回答。" {
		t.Fatalf("unexpected answer content: %q", result.Content)
	}
}

func TestSplitChunksUsesRunes(t *testing.T) {
	chunks := splitChunks("深度搜索思考内容", 3)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	for _, chunk := range chunks {
		for _, r := range chunk {
			if r == '\uFFFD' {
				t.Fatalf("unexpected replacement rune in chunk %q", chunk)
			}
		}
	}
}
