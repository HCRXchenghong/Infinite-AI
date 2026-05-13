package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) proxyDeveloperChatCompletions(ctx context.Context, w http.ResponseWriter, routeSlug string, payload map[string]any) error {
	return s.proxyDeveloperOpenAICompatible(ctx, w, routeSlug, payload, "/chat/completions", openAIAdapterChatCompletions)
}

func (s *Server) proxyDeveloperResponses(ctx context.Context, w http.ResponseWriter, routeSlug string, payload map[string]any) error {
	return s.proxyDeveloperOpenAICompatible(ctx, w, routeSlug, payload, "/responses", openAIAdapterResponses)
}

func (s *Server) proxyDeveloperOpenAICompatible(ctx context.Context, w http.ResponseWriter, routeSlug string, payload map[string]any, endpointPath string, kind string) error {
	route, err := s.Store.FindActiveModelRoute(ctx, routeSlug, true)
	if err != nil {
		return err
	}
	if route.ModelType == "image" {
		return fmt.Errorf("model %s is configured for image generation, not chat completions", routeSlug)
	}
	if !developerRouteSupportsOpenAIProxy(route) {
		return fmt.Errorf("model %s does not support Infinite Code agent tool calls", routeSlug)
	}
	if len(route.Endpoints) == 0 {
		return fmt.Errorf("provider route %s is not configured", routeSlug)
	}
	payload["model"] = route.UpstreamModel
	body, _ := json.Marshal(payload)
	var lastErr error
	for _, endpoint := range route.Endpoints {
		if !endpoint.Active || endpoint.BaseURL == "" || endpoint.Secret == "" {
			continue
		}
		candidates := openAIEndpointCandidates(endpoint.BaseURL, endpointPath)
		if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok && cached.Kind == kind {
			candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, endpointPath, cached.URL)
		}
		for _, target := range candidates {
			err := s.proxyDeveloperOpenAICompatibleAt(ctx, w, route, endpoint, body, target)
			if err == nil {
				s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
					Kind: kind,
					URL:  target,
				})
				return nil
			}
			lastErr = err
			if candidateErr, ok := err.(*openAIChatCompletionsCandidateError); ok && candidateErr.CanNext {
				continue
			}
			return err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("openai-compatible upstream endpoint is not configured")
}

func (s *Server) proxyDeveloperOpenAICompatibleAt(ctx context.Context, w http.ResponseWriter, route *store.ModelRoute, endpoint store.ModelEndpoint, body []byte, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		bodyText := readUpstreamErrorBody(res.Body)
		err := newUpstreamHTTPError("openai-compatible agent proxy", res.StatusCode, bodyText)
		return &openAIChatCompletionsCandidateError{
			Err:     err,
			CanNext: shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText),
		}
	}
	copyDeveloperProxyHeaders(w, res.Header)
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
	return nil
}

func developerRouteSupportsOpenAIProxy(route *store.ModelRoute) bool {
	protocol := strings.ToLower(strings.TrimSpace(route.Protocol))
	return protocol == "" || protocol == "openai" || protocol == "openai-compatible"
}

func copyDeveloperProxyHeaders(w http.ResponseWriter, headers http.Header) {
	for key, values := range headers {
		lower := strings.ToLower(key)
		if lower == "connection" || lower == "content-length" || lower == "transfer-encoding" || lower == "content-encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}
