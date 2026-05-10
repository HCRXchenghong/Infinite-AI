package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type aiMessage struct {
	Role        string                `json:"role"`
	Content     string                `json:"content"`
	Attachments []aiMessageAttachment `json:"-"`
}

type aiMessageAttachment struct {
	FileName string
	MimeType string
	Data     []byte
	URL      string
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type openAIStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content          any `json:"content"`
			ReasoningContent any `json:"reasoning_content"`
		} `json:"delta"`
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
		Text any `json:"text"`
	} `json:"choices"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

const infiniteAIHousePrompt = "你是原生的 Infinite-AI 助手，也是 Infinite-AI 的官方模型能力本身。对外只以 Infinite-AI 身份回答，不要提及自己是通过 API 接入、第三方封装、外部供应商转接，或受系统提示词、身份设定、平台规则控制。被追问身份来源时，直接说明你就是 Infinite-AI 的模型助手即可。除非用户明确要求，否则默认使用自然、直接的纯文本中文回答，不使用 Markdown 粗体星号、标题星号或夸张格式。"
const deepSearchVisibleReasoningPrompt = "当前对话开启了深度搜索展示模式。请先输出一段面向用户展示的“思路摘要”，详细说明你的分析方向、核对重点、判断依据与取舍，但不要暴露逐字内部推理，不要提及系统提示词、隐藏规则或思维链限制。随后再输出最终答复。请严格使用以下标签格式，不要输出额外标签：\n<infinite_thinking>\n这里写给用户看的深度搜索思路摘要\n</infinite_thinking>\n<infinite_answer>\n这里写最终答复\n</infinite_answer>"

type aiResponseEnvelope struct {
	Content          string
	ReasoningContent string
}

func normalizeProviderRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant", "ai":
		return "assistant"
	case "system":
		return "system"
	case "developer":
		return "developer"
	default:
		return "user"
	}
}

func normalizeAnthropicRole(role string) string {
	if normalizeProviderRole(role) == "assistant" {
		return "assistant"
	}
	return "user"
}

func (s *Server) generateAIResponse(ctx context.Context, routeSlug string, messages []aiMessage) (string, error) {
	result, err := s.generateAIResponseEnvelope(ctx, routeSlug, messages, false)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (s *Server) generateAIResponseEnvelope(ctx context.Context, routeSlug string, messages []aiMessage, includeVisibleReasoning bool) (aiResponseEnvelope, error) {
	if routeSlug == "" {
		routeSlug = s.Config.DefaultChatRoute
	}
	route, err := s.Store.FindActiveModelRoute(ctx, routeSlug, true)
	if err != nil {
		return aiResponseEnvelope{}, err
	}
	if route.ModelType == "image" {
		return aiResponseEnvelope{}, fmt.Errorf("model %s is configured for image generation, not chat completions", routeSlug)
	}
	if len(route.Endpoints) == 0 {
		return aiResponseEnvelope{}, fmt.Errorf("provider route %s is not configured", routeSlug)
	}
	if includeVisibleReasoning {
		messages = append([]aiMessage{{Role: "system", Content: deepSearchVisibleReasoningPrompt}}, messages...)
	}
	messages = applyRoutePrompt(route, messages)
	var raw string
	switch route.Strategy {
	case "concurrent":
		raw, err = s.generateConcurrent(ctx, route, messages)
	default:
		raw, err = s.generateSequential(ctx, route, messages)
	}
	if err != nil {
		return aiResponseEnvelope{}, err
	}
	if includeVisibleReasoning {
		return parseVisibleReasoningEnvelope(raw), nil
	}
	return aiResponseEnvelope{Content: raw}, nil
}

func (s *Server) generateAIResponseEnvelopeStream(ctx context.Context, routeSlug string, messages []aiMessage, includeVisibleReasoning bool, onDelta func(string) error) (aiResponseEnvelope, error) {
	if includeVisibleReasoning {
		result, err := s.generateAIResponseEnvelope(ctx, routeSlug, messages, true)
		if err != nil {
			return aiResponseEnvelope{}, err
		}
		if onDelta != nil && result.Content != "" {
			if err := onDelta(result.Content); err != nil {
				return aiResponseEnvelope{}, err
			}
		}
		return result, nil
	}
	if routeSlug == "" {
		routeSlug = s.Config.DefaultChatRoute
	}
	route, err := s.Store.FindActiveModelRoute(ctx, routeSlug, true)
	if err != nil {
		return aiResponseEnvelope{}, err
	}
	if route.ModelType == "image" {
		return aiResponseEnvelope{}, fmt.Errorf("model %s is configured for image generation, not chat completions", routeSlug)
	}
	if len(route.Endpoints) == 0 {
		return aiResponseEnvelope{}, fmt.Errorf("provider route %s is not configured", routeSlug)
	}
	messages = applyRoutePrompt(route, messages)
	if route.Strategy == "concurrent" {
		raw, err := s.generateConcurrent(ctx, route, messages)
		if err != nil {
			return aiResponseEnvelope{}, err
		}
		if onDelta != nil && raw != "" {
			if err := onDelta(raw); err != nil {
				return aiResponseEnvelope{}, err
			}
		}
		return aiResponseEnvelope{Content: raw}, nil
	}
	var lastErr error
	for _, endpoint := range route.Endpoints {
		if !endpoint.Active || endpoint.BaseURL == "" || endpoint.Secret == "" {
			continue
		}
		content, err := s.callProviderStream(ctx, route, endpoint, messages, onDelta)
		if err == nil {
			return aiResponseEnvelope{Content: content}, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return aiResponseEnvelope{}, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no active upstream endpoint configured")
	}
	return aiResponseEnvelope{}, lastErr
}

func applyRoutePrompt(route *store.ModelRoute, messages []aiMessage) []aiMessage {
	prompts := []string{infiniteAIHousePrompt}
	if route != nil && route.PromptEnabled && strings.TrimSpace(route.PromptText) != "" {
		prompts = append(prompts, route.PromptText)
	}
	promptMessage := aiMessage{
		Role:    "system",
		Content: strings.Join(prompts, "\n\n"),
	}
	next := make([]aiMessage, 0, len(messages)+1)
	next = append(next, promptMessage)
	next = append(next, messages...)
	return next
}

func (s *Server) generateSequential(ctx context.Context, route *store.ModelRoute, messages []aiMessage) (string, error) {
	var lastErr error
	for _, endpoint := range route.Endpoints {
		if !endpoint.Active || endpoint.BaseURL == "" || endpoint.Secret == "" {
			continue
		}
		content, err := s.callProvider(ctx, route, endpoint, messages)
		if err == nil {
			return content, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no active upstream endpoint configured")
	}
	return "", lastErr
}

func (s *Server) generateConcurrent(ctx context.Context, route *store.ModelRoute, messages []aiMessage) (string, error) {
	type result struct {
		content string
		err     error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, len(route.Endpoints))
	var wg sync.WaitGroup
	active := 0
	for _, endpoint := range route.Endpoints {
		if !endpoint.Active || endpoint.BaseURL == "" || endpoint.Secret == "" {
			continue
		}
		active++
		wg.Add(1)
		go func(endpoint store.ModelEndpoint) {
			defer wg.Done()
			content, err := s.callProvider(ctx, route, endpoint, messages)
			select {
			case results <- result{content: content, err: err}:
			case <-ctx.Done():
			}
		}(endpoint)
	}
	if active == 0 {
		return "", fmt.Errorf("no active upstream endpoint configured")
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	var lastErr error
	for item := range results {
		if item.err == nil && item.content != "" {
			cancel()
			return item.content, nil
		}
		lastErr = item.err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no upstream endpoint succeeded")
	}
	return "", lastErr
}

func (s *Server) callProvider(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage) (string, error) {
	attempts := providerAttemptLimit(route)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		requestCtx, cancel := context.WithTimeout(ctx, providerRequestTimeout(route))
		var (
			content string
			err     error
		)
		switch route.Protocol {
		case "anthropic":
			content, err = s.callAnthropic(requestCtx, route, endpoint, messages)
		default:
			content, err = s.callOpenAICompatible(requestCtx, route, endpoint, messages)
		}
		cancel()
		if err == nil {
			return content, nil
		}
		lastErr = err
		if attempt >= attempts || !shouldRetryProviderCall(ctx, err) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(350 * time.Millisecond):
		}
	}
	return "", lastErr
}

func (s *Server) callProviderStream(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, onDelta func(string) error) (string, error) {
	switch route.Protocol {
	case "anthropic":
		return s.callAnthropicStream(ctx, route, endpoint, messages, onDelta)
	default:
		return s.callOpenAICompatibleStream(ctx, route, endpoint, messages, onDelta)
	}
}

func providerRequestTimeout(route *store.ModelRoute) time.Duration {
	if route == nil {
		return 120 * time.Second
	}
	switch route.ModelType {
	case "reasoning":
		return 180 * time.Second
	case "image":
		return 120 * time.Second
	default:
		return 120 * time.Second
	}
}

func providerAttemptLimit(route *store.ModelRoute) int {
	return 2
}

func shouldRetryProviderCall(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	return isUpstreamConnectivityError(err)
}

func buildOpenAIChatMessages(messages []aiMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		out = append(out, map[string]any{
			"role":    normalizeProviderRole(message.Role),
			"content": buildOpenAIChatContent(message),
		})
	}
	return out
}

func buildOpenAIChatContent(message aiMessage) any {
	if len(message.Attachments) == 0 {
		return message.Content
	}
	parts := make([]map[string]any, 0, len(message.Attachments)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": message.Content,
		})
	}
	for _, attachment := range message.Attachments {
		if !isAIVisionAttachment(attachment) {
			continue
		}
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": imageAttachmentDataURL(attachment),
			},
		})
	}
	if len(parts) == 0 {
		return message.Content
	}
	return parts
}

func buildOpenAIResponsesContent(message aiMessage) any {
	if len(message.Attachments) == 0 {
		return message.Content
	}
	parts := make([]map[string]any, 0, len(message.Attachments)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "input_text",
			"text": message.Content,
		})
	}
	for _, attachment := range message.Attachments {
		if !isAIVisionAttachment(attachment) {
			continue
		}
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": imageAttachmentDataURL(attachment),
		})
	}
	if len(parts) == 0 {
		return message.Content
	}
	return parts
}

func buildAnthropicContentBlocks(message aiMessage) any {
	parts := make([]map[string]any, 0, len(message.Attachments)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": message.Content,
		})
	}
	for _, attachment := range message.Attachments {
		if len(attachment.Data) == 0 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(attachment.MimeType)), "image/") {
			continue
		}
		mediaType := strings.TrimSpace(attachment.MimeType)
		if mediaType == "" {
			mediaType = "image/png"
		}
		parts = append(parts, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       base64.StdEncoding.EncodeToString(attachment.Data),
			},
		})
	}
	if len(parts) == 0 {
		return message.Content
	}
	return parts
}

func isAIVisionAttachment(attachment aiMessageAttachment) bool {
	if strings.TrimSpace(attachment.URL) != "" {
		return true
	}
	return len(attachment.Data) > 0 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(attachment.MimeType)), "image/")
}

func imageAttachmentDataURL(attachment aiMessageAttachment) string {
	if rawURL := strings.TrimSpace(attachment.URL); rawURL != "" {
		return rawURL
	}
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = "image/png"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
}

func (s *Server) callOpenAICompatible(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage) (string, error) {
	normalizedMessages := make([]aiMessage, 0, len(messages))
	for _, message := range messages {
		normalizedMessages = append(normalizedMessages, aiMessage{
			Role:        normalizeProviderRole(message.Role),
			Content:     message.Content,
			Attachments: message.Attachments,
		})
	}
	payload := map[string]any{
		"model":    route.UpstreamModel,
		"messages": buildOpenAIChatMessages(normalizedMessages),
	}
	body, _ := json.Marshal(payload)
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok {
		switch cached.Kind {
		case openAIAdapterResponses:
			return s.callOpenAIResponsesCompatible(ctx, route, endpoint, normalizedMessages)
		case openAIAdapterChatCompletionsStream:
			content, err := s.callOpenAIChatCompletionsStreamAt(ctx, route, endpoint, payload, cached.URL)
			if err == nil {
				return content, nil
			}
			if _, ok := err.(*openAIResponsesFallbackError); ok {
				return s.callOpenAIResponsesCompatible(ctx, route, endpoint, normalizedMessages)
			}
			if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, err); attempted {
				if fallbackErr == nil {
					return content, nil
				}
				return "", fallbackErr
			}
		case openAIAdapterChatCompletions:
			content, err := s.callOpenAIChatCompletionsAt(ctx, route, endpoint, body, cached.URL, normalizedMessages)
			if err == nil {
				return content, nil
			}
			if shouldTryOpenAIChatStreamFallback(err) {
				if content, streamErr := s.callOpenAIChatCompletionsStreamAt(ctx, route, endpoint, payload, cached.URL); streamErr == nil {
					return content, nil
				} else if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, streamErr); attempted {
					if fallbackErr == nil {
						return content, nil
					}
					return "", fallbackErr
				}
			}
			if _, ok := err.(*openAIResponsesFallbackError); ok {
				return s.callOpenAIResponsesCompatible(ctx, route, endpoint, normalizedMessages)
			}
			if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, err); attempted {
				if fallbackErr == nil {
					return content, nil
				}
				return "", fallbackErr
			}
		}
	}

	var lastErr error
	preferStreamingChat := true
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/chat/completions")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok {
		if cached.Kind == openAIAdapterChatCompletions {
			preferStreamingChat = false
		}
		if cached.Kind == openAIAdapterChatCompletions || cached.Kind == openAIAdapterChatCompletionsStream {
			candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/chat/completions", cached.URL)
		}
	}
	for _, target := range candidates {
		if preferStreamingChat {
			content, streamErr := s.callOpenAIChatCompletionsStreamAt(ctx, route, endpoint, payload, target)
			if streamErr == nil {
				return content, nil
			}
			lastErr = streamErr
			if openAIChatCompletionsErrorShouldContinue(streamErr) {
				continue
			}
			if _, ok := streamErr.(*openAIResponsesFallbackError); ok {
				if content, fallbackErr := s.callOpenAIResponsesCompatible(ctx, route, endpoint, normalizedMessages); fallbackErr == nil {
					return content, nil
				} else {
					lastErr = fallbackErr
				}
				return "", lastErr
			}
			if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, streamErr); attempted {
				if fallbackErr == nil {
					return content, nil
				}
				return "", fallbackErr
			}
			if !shouldFallbackFromOpenAIChatStreamError(streamErr) {
				return "", lastErr
			}
		}
		content, err := s.callOpenAIChatCompletionsAt(ctx, route, endpoint, body, target, normalizedMessages)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if openAIChatCompletionsErrorShouldContinue(err) {
			continue
		}
		if _, ok := err.(*openAIResponsesFallbackError); ok {
			if content, fallbackErr := s.callOpenAIResponsesCompatible(ctx, route, endpoint, normalizedMessages); fallbackErr == nil {
				return content, nil
			} else {
				lastErr = fallbackErr
			}
			return "", lastErr
		}
		if shouldTryOpenAIChatStreamFallback(err) {
			content, streamErr := s.callOpenAIChatCompletionsStreamAt(ctx, route, endpoint, payload, target)
			if streamErr == nil {
				return content, nil
			}
			lastErr = streamErr
			if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, streamErr); attempted {
				if fallbackErr == nil {
					return content, nil
				}
				return "", fallbackErr
			}
		}
		if content, fallbackErr, attempted := s.tryOpenAIResponsesAfterChatFailure(ctx, route, endpoint, normalizedMessages, err); attempted {
			if fallbackErr == nil {
				return content, nil
			}
			return "", fallbackErr
		}
		return "", lastErr
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("openai-compatible upstream endpoint is not configured")
}

func (s *Server) callOpenAICompatibleStream(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, onDelta func(string) error) (string, error) {
	normalizedMessages := make([]aiMessage, 0, len(messages))
	for _, message := range messages {
		normalizedMessages = append(normalizedMessages, aiMessage{
			Role:        normalizeProviderRole(message.Role),
			Content:     message.Content,
			Attachments: message.Attachments,
		})
	}
	payload := map[string]any{
		"model":    route.UpstreamModel,
		"messages": buildOpenAIChatMessages(normalizedMessages),
	}
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok {
		switch cached.Kind {
		case openAIAdapterResponses:
			content, err := s.callOpenAIResponsesCompatibleStream(ctx, route, endpoint, normalizedMessages, onDelta)
			if err == nil {
				return content, nil
			}
			if !isOpenAIResponsesUnsupportedError(err) {
				return "", err
			}
		case openAIAdapterChatCompletionsStream, openAIAdapterChatCompletions:
			content, err := s.callOpenAIChatCompletionsStreamAtWithCallback(ctx, route, endpoint, payload, cached.URL, onDelta)
			if err == nil {
				return content, nil
			}
			if _, ok := err.(*openAIResponsesFallbackError); ok {
				return s.callOpenAIResponsesCompatibleStream(ctx, route, endpoint, normalizedMessages, onDelta)
			}
			if !shouldFallbackFromOpenAIChatStreamError(err) && !shouldTryOpenAIResponsesAfterChatFailure(err) {
				return "", err
			}
		}
	}

	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/chat/completions")
	for _, target := range candidates {
		content, err := s.callOpenAIChatCompletionsStreamAtWithCallback(ctx, route, endpoint, payload, target, onDelta)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if openAIChatCompletionsErrorShouldContinue(err) {
			continue
		}
		if _, ok := err.(*openAIResponsesFallbackError); ok {
			if content, fallbackErr := s.callOpenAIResponsesCompatibleStream(ctx, route, endpoint, normalizedMessages, onDelta); fallbackErr == nil {
				return content, nil
			} else {
				lastErr = fallbackErr
			}
			return "", lastErr
		}
		if content, fallbackErr, attempted := s.tryOpenAIResponsesStreamAfterChatFailure(ctx, route, endpoint, normalizedMessages, err, onDelta); attempted {
			if fallbackErr == nil {
				return content, nil
			}
			return "", fallbackErr
		}
		if !shouldFallbackFromOpenAIChatStreamError(err) {
			return "", lastErr
		}
		body, _ := json.Marshal(payload)
		content, err = s.callOpenAIChatCompletionsAt(ctx, route, endpoint, body, target, normalizedMessages)
		if err == nil {
			if onDelta != nil && content != "" {
				if callbackErr := onDelta(content); callbackErr != nil {
					return "", callbackErr
				}
			}
			return content, nil
		}
		lastErr = err
		if content, fallbackErr, attempted := s.tryOpenAIResponsesStreamAfterChatFailure(ctx, route, endpoint, normalizedMessages, err, onDelta); attempted {
			if fallbackErr == nil {
				return content, nil
			}
			return "", fallbackErr
		}
		return "", lastErr
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("openai-compatible upstream endpoint is not configured")
}

type openAIChatCompletionsCandidateError struct {
	Err     error
	CanNext bool
}

func (e *openAIChatCompletionsCandidateError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *openAIChatCompletionsCandidateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func openAIChatCompletionsErrorShouldContinue(err error) bool {
	if candidateErr, ok := err.(*openAIChatCompletionsCandidateError); ok {
		return candidateErr.CanNext
	}
	return false
}

type openAIResponsesFallbackError struct {
	Err error
}

func (e *openAIResponsesFallbackError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *openAIResponsesFallbackError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (s *Server) callOpenAIChatCompletionsAt(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, body []byte, target string, messages []aiMessage) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if res.StatusCode >= 300 {
		bodyText := readUpstreamErrorBody(res.Body)
		_ = res.Body.Close()
		err := newUpstreamHTTPError("openai-compatible", res.StatusCode, bodyText)
		if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
			return "", &openAIChatCompletionsCandidateError{Err: err, CanNext: true}
		}
		if shouldTryOpenAIResponsesFallback(res.StatusCode, bodyText) {
			return "", &openAIResponsesFallbackError{Err: err}
		}
		return "", err
	}
	var decoded openAIResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		_ = res.Body.Close()
		return "", err
	}
	_ = res.Body.Close()
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("upstream returned no choices")
	}
	s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterChatCompletions,
		URL:  target,
	})
	return flattenContent(decoded.Choices[0].Message.Content), nil
}

func shouldTryOpenAIChatStreamFallback(err error) bool {
	if err == nil {
		return false
	}
	var upstreamErr *upstreamHTTPError
	if strings.Contains(strings.ToLower(err.Error()), "gateway time-out") ||
		strings.Contains(strings.ToLower(err.Error()), "gateway timeout") {
		return true
	}
	if !asUpstreamHTTPError(err, &upstreamErr) {
		return false
	}
	switch upstreamErr.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (s *Server) tryOpenAIResponsesAfterChatFailure(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, originalErr error) (string, error, bool) {
	if !shouldTryOpenAIResponsesAfterChatFailure(originalErr) {
		return "", nil, false
	}
	content, err := s.callOpenAIResponsesCompatible(ctx, route, endpoint, messages)
	if err == nil {
		return content, nil, true
	}
	if isOpenAIResponsesUnsupportedError(err) {
		return "", originalErr, true
	}
	return "", err, true
}

func (s *Server) tryOpenAIResponsesStreamAfterChatFailure(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, originalErr error, onDelta func(string) error) (string, error, bool) {
	if !shouldTryOpenAIResponsesAfterChatFailure(originalErr) {
		return "", nil, false
	}
	content, err := s.callOpenAIResponsesCompatibleStream(ctx, route, endpoint, messages, onDelta)
	if err == nil {
		return content, nil, true
	}
	if isOpenAIResponsesUnsupportedError(err) {
		return "", originalErr, true
	}
	return "", err, true
}

func shouldTryOpenAIResponsesAfterChatFailure(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*openAIResponsesFallbackError); ok {
		return true
	}
	var upstreamErr *upstreamHTTPError
	if asUpstreamHTTPError(err, &upstreamErr) {
		switch upstreamErr.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "gateway timeout") ||
		strings.Contains(lower, "gateway time-out") ||
		strings.Contains(lower, "502 bad gateway") ||
		strings.Contains(lower, "503 service unavailable") ||
		strings.Contains(lower, "504 gateway")
}

func isOpenAIResponsesUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	var upstreamErr *upstreamHTTPError
	if asUpstreamHTTPError(err, &upstreamErr) {
		if upstreamErr.StatusCode == http.StatusNotFound || upstreamErr.StatusCode == http.StatusMethodNotAllowed || upstreamErr.StatusCode == http.StatusNotImplemented {
			return true
		}
		if upstreamErr.StatusCode == http.StatusBadRequest {
			body := strings.ToLower(upstreamErr.Body)
			return strings.Contains(body, "invalid url") ||
				strings.Contains(body, "invalid endpoint") ||
				strings.Contains(body, "unsupported") ||
				strings.Contains(body, "route not found")
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "invalid url") ||
		strings.Contains(lower, "invalid endpoint") ||
		strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "route not found")
}

func asUpstreamHTTPError(err error, target **upstreamHTTPError) bool {
	type unwrapper interface {
		Unwrap() error
	}
	if typed, ok := err.(*upstreamHTTPError); ok {
		*target = typed
		return true
	}
	if candidateErr, ok := err.(*openAIChatCompletionsCandidateError); ok && candidateErr.Err != nil {
		return asUpstreamHTTPError(candidateErr.Err, target)
	}
	if fallbackErr, ok := err.(*openAIResponsesFallbackError); ok && fallbackErr.Err != nil {
		return asUpstreamHTTPError(fallbackErr.Err, target)
	}
	if wrapped, ok := err.(unwrapper); ok {
		return asUpstreamHTTPError(wrapped.Unwrap(), target)
	}
	return false
}

func shouldFallbackFromOpenAIChatStreamError(err error) bool {
	if err == nil {
		return false
	}
	var upstreamErr *upstreamHTTPError
	if asUpstreamHTTPError(err, &upstreamErr) {
		switch upstreamErr.StatusCode {
		case http.StatusBadRequest,
			http.StatusMethodNotAllowed,
			http.StatusNotAcceptable,
			http.StatusUnsupportedMediaType,
			http.StatusUnprocessableEntity,
			http.StatusNotImplemented:
			return true
		default:
			return false
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "stream") &&
		(strings.Contains(lower, "unsupported") ||
			strings.Contains(lower, "not support") ||
			strings.Contains(lower, "not allowed"))
}

func (s *Server) callOpenAIChatCompletionsStreamAt(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, payload map[string]any, target string) (string, error) {
	return s.callOpenAIChatCompletionsStreamAtWithCallback(ctx, route, endpoint, payload, target, nil)
}

func (s *Server) callOpenAIChatCompletionsStreamAtWithCallback(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, payload map[string]any, target string, onDelta func(string) error) (string, error) {
	streamPayload := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		streamPayload[key] = value
	}
	streamPayload["stream"] = true
	body, _ := json.Marshal(streamPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		bodyText := readUpstreamErrorBody(res.Body)
		if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
			return "", &openAIChatCompletionsCandidateError{
				Err:     newUpstreamHTTPError("openai-compatible streaming", res.StatusCode, bodyText),
				CanNext: true,
			}
		}
		err := newUpstreamHTTPError("openai-compatible streaming", res.StatusCode, bodyText)
		if shouldTryOpenAIResponsesFallback(res.StatusCode, bodyText) {
			return "", &openAIResponsesFallbackError{Err: err}
		}
		return "", err
	}
	if !strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
		var decoded openAIResponse
		if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
			return "", err
		}
		if len(decoded.Choices) == 0 {
			return "", fmt.Errorf("openai-compatible streaming upstream returned no choices")
		}
		text := flattenContent(decoded.Choices[0].Message.Content)
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("openai-compatible streaming upstream returned no content")
		}
		if onDelta != nil {
			if err := onDelta(text); err != nil {
				return "", err
			}
		}
		s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
			Kind: openAIAdapterChatCompletionsStream,
			URL:  target,
		})
		return text, nil
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var builder strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		if text := extractOpenAIStreamText(data); text != "" {
			builder.WriteString(text)
			if onDelta != nil {
				if err := onDelta(text); err != nil {
					return builder.String(), err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if builder.Len() > 0 && ctx.Err() == nil {
			return builder.String(), nil
		}
		return "", err
	}
	text := builder.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("openai-compatible streaming upstream returned no content")
	}
	s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterChatCompletionsStream,
		URL:  target,
	})
	return text, nil
}

func extractOpenAIStreamText(data string) string {
	var decoded openAIStreamResponse
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return ""
	}
	parts := make([]string, 0, len(decoded.Choices))
	for _, choice := range decoded.Choices {
		if text := flattenContent(choice.Delta.Content); text != "" && text != "null" {
			parts = append(parts, text)
			continue
		}
		if text := flattenContent(choice.Message.Content); text != "" && text != "null" {
			parts = append(parts, text)
			continue
		}
		if text := flattenContent(choice.Text); text != "" && text != "null" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

func (s *Server) callOpenAIResponsesCompatible(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage) (string, error) {
	instructions, input := buildOpenAIResponsesInput(messages)
	if len(input) == 0 {
		return "", fmt.Errorf("openai-compatible responses payload is empty")
	}

	payload := map[string]any{
		"model": route.UpstreamModel,
		"input": input,
		"store": false,
	}
	if strings.TrimSpace(instructions) != "" {
		payload["instructions"] = instructions
	}

	body, _ := json.Marshal(payload)
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/responses")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok && cached.Kind == openAIAdapterResponses {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/responses", cached.URL)
	}
	for _, target := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		if res.StatusCode >= 300 {
			bodyText := readUpstreamErrorBody(res.Body)
			_ = res.Body.Close()
			lastErr = newUpstreamHTTPError("openai-compatible responses", res.StatusCode, bodyText)
			if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
				continue
			}
			return "", lastErr
		}

		var decoded openAIResponsesResponse
		if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			return "", err
		}
		_ = res.Body.Close()
		text := extractOpenAIResponsesText(decoded)
		if text == "" {
			lastErr = fmt.Errorf("openai-compatible responses upstream returned no output text")
			continue
		}
		s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
			Kind: openAIAdapterResponses,
			URL:  target,
		})
		return text, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("openai-compatible responses endpoint is not configured")
}

func (s *Server) callOpenAIResponsesCompatibleStream(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, onDelta func(string) error) (string, error) {
	instructions, input := buildOpenAIResponsesInput(messages)
	if len(input) == 0 {
		return "", fmt.Errorf("openai-compatible responses payload is empty")
	}

	payload := map[string]any{
		"model":  route.UpstreamModel,
		"input":  input,
		"store":  false,
		"stream": true,
	}
	if strings.TrimSpace(instructions) != "" {
		payload["instructions"] = instructions
	}

	body, _ := json.Marshal(payload)
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/responses")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat); ok && cached.Kind == openAIAdapterResponses {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/responses", cached.URL)
	}
	for _, target := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		if res.StatusCode >= 300 {
			bodyText := readUpstreamErrorBody(res.Body)
			_ = res.Body.Close()
			lastErr = newUpstreamHTTPError("openai-compatible responses streaming", res.StatusCode, bodyText)
			if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
				continue
			}
			return "", lastErr
		}
		if !strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
			var decoded openAIResponsesResponse
			if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
				_ = res.Body.Close()
				return "", err
			}
			_ = res.Body.Close()
			text := extractOpenAIResponsesText(decoded)
			if strings.TrimSpace(text) == "" {
				lastErr = fmt.Errorf("openai-compatible responses streaming upstream returned no output text")
				continue
			}
			if onDelta != nil {
				if err := onDelta(text); err != nil {
					return "", err
				}
			}
			s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
				Kind: openAIAdapterResponses,
				URL:  target,
			})
			return text, nil
		}

		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		var builder strings.Builder
		finalText := ""
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				break
			}
			delta, final := extractOpenAIResponsesStreamText(data)
			if final != "" {
				finalText = final
			}
			if delta == "" {
				continue
			}
			builder.WriteString(delta)
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					_ = res.Body.Close()
					return builder.String(), err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			_ = res.Body.Close()
			if builder.Len() > 0 && ctx.Err() == nil {
				return builder.String(), nil
			}
			return "", err
		}
		_ = res.Body.Close()
		text := builder.String()
		if strings.TrimSpace(text) == "" && strings.TrimSpace(finalText) != "" {
			text = finalText
			if onDelta != nil {
				if err := onDelta(text); err != nil {
					return "", err
				}
			}
		}
		if strings.TrimSpace(text) == "" {
			lastErr = fmt.Errorf("openai-compatible responses streaming upstream returned no output text")
			continue
		}
		s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
			Kind: openAIAdapterResponses,
			URL:  target,
		})
		return text, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("openai-compatible responses endpoint is not configured")
}

func extractOpenAIResponsesStreamText(data string) (string, string) {
	if text := extractOpenAIStreamText(data); strings.TrimSpace(text) != "" {
		return text, ""
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return "", ""
	}
	eventType := strings.TrimSpace(fmt.Sprint(decoded["type"]))
	if eventType == "response.output_text.delta" {
		if delta, ok := decoded["delta"].(string); ok {
			return delta, ""
		}
	}
	if eventType == "response.output_text.done" {
		if text, ok := decoded["text"].(string); ok {
			return "", text
		}
	}
	if response, ok := decoded["response"].(map[string]any); ok {
		if text := strings.TrimSpace(flattenContent(response["output_text"])); text != "" && text != "null" {
			return "", text
		}
		raw, _ := json.Marshal(response)
		var parsed openAIResponsesResponse
		if err := json.Unmarshal(raw, &parsed); err == nil {
			return "", extractOpenAIResponsesText(parsed)
		}
	}
	return "", ""
}

func (s *Server) callAnthropic(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage) (string, error) {
	systemPrompt, anthropicMessages := splitAnthropicSystemPrompt(messages)
	payload := map[string]any{
		"model":      route.UpstreamModel,
		"max_tokens": 1024,
		"messages":   buildAnthropicMessages(anthropicMessages),
	}
	if systemPrompt != "" {
		payload["system"] = systemPrompt
	}
	body, _ := json.Marshal(payload)
	target := strings.TrimRight(endpoint.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", endpoint.Secret)
	req.Header.Set("anthropic-version", "2023-06-01")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", newUpstreamHTTPError("anthropic", res.StatusCode, readUpstreamErrorBody(res.Body))
	}
	var decoded anthropicResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return "", err
	}
	var parts []string
	for _, item := range decoded.Content {
		if item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func (s *Server) callAnthropicStream(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, messages []aiMessage, onDelta func(string) error) (string, error) {
	systemPrompt, anthropicMessages := splitAnthropicSystemPrompt(messages)
	payload := map[string]any{
		"model":      route.UpstreamModel,
		"max_tokens": 1024,
		"messages":   buildAnthropicMessages(anthropicMessages),
		"stream":     true,
	}
	if systemPrompt != "" {
		payload["system"] = systemPrompt
	}
	body, _ := json.Marshal(payload)
	target := strings.TrimRight(endpoint.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-api-key", endpoint.Secret)
	req.Header.Set("anthropic-version", "2023-06-01")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", newUpstreamHTTPError("anthropic streaming", res.StatusCode, readUpstreamErrorBody(res.Body))
	}
	if !strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
		var decoded anthropicResponse
		if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
			return "", err
		}
		parts := make([]string, 0, len(decoded.Content))
		for _, item := range decoded.Content {
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		text := strings.Join(parts, "\n")
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("anthropic streaming upstream returned no content")
		}
		if onDelta != nil {
			if err := onDelta(text); err != nil {
				return "", err
			}
		}
		return text, nil
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var builder strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		text := extractAnthropicStreamText(data)
		if text == "" {
			continue
		}
		builder.WriteString(text)
		if onDelta != nil {
			if err := onDelta(text); err != nil {
				return builder.String(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if builder.Len() > 0 && ctx.Err() == nil {
			return builder.String(), nil
		}
		return "", err
	}
	text := builder.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("anthropic streaming upstream returned no content")
	}
	return text, nil
}

func extractAnthropicStreamText(data string) string {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return ""
	}
	if delta, ok := decoded["delta"].(map[string]any); ok {
		if text, ok := delta["text"].(string); ok {
			return text
		}
	}
	if contentBlock, ok := decoded["content_block"].(map[string]any); ok {
		if text, ok := contentBlock["text"].(string); ok {
			return text
		}
	}
	return ""
}

func buildAnthropicMessages(messages []aiMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Content) == "" && len(message.Attachments) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"role":    normalizeAnthropicRole(message.Role),
			"content": buildAnthropicContentBlocks(message),
		})
	}
	return out
}

func splitAnthropicSystemPrompt(messages []aiMessage) (string, []aiMessage) {
	anthropicMessages := make([]aiMessage, 0, len(messages))
	systemParts := make([]string, 0, 1)
	for _, message := range messages {
		role := normalizeProviderRole(message.Role)
		if role == "system" {
			if text := strings.TrimSpace(message.Content); text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		anthropicMessages = append(anthropicMessages, aiMessage{
			Role:        normalizeAnthropicRole(role),
			Content:     message.Content,
			Attachments: message.Attachments,
		})
	}
	return strings.Join(systemParts, "\n\n"), anthropicMessages
}

func flattenContent(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				if text, ok := mapped["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		raw, _ := json.Marshal(value)
		return string(raw)
	}
}

func parseVisibleReasoningEnvelope(raw string) aiResponseEnvelope {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return aiResponseEnvelope{}
	}
	thinking := extractTaggedBlock(raw, "infinite_thinking")
	answer := extractTaggedBlock(raw, "infinite_answer")
	if answer == "" {
		answer = cleanupReasoningTags(raw)
	}
	if answer == "" {
		answer = raw
	}
	return aiResponseEnvelope{
		Content:          strings.TrimSpace(answer),
		ReasoningContent: strings.TrimSpace(thinking),
	}
}

func extractTaggedBlock(raw string, tag string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(raw, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(raw[start:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(raw[start : start+end])
}

func cleanupReasoningTags(raw string) string {
	cleaned := removeTaggedBlock(raw, "infinite_thinking")
	replacements := []string{
		"<infinite_thinking>",
		"</infinite_thinking>",
		"<infinite_answer>",
		"</infinite_answer>",
	}
	for _, marker := range replacements {
		cleaned = strings.ReplaceAll(cleaned, marker, "")
	}
	return strings.TrimSpace(cleaned)
}

func removeTaggedBlock(raw string, tag string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	cleaned := raw
	for {
		start := strings.Index(cleaned, openTag)
		if start < 0 {
			return cleaned
		}
		afterOpen := start + len(openTag)
		end := strings.Index(cleaned[afterOpen:], closeTag)
		if end < 0 {
			return cleaned[:start]
		}
		cleaned = cleaned[:start] + cleaned[afterOpen+end+len(closeTag):]
	}
}
