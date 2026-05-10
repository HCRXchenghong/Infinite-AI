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

var errOpenAIWebSearchUnavailable = errors.New("openai official web search unavailable")

const openAIWebSearchInstructionPrompt = "当前对话开启了联网检索。请先使用内置 web_search 工具围绕用户原问题检索和核对信息，只采用与问题直接相关的来源；如果检索结果相关性不足，要在最终回答里说明不确定性。最终回答中的关键事实尽量用 [1]、[2] 这样的编号标注来源。"

type openAIResponsesInputMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type openAIResponsesResponse struct {
	OutputText string                      `json:"output_text"`
	Output     []openAIResponsesOutputItem `json:"output"`
}

type openAIResponsesOutputItem struct {
	Type    string                   `json:"type"`
	Action  *openAIWebSearchAction   `json:"action,omitempty"`
	Content []openAIResponsesContent `json:"content,omitempty"`
}

type openAIResponsesContent struct {
	Type        string              `json:"type"`
	Text        string              `json:"text"`
	Annotations []openAIURLCitation `json:"annotations,omitempty"`
}

type openAIURLCitation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type openAIWebSearchAction struct {
	Type    string                  `json:"type"`
	Query   string                  `json:"query,omitempty"`
	Queries []string                `json:"queries,omitempty"`
	Sources []openAIWebSearchSource `json:"sources,omitempty"`
}

type openAIWebSearchSource struct {
	Type  string `json:"type,omitempty"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
}

func (s *Server) generateOpenAIWebSearchResponse(ctx context.Context, routeSlug string, messages []aiMessage, sourceLimit int) (aiResponseEnvelope, []store.SearchSource, error) {
	if routeSlug == "" {
		routeSlug = s.Config.DefaultChatRoute
	}
	route, err := s.Store.FindActiveModelRoute(ctx, routeSlug, true)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	if route.Protocol != "openai" || route.ModelType == "image" || strings.TrimSpace(route.UpstreamModel) == "" {
		return aiResponseEnvelope{}, nil, errOpenAIWebSearchUnavailable
	}

	webSearchMessages := append([]aiMessage{
		{Role: "system", Content: openAIWebSearchInstructionPrompt},
		{Role: "system", Content: deepSearchVisibleReasoningPrompt},
	}, messages...)
	webSearchMessages = applyRoutePrompt(route, webSearchMessages)

	var lastErr error
	for _, endpoint := range openAIWebSearchEndpointOrder(route.Endpoints) {
		raw, sources, err := s.callOpenAIResponsesWebSearch(ctx, route, endpoint, webSearchMessages, sourceLimit)
		if err == nil {
			return parseVisibleReasoningEnvelope(raw), sources, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return aiResponseEnvelope{}, nil, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = errOpenAIWebSearchUnavailable
	}
	return aiResponseEnvelope{}, nil, lastErr
}

func (s *Server) callOpenAIResponsesWebSearch(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, sourceLimit int) (string, []store.SearchSource, error) {
	instructions, input := buildOpenAIResponsesInput(messages)
	if len(input) == 0 {
		return "", nil, errOpenAIWebSearchUnavailable
	}
	var lastErr error
	for _, toolType := range []string{"web_search", "web_search_preview"} {
		text, sources, err := s.callOpenAIResponsesWebSearchWithTool(ctx, route, endpoint, instructions, input, sourceLimit, toolType)
		if err == nil {
			return text, sources, nil
		}
		lastErr = err
		if !shouldTryNextOpenAIWebSearchTool(err) {
			return "", nil, err
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
	}
	if lastErr != nil {
		return "", nil, lastErr
	}
	return "", nil, errOpenAIWebSearchUnavailable
}

func (s *Server) callOpenAIResponsesWebSearchWithTool(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, instructions string, input []openAIResponsesInputMessage, sourceLimit int, toolType string) (string, []store.SearchSource, error) {
	tool := map[string]any{"type": toolType}
	if toolType == "web_search" {
		tool["external_web_access"] = true
	}
	payload := map[string]any{
		"model":       route.UpstreamModel,
		"tools":       []map[string]any{tool},
		"tool_choice": "auto",
		"include":     []string{"web_search_call.action.sources"},
		"input":       input,
		"store":       false,
	}
	if strings.TrimSpace(instructions) != "" {
		payload["instructions"] = instructions
	}

	body, _ := json.Marshal(payload)
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/responses")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationWebSearch); ok {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/responses", cached.URL)
	}
	for _, target := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return "", nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", nil, err
		}
		if res.StatusCode >= 300 {
			bodyText := readUpstreamErrorBody(res.Body)
			_ = res.Body.Close()
			lastErr = newUpstreamHTTPError("openai web search", res.StatusCode, bodyText)
			if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
				continue
			}
			return "", nil, lastErr
		}
		var decoded openAIResponsesResponse
		if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			return "", nil, err
		}
		_ = res.Body.Close()
		text, sources, usedSearch := parseOpenAIWebSearchResponse(decoded, sourceLimit)
		if strings.TrimSpace(text) == "" {
			lastErr = fmt.Errorf("openai web search returned no text")
			continue
		}
		if !usedSearch {
			if isOfficialOpenAIEndpoint(endpoint.BaseURL) {
				return "", nil, errOpenAIWebSearchUnavailable
			}
			s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationWebSearch, openAIEndpointAdaptation{
				Kind: openAIAdapterResponses,
				URL:  target,
			})
			return text, sources, nil
		}
		s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationWebSearch, openAIEndpointAdaptation{
			Kind: openAIAdapterResponses,
			URL:  target,
		})
		return text, sources, nil
	}
	if lastErr != nil {
		return "", nil, lastErr
	}
	return "", nil, errOpenAIWebSearchUnavailable
}

func shouldTryNextOpenAIWebSearchTool(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errOpenAIWebSearchUnavailable) {
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

func buildOpenAIResponsesInput(messages []aiMessage) (string, []openAIResponsesInputMessage) {
	instructionParts := make([]string, 0, 2)
	input := make([]openAIResponsesInputMessage, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" && len(message.Attachments) == 0 {
			continue
		}
		switch normalizeProviderRole(message.Role) {
		case "system", "developer":
			if content != "" {
				instructionParts = append(instructionParts, content)
			}
		case "assistant":
			input = append(input, openAIResponsesInputMessage{Role: "assistant", Content: buildOpenAIResponsesContent(message)})
		default:
			input = append(input, openAIResponsesInputMessage{Role: "user", Content: buildOpenAIResponsesContent(message)})
		}
	}
	return strings.Join(instructionParts, "\n\n"), input
}

func parseOpenAIWebSearchResponse(decoded openAIResponsesResponse, sourceLimit int) (string, []store.SearchSource, bool) {
	textParts := make([]string, 0, 1)
	usedSearch := false
	candidates := make([]openAIWebSearchSource, 0)

	for _, item := range decoded.Output {
		if item.Type == "web_search_call" {
			usedSearch = true
			if item.Action != nil {
				candidates = append(candidates, item.Action.Sources...)
			}
			continue
		}
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Text != "" {
				textParts = append(textParts, content.Text)
			}
			for _, annotation := range content.Annotations {
				if annotation.Type == "url_citation" && strings.TrimSpace(annotation.URL) != "" {
					candidates = append(candidates, openAIWebSearchSource{
						URL:   annotation.URL,
						Title: annotation.Title,
					})
				}
			}
		}
	}

	text := strings.TrimSpace(strings.Join(textParts, "\n"))
	if text == "" {
		text = strings.TrimSpace(decoded.OutputText)
	}
	return text, openAIWebSearchSourcesToStoreSources(candidates, sourceLimit), usedSearch
}

func openAIWebSearchSourcesToStoreSources(candidates []openAIWebSearchSource, sourceLimit int) []store.SearchSource {
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
			Title:  title,
			URL:    sourceURL,
			Domain: parsed.Hostname(),
			Index:  len(sources) + 1,
		})
		seen[seenKey] = len(sources) - 1
		if len(sources) >= sourceLimit {
			break
		}
	}
	return sources
}

func isOfficialOpenAIEndpoint(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func openAIWebSearchEndpointOrder(endpoints []store.ModelEndpoint) []store.ModelEndpoint {
	official := make([]store.ModelEndpoint, 0, len(endpoints))
	compatible := make([]store.ModelEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.Active || strings.TrimSpace(endpoint.BaseURL) == "" || strings.TrimSpace(endpoint.Secret) == "" {
			continue
		}
		if isOfficialOpenAIEndpoint(endpoint.BaseURL) {
			official = append(official, endpoint)
			continue
		}
		compatible = append(compatible, endpoint)
	}
	return append(official, compatible...)
}
