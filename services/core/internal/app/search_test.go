package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func TestNormalizeDeepSearchQueryExtractsEntity(t *testing.T) {
	input := "深度搜索链路验收：请用一句中文说明 SearXNG 是什么，并保留引用编号。"
	if got := normalizeDeepSearchQuery(input); got != "SearXNG" {
		t.Fatalf("normalizeDeepSearchQuery() = %q, want SearXNG", got)
	}
}

func TestNormalizeDeepSearchQueryUsesQuotedText(t *testing.T) {
	input := "请检索“Infinite-AI 开发者平台 API URL”并给出引用"
	if got := normalizeDeepSearchQuery(input); got != "Infinite-AI 开发者平台 API URL" {
		t.Fatalf("normalizeDeepSearchQuery() = %q", got)
	}
}

func TestNormalizeDeepSearchQueryFallsBackToCleanedChinese(t *testing.T) {
	input := "请帮我深度搜索今天的人工智能行业趋势，并保留引用编号。"
	if got := normalizeDeepSearchQuery(input); got != "今天的人工智能行业趋势" {
		t.Fatalf("normalizeDeepSearchQuery() = %q", got)
	}
}

func TestNormalizeDeepSearchQueryKeepsBeijingTimeIntent(t *testing.T) {
	input := "你知道现在是北京时间几点嘛"
	if got := normalizeDeepSearchQuery(input); got != "北京时间 现在几点" {
		t.Fatalf("normalizeDeepSearchQuery() = %q", got)
	}
}

func TestPlanDeepSearchQueriesAddsSecondPassForFreshness(t *testing.T) {
	queries := planDeepSearchQueries("请帮我搜索今天的人工智能行业趋势", "今天的人工智能行业趋势")
	if len(queries) < 2 {
		t.Fatalf("queries = %#v, want at least two planned searches", queries)
	}
	if queries[0] != "今天的人工智能行业趋势" {
		t.Fatalf("first query = %q", queries[0])
	}
}

func TestDeepSearchCandidateScoringPrefersRelevantSource(t *testing.T) {
	relevant := deepSearchCandidate{
		Title:   "OpenAI Responses API web search documentation",
		URL:     "https://platform.openai.com/docs/guides/tools-web-search",
		Snippet: "Use the web_search tool with the Responses API.",
		Domain:  "platform.openai.com",
		Rank:    2,
	}
	irrelevant := deepSearchCandidate{
		Title:   "Local weather and sports",
		URL:     "https://example.com/weather",
		Snippet: "A page about restaurants and weather.",
		Domain:  "example.com",
		Rank:    1,
	}
	relevant.Score = scoreDeepSearchCandidate("OpenAI Responses API web search", "OpenAI web_search", relevant)
	irrelevant.Score = scoreDeepSearchCandidate("OpenAI Responses API web search", "OpenAI web_search", irrelevant)
	if relevant.Score <= irrelevant.Score {
		t.Fatalf("relevant score %.3f <= irrelevant score %.3f", relevant.Score, irrelevant.Score)
	}
	if isRelevantSearchCandidate(irrelevant, 1) {
		t.Fatalf("irrelevant candidate should be filtered: %#v", irrelevant)
	}
}

func TestApplyDeepSearchFallbackInjectsSearXNGSources(t *testing.T) {
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"OpenAI","url":"https://openai.com/","content":"OpenAI latest news","engine":"test"}]}`))
	}))
	defer searchServer.Close()

	server := &Server{}
	config := &store.SearchProviderConfig{
		Enabled:        true,
		Provider:       "openai_then_searxng",
		BaseURL:        searchServer.URL,
		ResultCount:    5,
		TimeoutSeconds: 2,
	}

	messages, sources := server.applyDeepSearchFallback(context.Background(), []aiMessage{{Role: "user", Content: "请搜索 OpenAI"}}, "OpenAI", config, nil)
	if len(sources) != 1 || sources[0].URL != "https://openai.com/" {
		t.Fatalf("sources = %#v", sources)
	}
	if len(messages) == 0 || !strings.Contains(messages[0].Content, "OpenAI latest news") {
		t.Fatalf("expected search source message first, got %#v", messages)
	}
}

func TestBuildDeepSearchUnavailableMessage(t *testing.T) {
	message := buildDeepSearchUnavailableMessage()
	if message.Role != "system" || !strings.Contains(message.Content, "不要声称已经完成联网核验") {
		t.Fatalf("message = %#v", message)
	}
}
