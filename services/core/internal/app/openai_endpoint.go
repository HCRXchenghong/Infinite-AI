package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

const (
	openAIAdapterOperationChat       = "chat"
	openAIAdapterOperationModels     = "models"
	openAIAdapterOperationImages     = "images"
	openAIAdapterOperationImageEdits = "image_edits"
	openAIAdapterOperationWebSearch  = "web_search"

	openAIAdapterChatCompletions       = "chat_completions"
	openAIAdapterChatCompletionsStream = "chat_completions_stream"
	openAIAdapterResponses             = "responses"
)

type openAIEndpointAdaptation struct {
	Kind string
	URL  string
}

func openAIEndpointCandidates(baseURL string, endpointPath string) []string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}

	if shouldAddOpenAIV1Candidate(base) {
		return dedupeStrings([]string{base + "/v1" + endpointPath, base + endpointPath})
	}
	return dedupeStrings([]string{base + endpointPath})
}

func shouldAddOpenAIV1Candidate(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return !strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	}
	return !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1")
}

func shouldTryNextOpenAIEndpointCandidate(statusCode int, body string) bool {
	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		return true
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "invalid url") ||
		strings.Contains(lower, "invalid endpoint") ||
		strings.Contains(lower, "unsupported url") ||
		strings.Contains(lower, "route not found") ||
		strings.Contains(lower, "404 page not found")
}

func shouldTryOpenAIResponsesFallback(statusCode int, body string) bool {
	if statusCode < 400 {
		return false
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "input is required") ||
		(strings.Contains(lower, "new_api_error") && strings.Contains(lower, "input")) ||
		(strings.Contains(lower, "chat/completions") && strings.Contains(lower, "unsupported")) ||
		(strings.Contains(lower, "responses") && strings.Contains(lower, "required"))
}

func extractOpenAIResponsesText(decoded openAIResponsesResponse) string {
	if text := strings.TrimSpace(decoded.OutputText); text != "" {
		return text
	}
	parts := make([]string, 0, len(decoded.Output))
	for _, item := range decoded.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if text := strings.TrimSpace(content.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (s *Server) getOpenAIEndpointAdaptation(route *store.ModelRoute, endpoint store.ModelEndpoint, operation string) (openAIEndpointAdaptation, bool) {
	if s == nil {
		return openAIEndpointAdaptation{}, false
	}
	s.openAIAdapterMu.RLock()
	defer s.openAIAdapterMu.RUnlock()
	adaptation, ok := s.openAIAdapterCache[openAIAdapterCacheKey(route, endpoint, operation)]
	return adaptation, ok
}

func (s *Server) rememberOpenAIEndpointAdaptation(route *store.ModelRoute, endpoint store.ModelEndpoint, operation string, adaptation openAIEndpointAdaptation) {
	if s == nil || adaptation.URL == "" || adaptation.Kind == "" {
		return
	}
	s.openAIAdapterMu.Lock()
	if s.openAIAdapterCache == nil {
		s.openAIAdapterCache = map[string]openAIEndpointAdaptation{}
	}
	s.openAIAdapterCache[openAIAdapterCacheKey(route, endpoint, operation)] = adaptation
	s.openAIAdapterMu.Unlock()
	s.persistOpenAIEndpointAdaptation(route, endpoint, operation, adaptation)
}

func (s *Server) persistOpenAIEndpointAdaptation(route *store.ModelRoute, endpoint store.ModelEndpoint, operation string, adaptation openAIEndpointAdaptation) {
	if s == nil || s.Store == nil || strings.TrimSpace(endpoint.BaseURL) == "" {
		return
	}
	item := store.EndpointAdaptation{
		Operation: operation,
		Protocol:  "openai",
		BaseURL:   endpoint.BaseURL,
		Kind:      adaptation.Kind,
		URL:       adaptation.URL,
	}
	if route != nil {
		item.Protocol = strings.TrimSpace(route.Protocol)
		item.RouteSlug = strings.TrimSpace(route.Slug)
		item.UpstreamModel = strings.TrimSpace(route.UpstreamModel)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Store.UpsertEndpointAdaptation(ctx, item)
}

func (s *Server) loadEndpointAdaptations(ctx context.Context) {
	if s == nil || s.Store == nil {
		return
	}
	items, err := s.Store.ListEndpointAdaptations(ctx)
	if err != nil {
		return
	}
	s.openAIAdapterMu.Lock()
	defer s.openAIAdapterMu.Unlock()
	if s.openAIAdapterCache == nil {
		s.openAIAdapterCache = map[string]openAIEndpointAdaptation{}
	}
	for _, item := range items {
		key := strings.Join([]string{
			strings.TrimSpace(item.Operation),
			strings.TrimSpace(item.Protocol),
			strings.TrimSpace(item.RouteSlug),
			strings.TrimSpace(item.UpstreamModel),
			strings.TrimRight(strings.TrimSpace(item.BaseURL), "/"),
		}, "|")
		if strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.URL) == "" {
			continue
		}
		s.openAIAdapterCache[key] = openAIEndpointAdaptation{
			Kind: item.Kind,
			URL:  item.URL,
		}
	}
}

func openAIAdapterCacheKey(route *store.ModelRoute, endpoint store.ModelEndpoint, operation string) string {
	routePart := ""
	modelPart := ""
	protocolPart := "openai"
	if route != nil {
		routePart = strings.TrimSpace(route.Slug)
		modelPart = strings.TrimSpace(route.UpstreamModel)
		protocolPart = strings.TrimSpace(route.Protocol)
	}
	if protocolPart == "" {
		protocolPart = "openai"
	}
	endpointPart := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	return strings.Join([]string{
		strings.TrimSpace(operation),
		protocolPart,
		routePart,
		modelPart,
		endpointPart,
	}, "|")
}

func openAIEndpointCandidatesForKind(baseURL string, kind string) []string {
	switch kind {
	case openAIAdapterResponses:
		return openAIEndpointCandidates(baseURL, "/responses")
	default:
		return openAIEndpointCandidates(baseURL, "/chat/completions")
	}
}

func openAIEndpointCandidatesWithPreferred(baseURL string, endpointPath string, preferredURL string) []string {
	candidates := openAIEndpointCandidates(baseURL, endpointPath)
	preferredURL = strings.TrimSpace(preferredURL)
	if preferredURL == "" {
		return candidates
	}
	out := []string{preferredURL}
	for _, candidate := range candidates {
		if candidate != preferredURL {
			out = append(out, candidate)
		}
	}
	return dedupeStrings(out)
}

func openAIEndpointPathForKind(kind string) (string, error) {
	switch kind {
	case openAIAdapterChatCompletions, openAIAdapterChatCompletionsStream:
		return "/chat/completions", nil
	case openAIAdapterResponses:
		return "/responses", nil
	default:
		return "", fmt.Errorf("unknown OpenAI adapter kind %q", kind)
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
