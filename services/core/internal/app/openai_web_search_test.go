package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func TestBuildOpenAIResponsesInputSplitsInstructions(t *testing.T) {
	instructions, input := buildOpenAIResponsesInput([]aiMessage{
		{Role: "system", Content: "house prompt"},
		{Role: "developer", Content: "search carefully"},
		{Role: "user", Content: "latest OpenAI web search docs"},
		{Role: "assistant", Content: "previous answer"},
	})

	if instructions != "house prompt\n\nsearch carefully" {
		t.Fatalf("instructions = %q", instructions)
	}
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2", len(input))
	}
	if input[0].Role != "user" || input[0].Content != "latest OpenAI web search docs" {
		t.Fatalf("unexpected first input: %#v", input[0])
	}
	if input[1].Role != "assistant" || input[1].Content != "previous answer" {
		t.Fatalf("unexpected second input: %#v", input[1])
	}
}

func TestParseOpenAIWebSearchResponseCollectsCitations(t *testing.T) {
	text, sources, usedSearch := parseOpenAIWebSearchResponse(openAIResponsesResponse{
		Output: []openAIResponsesOutputItem{
			{
				Type: "web_search_call",
				Action: &openAIWebSearchAction{
					Sources: []openAIWebSearchSource{
						{URL: "https://platform.openai.com/docs/guides/tools-web-search"},
					},
				},
			},
			{
				Type: "message",
				Content: []openAIResponsesContent{
					{
						Text: "OpenAI Web Search is available through the Responses API.",
						Annotations: []openAIURLCitation{
							{Type: "url_citation", URL: "https://platform.openai.com/docs/guides/tools-web-search", Title: "Web search"},
							{Type: "url_citation", URL: "https://openai.com/index/introducing-chatgpt-search/", Title: "Introducing ChatGPT search"},
						},
					},
				},
			},
		},
	}, 5)

	if !usedSearch {
		t.Fatalf("expected usedSearch")
	}
	if text != "OpenAI Web Search is available through the Responses API." {
		t.Fatalf("text = %q", text)
	}
	if len(sources) != 2 {
		t.Fatalf("len(sources) = %d, want 2", len(sources))
	}
	if sources[0].Title != "Web search" {
		t.Fatalf("first source title = %q", sources[0].Title)
	}
	if sources[1].Title != "Introducing ChatGPT search" {
		t.Fatalf("second source title = %q", sources[1].Title)
	}
	if sources[1].Domain != "openai.com" {
		t.Fatalf("second source domain = %q", sources[1].Domain)
	}
}

func TestParseOpenAIWebSearchResponseFallsBackToOutputText(t *testing.T) {
	text, sources, usedSearch := parseOpenAIWebSearchResponse(openAIResponsesResponse{
		OutputText: "fallback text",
		Output: []openAIResponsesOutputItem{
			{Type: "web_search_call"},
		},
	}, 5)

	if !usedSearch {
		t.Fatalf("expected usedSearch")
	}
	if text != "fallback text" {
		t.Fatalf("text = %q", text)
	}
	if len(sources) != 0 {
		t.Fatalf("len(sources) = %d, want 0", len(sources))
	}
}

func TestIsOfficialOpenAIEndpoint(t *testing.T) {
	if !isOfficialOpenAIEndpoint("https://api.openai.com/v1") {
		t.Fatalf("expected official endpoint")
	}
	if isOfficialOpenAIEndpoint("https://example.com/v1") {
		t.Fatalf("expected non-official endpoint")
	}
}

func TestOpenAIWebSearchEndpointOrderPrefersOfficialFirst(t *testing.T) {
	endpoints := []store.ModelEndpoint{
		{BaseURL: "https://compatible-a.example.com/v1", Secret: "x", Active: true},
		{BaseURL: "https://api.openai.com/v1", Secret: "y", Active: true},
		{BaseURL: "https://compatible-b.example.com/v1", Secret: "z", Active: true},
	}
	got := openAIWebSearchEndpointOrder(endpoints)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if !isOfficialOpenAIEndpoint(got[0].BaseURL) {
		t.Fatalf("expected official endpoint first, got %#v", got[0])
	}
	if isOfficialOpenAIEndpoint(got[1].BaseURL) || isOfficialOpenAIEndpoint(got[2].BaseURL) {
		t.Fatalf("expected compatible endpoints after official, got %#v", got)
	}
}

func TestOpenAIResponsesWebSearchFallsBackToPreviewTool(t *testing.T) {
	toolTypes := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Tools []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		toolType, _ := payload.Tools[0]["type"].(string)
		toolTypes = append(toolTypes, toolType)
		w.Header().Set("Content-Type", "application/json")
		if toolType == "web_search" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unsupported tool type web_search"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"web_search_call","action":{"sources":[{"url":"https://example.com","title":"Example"}]}},{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`))
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.5"}
	endpoint := store.ModelEndpoint{BaseURL: upstream.URL, Secret: "test-key", Active: true}
	text, sources, err := server.callOpenAIResponsesWebSearch(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "search"}}, 5)
	if err != nil {
		t.Fatalf("callOpenAIResponsesWebSearch() error = %v", err)
	}
	if text != "OK" {
		t.Fatalf("text = %q", text)
	}
	if len(sources) != 1 || sources[0].URL != "https://example.com" {
		t.Fatalf("sources = %#v", sources)
	}
	if len(toolTypes) != 2 || toolTypes[0] != "web_search" || toolTypes[1] != "web_search_preview" {
		t.Fatalf("toolTypes = %#v", toolTypes)
	}
}
