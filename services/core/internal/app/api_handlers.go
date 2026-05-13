package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type chatCompletionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	Stream     bool             `json:"stream"`
	Tools      []map[string]any `json:"tools"`
	ToolChoice any              `json:"tool_choice"`
}

type responsesCreateRequest struct {
	Model        string           `json:"model"`
	Input        any              `json:"input"`
	Instructions any              `json:"instructions"`
	Tools        []map[string]any `json:"tools"`
	ToolChoice   any              `json:"tool_choice"`
	Include      []string         `json:"include"`
	Store        *bool            `json:"store"`
	Stream       bool             `json:"stream"`
}

type anthropicMessageRequest struct {
	Model     string `json:"model"`
	System    any    `json:"system"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

type imageGenerationRequest struct {
	Model           string                `json:"model"`
	Prompt          string                `json:"prompt"`
	Size            string                `json:"size"`
	N               int                   `json:"n"`
	Quality         string                `json:"quality"`
	ResponseFormat  string                `json:"response_format"`
	Background      string                `json:"background"`
	InputFidelity   string                `json:"input_fidelity"`
	ReferenceImages []imageReferenceInput `json:"-"`
}

type imageReferenceInput struct {
	FileName string
	MimeType string
	Data     []byte
}

const defaultPhotoRoute = "infinite-ai-photo"

func (s *Server) handleDeveloperChatCompletion(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	key, userID, account, rawToken, ok := s.authenticateDeveloperAPIRequest(w, r)
	if !ok {
		return
	}
	if !apiKeyAllows(key.Scopes, "chat:write", "chat:completions:create") {
		httpx.Error(w, http.StatusForbidden, "api_key_scope_forbidden", "当前 API Key 没有对话生成权限")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "forbidden",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	if err := s.enforceAPIKeyRateLimit(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusTooManyRequests, "api_key_rate_limited", "API Key 请求过于频繁，请稍后再试")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "rate_limited",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	_ = s.Store.TouchAPIKey(r.Context(), key.ID)

	var body chatCompletionRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if body.Model == "" {
		body.Model = s.Config.DefaultChatRoute
	}
	status := "error"
	errorDetail := ""
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "developer_api",
			Path:        r.URL.Path,
			UserID:      userID,
			KeyID:       key.ID,
			Account:     account,
			Model:       body.Model,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	messages := make([]aiMessage, 0, len(body.Messages))
	for _, item := range body.Messages {
		content, attachments := parseOpenAICompatibleContent(item.Content)
		messages = append(messages, aiMessage{
			Role:        normalizeProviderRole(item.Role),
			Content:     content,
			Attachments: attachments,
		})
	}
	quotaReservation, quotaOK := s.reserveInfiniteCodeQuota(w, r, rawToken, userID, body.Model)
	if !quotaOK {
		status = "quota_exhausted"
		return
	}
	if developerToolsRequireAgentProxy(body.Tools) {
		if err := s.proxyDeveloperChatCompletions(r.Context(), w, body.Model, chatCompletionProxyPayload(body)); err != nil {
			status = classifyUpstreamErrorStatus(err)
			errorDetail = summarizeErrorDetail(err)
			s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "agent_proxy_failed", err)
			httpx.Error(w, http.StatusBadGateway, "agent_proxy_failed", sanitizeUserFacingChatError(err))
			return
		}
		status = "ok"
		return
	}
	if body.Stream && !developerToolsRequestWebSearch(body.Tools) {
		status = "ok"
		if err := s.streamDeveloperChatCompletionNative(r.Context(), w, body.Model, messages); err != nil {
			status = classifyUpstreamErrorStatus(err)
			errorDetail = summarizeErrorDetail(err)
			s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "stream_generation_failed", err)
		}
		return
	}
	reply, _, err := s.generateDeveloperAPIResponse(r.Context(), body.Model, messages, body.Tools)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "upstream_generation_failed", err)
		httpx.Error(w, http.StatusBadGateway, "upstream_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	if body.Stream {
		status = "ok"
		streamOpenAIResponse(w, body.Model, reply)
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl_" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   body.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": reply,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
		"userId": userID,
	})
}

func (s *Server) handleDeveloperResponses(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	key, userID, account, rawToken, ok := s.authenticateDeveloperAPIRequest(w, r)
	if !ok {
		return
	}
	if !apiKeyAllows(key.Scopes, "chat:write", "chat:completions:create") {
		httpx.Error(w, http.StatusForbidden, "api_key_scope_forbidden", "当前 API Key 没有响应生成权限")
		return
	}
	if err := s.enforceAPIKeyRateLimit(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusTooManyRequests, "api_key_rate_limited", "API Key 请求过于频繁，请稍后再试")
		return
	}
	_ = s.Store.TouchAPIKey(r.Context(), key.ID)

	var body responsesCreateRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		body.Model = s.Config.DefaultChatRoute
	}
	status := "error"
	errorDetail := ""
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "developer_api",
			Path:        r.URL.Path,
			UserID:      userID,
			KeyID:       key.ID,
			Account:     account,
			Model:       body.Model,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	messages := parseResponsesInputMessages(body.Input)
	if instructions := strings.TrimSpace(flattenContent(body.Instructions)); instructions != "" {
		messages = append([]aiMessage{{Role: "system", Content: instructions}}, messages...)
	}
	if len(messages) == 0 {
		httpx.Error(w, http.StatusBadRequest, "responses_input_required", "请提供 input")
		return
	}
	quotaReservation, quotaOK := s.reserveInfiniteCodeQuota(w, r, rawToken, userID, body.Model)
	if !quotaOK {
		status = "quota_exhausted"
		return
	}
	if developerToolsRequireAgentProxy(body.Tools) {
		if err := s.proxyDeveloperResponses(r.Context(), w, body.Model, responsesProxyPayload(body)); err != nil {
			status = classifyUpstreamErrorStatus(err)
			errorDetail = summarizeErrorDetail(err)
			s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "agent_proxy_failed", err)
			httpx.Error(w, http.StatusBadGateway, "agent_proxy_failed", sanitizeUserFacingChatError(err))
			return
		}
		status = "ok"
		return
	}
	if body.Stream && !developerToolsRequestWebSearch(body.Tools) {
		status = "ok"
		if err := s.streamDeveloperResponsesNative(r.Context(), w, body.Model, messages); err != nil {
			status = classifyUpstreamErrorStatus(err)
			errorDetail = summarizeErrorDetail(err)
			s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "stream_generation_failed", err)
		}
		return
	}
	reply, sources, err := s.generateDeveloperAPIResponse(r.Context(), body.Model, messages, body.Tools)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "upstream_generation_failed", err)
		httpx.Error(w, http.StatusBadGateway, "upstream_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	status = "ok"
	if body.Stream {
		streamResponsesAPIResponse(w, body.Model, reply, sources)
		return
	}
	httpx.JSON(w, http.StatusOK, buildResponsesAPIResponse(body.Model, reply, sources))
}

func (s *Server) handleDeveloperAnthropicMessage(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	key, userID, account, rawToken, ok := s.authenticateDeveloperAPIRequest(w, r)
	if !ok {
		return
	}
	if !apiKeyAllows(key.Scopes, "chat:write", "chat:completions:create") {
		httpx.Error(w, http.StatusForbidden, "api_key_scope_forbidden", "当前 API Key 没有消息生成权限")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "forbidden",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	if err := s.enforceAPIKeyRateLimit(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusTooManyRequests, "api_key_rate_limited", "API Key 请求过于频繁，请稍后再试")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "rate_limited",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	_ = s.Store.TouchAPIKey(r.Context(), key.ID)

	var body anthropicMessageRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		body.Model = s.Config.DefaultChatRoute
	}
	status := "error"
	errorDetail := ""
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "developer_api",
			Path:        r.URL.Path,
			UserID:      userID,
			KeyID:       key.ID,
			Account:     account,
			Model:       body.Model,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	messages := make([]aiMessage, 0, len(body.Messages)+1)
	systemText := strings.TrimSpace(flattenContent(body.System))
	if systemText != "" {
		messages = append(messages, aiMessage{Role: "system", Content: systemText})
	}
	for _, item := range body.Messages {
		content, attachments := parseOpenAICompatibleContent(item.Content)
		messages = append(messages, aiMessage{
			Role:        normalizeProviderRole(item.Role),
			Content:     content,
			Attachments: attachments,
		})
	}
	quotaReservation, quotaOK := s.reserveInfiniteCodeQuota(w, r, rawToken, userID, body.Model)
	if !quotaOK {
		status = "quota_exhausted"
		return
	}
	reply, err := s.generateAIResponse(r.Context(), body.Model, messages)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "upstream_generation_failed", err)
		httpx.Error(w, http.StatusBadGateway, "upstream_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":    "msg_" + time.Now().Format("20060102150405"),
		"type":  "message",
		"role":  "assistant",
		"model": body.Model,
		"content": []map[string]any{
			{
				"type": "text",
				"text": reply,
			},
		},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	})
}

func (s *Server) handleDeveloperImageGeneration(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	key, userID, account, rawToken, ok := s.authenticateDeveloperAPIRequest(w, r)
	if !ok {
		return
	}
	if !apiKeyAllows(key.Scopes, "images:generate", "image:write", "chat:write") {
		httpx.Error(w, http.StatusForbidden, "api_key_scope_forbidden", "当前 API Key 没有图片生成权限")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "forbidden",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	if err := s.enforceAPIKeyRateLimit(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusTooManyRequests, "api_key_rate_limited", "API Key 请求过于频繁，请稍后再试")
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:    "developer_api",
			Path:      r.URL.Path,
			UserID:    userID,
			KeyID:     key.ID,
			Account:   account,
			Model:     "",
			Status:    "rate_limited",
			LatencyMs: time.Since(startedAt).Milliseconds(),
		})
		return
	}
	_ = s.Store.TouchAPIKey(r.Context(), key.ID)

	var body imageGenerationRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		body.Model = defaultPhotoRoute
	}
	if strings.TrimSpace(body.Prompt) == "" {
		httpx.Error(w, http.StatusBadRequest, "prompt_required", "请输入图片生成提示词")
		return
	}
	status := "error"
	errorDetail := ""
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "developer_api",
			Path:        r.URL.Path,
			UserID:      userID,
			KeyID:       key.ID,
			Account:     account,
			Model:       body.Model,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	quotaReservation, quotaOK := s.reserveInfiniteCodeQuota(w, r, rawToken, userID, body.Model)
	if !quotaOK {
		status = "quota_exhausted"
		return
	}
	responsePayload, err := s.generateImageResponse(r.Context(), body.Model, body)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "upstream_image_generation_failed", err)
		httpx.Error(w, http.StatusBadGateway, "upstream_image_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, responsePayload)
}

func (s *Server) handleDeveloperImageEdit(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	key, userID, account, rawToken, ok := s.authenticateDeveloperAPIRequest(w, r)
	if !ok {
		return
	}
	if !apiKeyAllows(key.Scopes, "images:generate", "image:write", "chat:write") {
		httpx.Error(w, http.StatusForbidden, "api_key_scope_forbidden", "当前 API Key 没有图片编辑权限")
		return
	}
	if err := s.enforceAPIKeyRateLimit(r.Context(), key); err != nil {
		httpx.Error(w, http.StatusTooManyRequests, "api_key_rate_limited", "API Key 请求过于频繁，请稍后再试")
		return
	}
	_ = s.Store.TouchAPIKey(r.Context(), key.ID)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		httpx.Error(w, http.StatusBadRequest, "multipart_invalid", "请使用 multipart/form-data 上传 image 和 prompt")
		return
	}
	model := strings.TrimSpace(r.FormValue("model"))
	if model == "" {
		model = defaultPhotoRoute
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		httpx.Error(w, http.StatusBadRequest, "prompt_required", "请输入图片编辑提示词")
		return
	}
	references, err := imageReferencesFromMultipart(r.MultipartForm)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "image_reference_invalid", err.Error())
		return
	}
	if len(references) == 0 {
		httpx.Error(w, http.StatusBadRequest, "image_required", "请至少上传一张 image")
		return
	}
	status := "error"
	errorDetail := ""
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "developer_api",
			Path:        r.URL.Path,
			UserID:      userID,
			KeyID:       key.ID,
			Account:     account,
			Model:       model,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	request := imageGenerationRequest{
		Model:           model,
		Prompt:          prompt,
		Size:            strings.TrimSpace(r.FormValue("size")),
		Quality:         strings.TrimSpace(r.FormValue("quality")),
		ResponseFormat:  strings.TrimSpace(r.FormValue("response_format")),
		Background:      strings.TrimSpace(r.FormValue("background")),
		InputFidelity:   strings.TrimSpace(r.FormValue("input_fidelity")),
		ReferenceImages: references,
	}
	quotaReservation, quotaOK := s.reserveInfiniteCodeQuota(w, r, rawToken, userID, model)
	if !quotaOK {
		status = "quota_exhausted"
		return
	}
	payload, err := s.generateImageResponse(r.Context(), model, request)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		s.refundInfiniteCodeQuota(r.Context(), quotaReservation, "upstream_image_edit_failed", err)
		httpx.Error(w, http.StatusBadGateway, "upstream_image_edit_failed", sanitizeUserFacingChatError(err))
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, payload)
}

func (s *Server) authenticateDeveloperAPIRequest(w http.ResponseWriter, r *http.Request) (*store.APIKey, string, string, string, bool) {
	rawAPIKey := developerAPIKeyFromRequest(r)
	if rawAPIKey == "" {
		httpx.Error(w, http.StatusUnauthorized, "api_key_missing", "请提供 API Key")
		return nil, "", "", "", false
	}
	if strings.HasPrefix(rawAPIKey, auth.InfiniteCodeAccessTokenPrefix) {
		if key, userID, account, ok := s.authenticateInfiniteCodeToken(r.Context(), rawAPIKey); ok {
			return key, userID, account, rawAPIKey, true
		}
	}
	key, userID, err := s.Store.FindAPIKeyByHash(r.Context(), auth.HashAPIKey(rawAPIKey))
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "api_key_invalid", "API Key 无效或已被撤销")
		return nil, "", "", "", false
	}
	account := userID
	if user, lookupErr := s.Store.GetUserByID(r.Context(), userID); lookupErr == nil && strings.TrimSpace(user.Email) != "" {
		account = user.Email
	}
	return key, userID, account, rawAPIKey, true
}

func (s *Server) authenticateInfiniteCodeToken(ctx context.Context, rawToken string) (*store.APIKey, string, string, bool) {
	if s.Redis == nil {
		return nil, "", "", false
	}
	raw, err := s.Redis.Get(ctx, auth.InfiniteCodeTokenRedisKey("access", rawToken)).Result()
	if err != nil {
		return nil, "", "", false
	}
	var principal auth.InfiniteCodeTokenPrincipal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil || strings.TrimSpace(principal.UserID) == "" {
		return nil, "", "", false
	}
	account := principal.Email
	if user, err := s.Store.GetUserByID(ctx, principal.UserID); err == nil {
		if user.Status != "active" {
			return nil, "", "", false
		}
		if strings.TrimSpace(user.Email) != "" {
			account = user.Email
		}
	}
	if strings.TrimSpace(account) == "" {
		account = principal.UserID
	}
	return &store.APIKey{
		ID:                 "00000000-0000-0000-0000-000000000000",
		Name:               "Infinite Code",
		Prefix:             auth.InfiniteCodeAccessTokenPrefix,
		Scopes:             []string{"*"},
		Status:             "active",
		RateLimitPerMinute: 120,
	}, principal.UserID, account, true
}

func developerAPIKeyFromRequest(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("X-External-API-Key")); raw != "" {
		return raw
	}
	if raw := strings.TrimSpace(r.Header.Get("x-api-key")); raw != "" {
		return raw
	}
	externalAuthorization := r.Header.Get("X-External-Authorization")
	if externalAuthorization == "" {
		externalAuthorization = r.Header.Get("Authorization")
	}
	return strings.TrimSpace(strings.TrimPrefix(externalAuthorization, "Bearer "))
}

func apiKeyAllows(scopes []string, required ...string) bool {
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "*" {
			return true
		}
		for _, candidate := range required {
			if scope == candidate {
				return true
			}
		}
	}
	return false
}

func (s *Server) enforceAPIKeyRateLimit(ctx context.Context, key *store.APIKey) error {
	if s.Redis == nil || key == nil || key.ID == "" || key.RateLimitPerMinute <= 0 {
		return nil
	}
	window := time.Now().UTC().Format("200601021504")
	redisKey := fmt.Sprintf("rate-limit:api-key:%s:%s", key.ID, window)
	count, err := s.Redis.Incr(ctx, redisKey).Result()
	if err != nil {
		return nil
	}
	if count == 1 {
		_ = s.Redis.Expire(ctx, redisKey, 2*time.Minute).Err()
	}
	if count > int64(key.RateLimitPerMinute) {
		return fmt.Errorf("API Key 请求过于频繁，请稍后再试")
	}
	return nil
}

func streamOpenAIResponse(w http.ResponseWriter, model string, reply string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
		return
	}
	chunks := splitChunks(reply, 32)
	for _, chunk := range chunks {
		payload := map[string]any{
			"id":      "chatcmpl_stream",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": chunk,
					},
				},
			},
		}
		raw, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) streamDeveloperChatCompletionNative(ctx context.Context, w http.ResponseWriter, model string, messages []aiMessage) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
		return fmt.Errorf("stream unsupported")
	}
	streamID := "chatcmpl_stream_" + time.Now().Format("20060102150405")
	emitted := false
	sendDelta := func(delta string) error {
		if delta == "" {
			return nil
		}
		emitted = true
		payload := map[string]any{
			"id":      streamID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": delta,
					},
				},
			},
		}
		raw, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
		return nil
	}
	result, err := s.generateAIResponseEnvelopeStream(ctx, model, messages, false, sendDelta)
	if err != nil {
		sendOpenAIStreamError(w, err)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return err
	}
	if !emitted && result.Content != "" {
		if err := sendDelta(result.Content); err != nil {
			return err
		}
	}
	finishPayload := map[string]any{
		"id":      streamID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			},
		},
	}
	raw, _ := json.Marshal(finishPayload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

func sendOpenAIStreamError(w http.ResponseWriter, err error) {
	payload := map[string]any{
		"error": map[string]any{
			"message": sanitizeUserFacingChatError(err),
			"type":    "upstream_generation_failed",
		},
	}
	raw, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}

func (s *Server) generateDeveloperAPIResponse(ctx context.Context, model string, messages []aiMessage, tools []map[string]any) (string, []store.SearchSource, error) {
	if !developerToolsRequestWebSearch(tools) {
		reply, err := s.generateAIResponse(ctx, model, messages)
		return reply, nil, err
	}
	sourceLimit := 5
	if config := s.enabledDeepSearchConfig(ctx); config != nil && config.ResultCount > 0 {
		sourceLimit = config.ResultCount
	}
	if result, sources, err := s.generateOpenAIWebSearchResponse(ctx, model, messages, sourceLimit); err == nil && strings.TrimSpace(result.Content) != "" {
		return result.Content, sources, nil
	}
	if result, sources, err := s.generateAnthropicWebSearchResponse(ctx, model, messages, sourceLimit); err == nil && strings.TrimSpace(result.Content) != "" {
		return result.Content, sources, nil
	}
	searchQuery := latestAIUserPrompt(messages)
	fallbackHistory, sources := s.applyDeepSearchFallback(ctx, messages, searchQuery, s.enabledDeepSearchConfig(ctx), nil)
	result, err := s.generateAIResponseEnvelope(ctx, model, fallbackHistory, false)
	if err != nil {
		return "", sources, err
	}
	return result.Content, sources, nil
}

func developerToolsRequestWebSearch(tools []map[string]any) bool {
	for _, tool := range tools {
		toolType := strings.ToLower(strings.TrimSpace(fmt.Sprint(tool["type"])))
		if toolType == "web_search" || toolType == "web_search_preview" {
			return true
		}
		if function, ok := tool["function"].(map[string]any); ok {
			name := strings.ToLower(strings.TrimSpace(fmt.Sprint(function["name"])))
			if strings.Contains(name, "web_search") || strings.Contains(name, "search") {
				return true
			}
		}
	}
	return false
}

func developerToolsRequireAgentProxy(tools []map[string]any) bool {
	return len(tools) > 0 && !developerToolsRequestWebSearch(tools)
}

func chatCompletionProxyPayload(body chatCompletionRequest) map[string]any {
	payload := map[string]any{
		"model":    body.Model,
		"messages": body.Messages,
		"stream":   body.Stream,
	}
	if len(body.Tools) > 0 {
		payload["tools"] = body.Tools
		if body.ToolChoice != nil {
			payload["tool_choice"] = body.ToolChoice
		}
	}
	return payload
}

func responsesProxyPayload(body responsesCreateRequest) map[string]any {
	payload := map[string]any{
		"model": body.Model,
		"input": body.Input,
		"store": false,
	}
	if body.Store != nil {
		payload["store"] = *body.Store
	}
	if body.Stream {
		payload["stream"] = true
	}
	if body.Instructions != nil {
		payload["instructions"] = body.Instructions
	}
	if len(body.Tools) > 0 {
		payload["tools"] = body.Tools
		if body.ToolChoice != nil {
			payload["tool_choice"] = body.ToolChoice
		}
	}
	if len(body.Include) > 0 {
		payload["include"] = body.Include
	}
	return payload
}

func parseResponsesInputMessages(input any) []aiMessage {
	switch typed := input.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []aiMessage{{Role: "user", Content: typed}}
	case []any:
		out := make([]aiMessage, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				role := strings.TrimSpace(fmt.Sprint(mapped["role"]))
				if role == "" {
					role = "user"
				}
				content, attachments := parseOpenAICompatibleContent(mapped["content"])
				if strings.TrimSpace(content) == "" && len(attachments) == 0 {
					continue
				}
				out = append(out, aiMessage{Role: normalizeProviderRole(role), Content: content, Attachments: attachments})
			} else if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, aiMessage{Role: "user", Content: text})
			}
		}
		return out
	case map[string]any:
		role := strings.TrimSpace(fmt.Sprint(typed["role"]))
		if role == "" {
			role = "user"
		}
		content, attachments := parseOpenAICompatibleContent(typed["content"])
		if strings.TrimSpace(content) == "" && len(attachments) == 0 {
			return nil
		}
		return []aiMessage{{Role: normalizeProviderRole(role), Content: content, Attachments: attachments}}
	default:
		text := strings.TrimSpace(flattenContent(input))
		if text == "" {
			return nil
		}
		return []aiMessage{{Role: "user", Content: text}}
	}
}

func parseOpenAICompatibleContent(value any) (string, []aiMessageAttachment) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []any:
		textParts := make([]string, 0, len(typed))
		attachments := make([]aiMessageAttachment, 0)
		for _, item := range typed {
			mapped, ok := item.(map[string]any)
			if !ok {
				if text := strings.TrimSpace(flattenContent(item)); text != "" {
					textParts = append(textParts, text)
				}
				continue
			}
			partType := strings.ToLower(strings.TrimSpace(fmt.Sprint(mapped["type"])))
			switch partType {
			case "text", "input_text", "output_text":
				if text := strings.TrimSpace(fmt.Sprint(mapped["text"])); text != "" {
					textParts = append(textParts, text)
				}
			case "image_url", "input_image":
				rawURL := ""
				if imageURL, ok := mapped["image_url"].(map[string]any); ok {
					rawURL = strings.TrimSpace(fmt.Sprint(imageURL["url"]))
				} else if value, ok := mapped["image_url"].(string); ok {
					rawURL = strings.TrimSpace(value)
				}
				if rawURL == "" {
					rawURL = strings.TrimSpace(fmt.Sprint(mapped["url"]))
				}
				if attachment, ok := parseDataURLImageAttachment(rawURL); ok {
					attachments = append(attachments, attachment)
				} else if isSupportedRemoteImageURL(rawURL) {
					attachments = append(attachments, aiMessageAttachment{
						FileName: "input-image",
						MimeType: "image/remote",
						URL:      rawURL,
					})
				}
			default:
				if text := strings.TrimSpace(flattenContent(mapped)); text != "" && text != "null" {
					textParts = append(textParts, text)
				}
			}
		}
		return strings.Join(textParts, "\n"), attachments
	default:
		return flattenContent(value), nil
	}
}

func parseDataURLImageAttachment(rawURL string) (aiMessageAttachment, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "data:image/") {
		return aiMessageAttachment{}, false
	}
	comma := strings.Index(rawURL, ",")
	if comma < 0 {
		return aiMessageAttachment{}, false
	}
	header := rawURL[:comma]
	encoded := rawURL[comma+1:]
	if !strings.Contains(strings.ToLower(header), ";base64") {
		return aiMessageAttachment{}, false
	}
	mediaType := strings.TrimPrefix(header, "data:")
	if semi := strings.Index(mediaType, ";"); semi >= 0 {
		mediaType = mediaType[:semi]
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return aiMessageAttachment{}, false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return aiMessageAttachment{}, false
	}
	return aiMessageAttachment{
		FileName: "input-image.png",
		MimeType: mediaType,
		Data:     data,
	}, true
}

func isSupportedRemoteImageURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" || parsed.Scheme == "http"
}

func buildResponsesAPIResponse(model string, reply string, sources []store.SearchSource) map[string]any {
	annotations := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.URL) == "" {
			continue
		}
		annotations = append(annotations, map[string]any{
			"type":  "url_citation",
			"url":   source.URL,
			"title": source.Title,
		})
	}
	return map[string]any{
		"id":          "resp_" + time.Now().Format("20060102150405"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       model,
		"output_text": reply,
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{
						"type":        "output_text",
						"text":        reply,
						"annotations": annotations,
					},
				},
			},
		},
		"store": false,
	}
}

func streamResponsesAPIResponse(w http.ResponseWriter, model string, reply string, sources []store.SearchSource) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
		return
	}
	responseID := "resp_" + time.Now().Format("20060102150405")
	sendResponsesStreamEvent(w, "response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"model":  model,
			"status": "in_progress",
		},
	})
	flusher.Flush()
	for _, chunk := range splitChunks(reply, 32) {
		sendResponsesStreamEvent(w, "response.output_text.delta", map[string]any{
			"type":  "response.output_text.delta",
			"delta": chunk,
		})
		flusher.Flush()
	}
	completed := buildResponsesAPIResponse(model, reply, sources)
	completed["id"] = responseID
	sendResponsesStreamEvent(w, "response.completed", map[string]any{
		"type":     "response.completed",
		"response": completed,
	})
	flusher.Flush()
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) streamDeveloperResponsesNative(ctx context.Context, w http.ResponseWriter, model string, messages []aiMessage) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
		return fmt.Errorf("stream unsupported")
	}
	responseID := "resp_" + time.Now().Format("20060102150405")
	sendResponsesStreamEvent(w, "response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"model":  model,
			"status": "in_progress",
		},
	})
	flusher.Flush()
	emitted := false
	sendDelta := func(delta string) error {
		if delta == "" {
			return nil
		}
		emitted = true
		sendResponsesStreamEvent(w, "response.output_text.delta", map[string]any{
			"type":  "response.output_text.delta",
			"delta": delta,
		})
		flusher.Flush()
		return nil
	}
	result, err := s.generateAIResponseEnvelopeStream(ctx, model, messages, false, sendDelta)
	if err != nil {
		sendResponsesStreamEvent(w, "response.failed", map[string]any{
			"type": "response.failed",
			"error": map[string]any{
				"message": sanitizeUserFacingChatError(err),
				"type":    "upstream_generation_failed",
			},
		})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return err
	}
	if !emitted && result.Content != "" {
		if err := sendDelta(result.Content); err != nil {
			return err
		}
	}
	completed := buildResponsesAPIResponse(model, result.Content, nil)
	completed["id"] = responseID
	sendResponsesStreamEvent(w, "response.completed", map[string]any{
		"type":     "response.completed",
		"response": completed,
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	return nil
}

func sendResponsesStreamEvent(w http.ResponseWriter, event string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}

func imageReferencesFromMultipart(form *multipart.Form) ([]imageReferenceInput, error) {
	if form == nil {
		return nil, nil
	}
	fileHeaders := make([]*multipart.FileHeader, 0)
	for _, key := range []string{"image", "image[]"} {
		fileHeaders = append(fileHeaders, form.File[key]...)
	}
	references := make([]imageReferenceInput, 0, len(fileHeaders))
	for _, header := range fileHeaders {
		if header == nil {
			continue
		}
		if header.Size > maxImageReferenceBytes {
			return nil, fmt.Errorf("参考图 %s 超过 25MB", header.Filename)
		}
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxImageReferenceBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(data)) > maxImageReferenceBytes {
			return nil, fmt.Errorf("参考图 %s 超过 25MB", header.Filename)
		}
		mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
		if mimeType == "" {
			mimeType = http.DetectContentType(data)
		}
		if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			return nil, fmt.Errorf("%s 不是图片文件", header.Filename)
		}
		references = append(references, imageReferenceInput{
			FileName: sanitizeFileName(header.Filename),
			MimeType: mimeType,
			Data:     data,
		})
	}
	return references, nil
}

func (s *Server) generateImageResponse(ctx context.Context, routeSelector string, request imageGenerationRequest) (map[string]any, error) {
	route, err := s.Store.FindActiveModelRoute(ctx, routeSelector, true)
	if err != nil {
		return nil, err
	}
	if route.ModelType != "image" {
		return nil, fmt.Errorf("model %s is not configured for image generation", routeSelector)
	}
	if len(route.Endpoints) == 0 {
		return nil, fmt.Errorf("provider route %s is not configured", routeSelector)
	}
	request.Prompt = mergeImagePrompt(route, request.Prompt)
	var lastErr error
	endpoints := openAIImageEndpointOrder(route.Endpoints)
	for _, endpoint := range endpoints {
		payload, err := s.callOpenAIImageGeneration(ctx, route, endpoint, request)
		if err == nil {
			return payload, nil
		}
		lastErr = err
		if route.Strategy == "concurrent" {
			continue
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no active upstream endpoint configured")
	}
	return nil, lastErr
}

func openAIImageEndpointOrder(endpoints []store.ModelEndpoint) []store.ModelEndpoint {
	official := make([]store.ModelEndpoint, 0, len(endpoints))
	compatible := make([]store.ModelEndpoint, 0, len(endpoints))
	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		baseURL := strings.TrimSpace(endpoint.BaseURL)
		secret := strings.TrimSpace(endpoint.Secret)
		if !endpoint.Active || baseURL == "" || secret == "" {
			continue
		}
		key := strings.ToLower(strings.TrimRight(baseURL, "/")) + "\x00" + secret
		if seen[key] {
			continue
		}
		seen[key] = true
		if isOfficialOpenAIEndpoint(baseURL) {
			official = append(official, endpoint)
			continue
		}
		compatible = append(compatible, endpoint)
	}
	return append(official, compatible...)
}

func mergeImagePrompt(route *store.ModelRoute, prompt string) string {
	if route == nil || !route.PromptEnabled || strings.TrimSpace(route.PromptText) == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return route.PromptText
	}
	return route.PromptText + "\n\n" + prompt
}

func (s *Server) callOpenAIImageGeneration(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, request imageGenerationRequest) (map[string]any, error) {
	if route.Protocol != "openai" {
		return nil, fmt.Errorf("image generation currently requires openai-compatible protocol")
	}
	if len(request.ReferenceImages) > 0 {
		if payload, err := s.callOpenAIImageEdit(ctx, route, endpoint, request); err == nil {
			return payload, nil
		}
	}
	payload := map[string]any{
		"model":  route.UpstreamModel,
		"prompt": request.Prompt,
	}
	if request.Size != "" {
		payload["size"] = request.Size
	}
	if request.N > 0 {
		payload["n"] = request.N
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	if request.ResponseFormat != "" {
		payload["response_format"] = request.ResponseFormat
	}
	if request.Background != "" {
		payload["background"] = request.Background
	}
	body, _ := json.Marshal(payload)
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/images/generations")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationImages); ok {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/images/generations", cached.URL)
	}
	for _, target := range candidates {
		requestCtx, cancel := context.WithTimeout(ctx, imageUpstreamRequestTimeout(route))
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return nil, err
		}
		if res.StatusCode >= 300 {
			bodyText := readUpstreamErrorBody(res.Body)
			_ = res.Body.Close()
			cancel()
			lastErr = newUpstreamHTTPError("openai-compatible image", res.StatusCode, bodyText)
			if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
				continue
			}
			return nil, lastErr
		}
		var decoded map[string]any
		if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			cancel()
			return nil, err
		}
		_ = res.Body.Close()
		cancel()
		s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationImages, openAIEndpointAdaptation{
			Kind: "images",
			URL:  target,
		})
		return decoded, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("openai-compatible image upstream endpoint is not configured")
}

func (s *Server) callOpenAIImageEdit(ctx context.Context, route *store.ModelRoute, endpoint store.ModelEndpoint, request imageGenerationRequest) (map[string]any, error) {
	if route.Protocol != "openai" {
		return nil, fmt.Errorf("image editing currently requires openai-compatible protocol")
	}
	if len(request.ReferenceImages) == 0 {
		return nil, fmt.Errorf("image editing requires at least one reference image")
	}
	request = adaptOpenAIImageEditRequestForEndpoint(endpoint, request)
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/images/edits")
	if cached, ok := s.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationImageEdits); ok {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/images/edits", cached.URL)
	}
	imageFieldNames := []string{"image"}
	if len(request.ReferenceImages) > 1 {
		imageFieldNames = []string{"image[]", "image"}
	}
	for _, target := range candidates {
		for _, imageFieldName := range imageFieldNames {
			body, contentType, err := buildOpenAIImageEditMultipart(route.UpstreamModel, request, imageFieldName)
			if err != nil {
				return nil, err
			}
			requestCtx, cancel := context.WithTimeout(ctx, imageUpstreamRequestTimeout(route))
			req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target, body)
			if err != nil {
				cancel()
				return nil, err
			}
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				cancel()
				return nil, err
			}
			if res.StatusCode >= 300 {
				bodyText := readUpstreamErrorBody(res.Body)
				_ = res.Body.Close()
				cancel()
				lastErr = newUpstreamHTTPError("openai-compatible image edit", res.StatusCode, bodyText)
				if len(imageFieldNames) > 1 && imageFieldName == imageFieldNames[0] && res.StatusCode == http.StatusBadRequest {
					continue
				}
				if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
					continue
				}
				return nil, lastErr
			}
			var decoded map[string]any
			if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
				_ = res.Body.Close()
				cancel()
				return nil, err
			}
			_ = res.Body.Close()
			cancel()
			s.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationImageEdits, openAIEndpointAdaptation{
				Kind: "image_edits",
				URL:  target,
			})
			return decoded, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("openai-compatible image edit upstream endpoint is not configured")
}

func adaptOpenAIImageEditRequestForEndpoint(endpoint store.ModelEndpoint, request imageGenerationRequest) imageGenerationRequest {
	if len(request.ReferenceImages) <= 1 || isOfficialOpenAIEndpoint(endpoint.BaseURL) {
		return request
	}
	// Some OpenAI-compatible gateways hang on multi-image edit multipart fields.
	// Use the first source image for edit semantics and avoid blocking the run.
	request.ReferenceImages = request.ReferenceImages[:1]
	return request
}

func imageUpstreamRequestTimeout(route *store.ModelRoute) time.Duration {
	if route != nil && route.ModelType == "image" {
		return 90 * time.Second
	}
	return 60 * time.Second
}

func buildOpenAIImageEditMultipart(model string, request imageGenerationRequest, imageFieldName string) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", model); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("prompt", request.Prompt); err != nil {
		return nil, "", err
	}
	if request.Size != "" {
		if err := writer.WriteField("size", request.Size); err != nil {
			return nil, "", err
		}
	}
	if request.N > 0 {
		if err := writer.WriteField("n", fmt.Sprintf("%d", request.N)); err != nil {
			return nil, "", err
		}
	}
	if request.Quality != "" {
		if err := writer.WriteField("quality", request.Quality); err != nil {
			return nil, "", err
		}
	}
	if request.ResponseFormat != "" {
		if err := writer.WriteField("response_format", request.ResponseFormat); err != nil {
			return nil, "", err
		}
	}
	if request.Background != "" {
		if err := writer.WriteField("background", request.Background); err != nil {
			return nil, "", err
		}
	}
	if request.InputFidelity != "" && shouldSendOpenAIImageInputFidelity(model) {
		if err := writer.WriteField("input_fidelity", request.InputFidelity); err != nil {
			return nil, "", err
		}
	}
	for index, image := range request.ReferenceImages {
		fileName := sanitizeFileName(image.FileName)
		if fileName == "" {
			fileName = fmt.Sprintf("reference-%d.png", index+1)
		}
		part, err := writer.CreateFormFile(imageFieldName, fileName)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(image.Data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body, writer.FormDataContentType(), nil
}

func shouldSendOpenAIImageInputFidelity(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return true
	}
	return !strings.Contains(model, "gpt-image-2")
}

func splitChunks(value string, size int) []string {
	if value == "" || size <= 0 {
		return []string{value}
	}
	runes := []rune(value)
	var chunks []string
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}
