package app

import "testing"

func TestNormalizeConversationTitle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expects string
	}{
		{
			name:    "strips polite and action prefixes",
			input:   "帮我系统梳理一下 Infinite-AI 用户端最近聊天标题怎么优化，顺便给一个三点总结。",
			expects: "Infinite-AI 用户端最近聊天标题怎么优化",
		},
		{
			name:    "uses first meaningful clause",
			input:   "请你分析一下今天 Infinite-AI 的会员页面为什么转化低；再给两个优化方向",
			expects: "今天 Infinite-AI 的会员页面为什么转化低",
		},
		{
			name:    "falls back when empty",
			input:   "   ",
			expects: "新聊天",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeConversationTitle(test.input); got != test.expects {
				t.Fatalf("normalizeConversationTitle(%q) = %q, want %q", test.input, got, test.expects)
			}
		})
	}
}

func TestMaybeConversationTitleAcceptsPlaceholderTitles(t *testing.T) {
	if got := maybeConversationTitle("新聊天", "帮我写一个套餐页升级文案"); got != "套餐页升级文案" {
		t.Fatalf("maybeConversationTitle(new chat) = %q", got)
	}
	if got := maybeConversationTitle("新对话", "帮我整理后台模型配置保存失败的排查步骤"); got != "后台模型配置保存失败的排查步骤" {
		t.Fatalf("maybeConversationTitle(new conversation) = %q", got)
	}
	if got := maybeConversationTitle("已经命名好的标题", "帮我再改一个标题"); got != "" {
		t.Fatalf("maybeConversationTitle(named conversation) = %q, want empty", got)
	}
}
