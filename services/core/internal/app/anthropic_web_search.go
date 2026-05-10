package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

var errAnthropicWebSearchUnavailable = errors.New("anthropic official web search unavailable")

const anthropicWebSearchInstructionPrompt = "当前对话开启了联网检索。请先使用内置 web_search 工具围绕用户原问题检索和核对信息，只采用与问题直接相关的来源；如果检索结果相关性不足，要在最终回答里说明不确定性。最终回答中的关键事实尽量用 [1]、[2] 这样的编号标注来源。"

type anthropicWebSearchResponse struct {
	Content []anthropicWebSearchContent `json:"content"`
}

type anthropicWebSearchContent struct {
	Type      string                       `json:"type"`
	Text      string                       `json:"text,omitempty"`
	Name      string                       `json:"name,omitempty"`
	Content   any                          `json:"content,omitempty"`
	Citations []anthropicWebSearchCitation `json:"citations,omitempty"`
	Input     map[string]any               `json:"input,omitempty"`
}

type anthropicWebSearchCitation struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CitedText string `json:"cited_text"`
}

type anthropicWebSearchResult struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	PageAge string `json:"page_age"`
}

type anthropicWebSearchToolError struct {
	Type      string `json:"type"`
	ErrorCode string `json:"error_code"`
}

func (s *Server) generateAnthropicWebSearchResponse(ctx context.Context, routeSlug string, messages []aiMessage, sourceLimit int) (aiResponseEnvelope, []store.SearchSource, error) {
	if routeSlug == "" {
		routeSlug = s.Config.DefaultChatRoute
	}
	route, err := s.Store.FindActiveModelRoute(ctx, routeSlug, true)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	if route.Protocol != "anthropic" || route.ModelType == "image" || strings.TrimSpace(route.UpstreamModel) == "" {
		return aiResponseEnvelope{}, nil, errAnthropicWebSearchUnavailable
	}

	webSearchMessages := append([]aiMessage{
		{Role: "system", Content: anthropicWebSearchInstructionPrompt},
		{Role: "system", Content: deepSearchVisibleReasoningPrompt},
	}, messages...)
	webSearchMessages = applyRoutePrompt(route, webSearchMessages)

	var lastErr error
	for _, endpoint := range route.Endpoints {
		if !endpoint.Active || endpoint.BaseURL == "" || endpoint.Secret == "" {
			continue
		}
		raw, sources, err := s.callAnthropicMessagesWebSearch(ctx, route, endpoint, webSearchMessages, sourceLimit)
		if err == nil {
			return parseVisibleReasoningEnvelope(raw), sources, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return aiResponseEnvelope{}, nil, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = errAnthropicWebSearchUnavailable
	}
	return aiResponseEnvelope{}, nil, lastErr
}

func (s *Server) callAnthropicMessagesWebSearch(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, sourceLimit int) (string, []store.SearchSource, error) {
	systemPrompt, anthropicMessages := splitAnthropicSystemPrompt(messages)
	if len(anthropicMessages) == 0 {
		return "", nil, errAnthropicWebSearchUnavailable
	}
	var lastErr error
	for _, toolType := range []string{"web_search_20250305"} {
		text, sources, err := s.callAnthropicMessagesWebSearchWithTool(ctx, route, endpoint, systemPrompt, anthropicMessages, sourceLimit, toolType)
		if err == nil {
			return text, sources, nil
		}
		lastErr = err
		if !shouldTryNextAnthropicWebSearchTool(err) {
			return "", nil, err
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
	}
	if lastErr != nil {
		return "", nil, lastErr
	}
	return "", nil, errAnthropicWebSearchUnavailable
}

func (s *Server) callAnthropicMessagesWebSearchWithTool(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, systemPrompt string, anthropicMessages []aiMessage, sourceLimit int, toolType string) (string, []store.SearchSource, error) {
	payload := map[string]any{
		"model":      route.UpstreamModel,
		"max_tokens": 4096,
		"messages":   buildAnthropicMessages(anthropicMessages),
		"tools": []map[string]any{
			{
				"type":     toolType,
				"name":     "web_search",
				"max_uses": 5,
			},
		},
	}
	if strings.TrimSpace(systemPrompt) != "" {
		payload["system"] = systemPrompt
	}

	body, _ := json.Marshal(payload)
	target := strings.TrimRight(endpoint.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", endpoint.Secret)
	req.Header.Set("anthropic-version", "2023-06-01")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", nil, newUpstreamHTTPError("anthropic web search", res.StatusCode, readUpstreamErrorBody(res.Body))
	}
	var decoded anthropicWebSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return "", nil, err
	}
	text, sources, usedSearch, searchErr := parseAnthropicWebSearchResponse(decoded, sourceLimit)
	if strings.TrimSpace(text) == "" {
		return "", nil, fmt.Errorf("anthropic web search returned no text")
	}
	if searchErr != "" {
		return "", nil, fmt.Errorf("anthropic web search tool error: %s", searchErr)
	}
	if !usedSearch {
		return "", nil, errAnthropicWebSearchUnavailable
	}
	return text, sources, nil
}

func shouldTryNextAnthropicWebSearchTool(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errAnthropicWebSearchUnavailable) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "web_search") ||
		strings.Contains(message, "web search") ||
		strings.Contains(message, "tool") ||
		strings.Contains(message, "unsupported") ||
		strings.Contains(message, "invalid") ||
		strings.Contains(message, "unknown")
}

func parseAnthropicWebSearchResponse(decoded anthropicWebSearchResponse, sourceLimit int) (string, []store.SearchSource, bool, string) {
	textParts := make([]string, 0, 1)
	candidates := make([]anthropicWebSearchResult, 0)
	usedSearch := false
	searchErr := ""

	for _, item := range decoded.Content {
		switch item.Type {
		case "server_tool_use":
			if item.Name == "web_search" {
				usedSearch = true
			}
		case "web_search_tool_result":
			usedSearch = true
			results, errCode := parseAnthropicWebSearchToolResult(item.Content)
			candidates = append(candidates, results...)
			if errCode != "" && searchErr == "" {
				searchErr = errCode
			}
		case "text":
			if item.Text != "" {
				textParts = append(textParts, item.Text)
			}
			for _, citation := range item.Citations {
				if citation.Type == "web_search_result_location" && strings.TrimSpace(citation.URL) != "" {
					candidates = append(candidates, anthropicWebSearchResult{
						URL:   citation.URL,
						Title: citation.Title,
					})
				}
			}
		}
	}

	text := strings.TrimSpace(strings.Join(textParts, "\n"))
	return text, anthropicWebSearchSourcesToStoreSources(candidates, sourceLimit), usedSearch, searchErr
}

func parseAnthropicWebSearchToolResult(value any) ([]anthropicWebSearchResult, string) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, ""
	}
	var singleError anthropicWebSearchToolError
	if err := json.Unmarshal(raw, &singleError); err == nil && singleError.Type == "web_search_tool_result_error" {
		return nil, singleError.ErrorCode
	}
	var results []anthropicWebSearchResult
	if err := json.Unmarshal(raw, &results); err == nil {
		return filterAnthropicWebSearchResults(results), ""
	}
	var singleResult anthropicWebSearchResult
	if err := json.Unmarshal(raw, &singleResult); err == nil && singleResult.Type == "web_search_result" {
		return []anthropicWebSearchResult{singleResult}, ""
	}
	return nil, ""
}

func filterAnthropicWebSearchResults(results []anthropicWebSearchResult) []anthropicWebSearchResult {
	out := make([]anthropicWebSearchResult, 0, len(results))
	for _, result := range results {
		if result.Type == "web_search_result" && strings.TrimSpace(result.URL) != "" {
			out = append(out, result)
		}
	}
	return out
}

func anthropicWebSearchSourcesToStoreSources(candidates []anthropicWebSearchResult, sourceLimit int) []store.SearchSource {
	if sourceLimit <= 0 {
		sourceLimit = 5
	}
	seen := map[string]int{}
	sources := make([]store.SearchSource, 0, sourceLimit)
	for _, candidate := range candidates {
		sourceURL := strings.TrimSpace(candidate.URL)
		if sourceURL == "" {
			continue
		}
		seenKey := strings.ToLower(sourceURL)
		title := strings.TrimSpace(candidate.Title)
		if existingIndex, ok := seen[seenKey]; ok {
			if title != "" && sources[existingIndex].Title == sources[existingIndex].URL {
				sources[existingIndex].Title = title
			}
			continue
		}
		if title == "" {
			title = sourceURL
		}
		parsed, _ := url.Parse(sourceURL)
		sources = append(sources, store.SearchSource{
			Title:   title,
			URL:     sourceURL,
			Snippet: strings.TrimSpace(candidate.PageAge),
			Domain:  parsed.Hostname(),
			Index:   len(sources) + 1,
		})
		seen[seenKey] = len(sources) - 1
		if len(sources) >= sourceLimit {
			break
		}
	}
	return sources
}

func isOfficialAnthropicEndpoint(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "api.anthropic.com") || strings.EqualFold(host, "api.claude.com")
}
