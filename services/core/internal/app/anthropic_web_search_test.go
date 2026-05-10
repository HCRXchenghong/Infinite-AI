package app

import "testing"

func TestParseAnthropicWebSearchResponseCollectsResultsAndCitations(t *testing.T) {
	text, sources, usedSearch, searchErr := parseAnthropicWebSearchResponse(anthropicWebSearchResponse{
		Content: []anthropicWebSearchContent{
			{
				Type: "server_tool_use",
				Name: "web_search",
			},
			{
				Type: "web_search_tool_result",
				Content: []anthropicWebSearchResult{
					{Type: "web_search_result", URL: "https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/web-search-tool", Title: "Web search tool"},
				},
			},
			{
				Type: "text",
				Text: "Claude supports native web search through the Messages API.",
				Citations: []anthropicWebSearchCitation{
					{Type: "web_search_result_location", URL: "https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/web-search-tool", Title: "Claude web search"},
					{Type: "web_search_result_location", URL: "https://support.claude.com/en/articles/10684626-enabling-and-using-web-search", Title: "Claude support"},
				},
			},
		},
	}, 5)

	if searchErr != "" {
		t.Fatalf("unexpected search error: %q", searchErr)
	}
	if !usedSearch {
		t.Fatalf("expected usedSearch")
	}
	if text != "Claude supports native web search through the Messages API." {
		t.Fatalf("text = %q", text)
	}
	if len(sources) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(sources))
	}
	if sources[0].Title != "Web search tool" {
		t.Fatalf("first source title = %q", sources[0].Title)
	}
	if sources[1].Domain != "support.claude.com" {
		t.Fatalf("second source domain = %q", sources[1].Domain)
	}
}

func TestParseAnthropicWebSearchResponseReportsToolError(t *testing.T) {
	_, _, usedSearch, searchErr := parseAnthropicWebSearchResponse(anthropicWebSearchResponse{
		Content: []anthropicWebSearchContent{
			{
				Type: "web_search_tool_result",
				Content: map[string]any{
					"type":       "web_search_tool_result_error",
					"error_code": "max_uses_exceeded",
				},
			},
			{
				Type: "text",
				Text: "Search failed.",
			},
		},
	}, 5)

	if !usedSearch {
		t.Fatalf("expected usedSearch")
	}
	if searchErr != "max_uses_exceeded" {
		t.Fatalf("searchErr = %q", searchErr)
	}
}

func TestIsOfficialAnthropicEndpoint(t *testing.T) {
	if !isOfficialAnthropicEndpoint("https://api.anthropic.com/v1") {
		t.Fatalf("expected official Anthropic endpoint")
	}
	if isOfficialAnthropicEndpoint("https://example.com/v1") {
		t.Fatalf("expected non-official endpoint")
	}
}
