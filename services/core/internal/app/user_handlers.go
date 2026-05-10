package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type createConversationRequest struct {
	Title      string `json:"title"`
	ModelSlug  string `json:"modelSlug"`
	DeepSearch bool   `json:"deepSearch"`
}

type createMessageRequest struct {
	Content       string   `json:"content"`
	ModelSlug     string   `json:"modelSlug"`
	DeepSearch    bool     `json:"deepSearch"`
	AttachmentIDs []string `json:"attachmentIds"`
	EditMessageID string   `json:"editMessageId"`
}

type createAPIKeyRequest struct {
	Name               string   `json:"name"`
	Scopes             []string `json:"scopes"`
	RateLimitPerMinute int      `json:"rateLimitPerMinute"`
}

type createOrderRequest struct {
	Type           string  `json:"type"`
	PlanCode       string  `json:"planCode"`
	RechargeAmount float64 `json:"rechargeAmount"`
	SubMethod      string  `json:"subMethod"`
}

type updateSettingsRequest struct {
	Theme              string `json:"theme"`
	Language           string `json:"language"`
	DeepSearchDefault  bool   `json:"deepSearchDefault"`
	SelectedModelSlug  string `json:"selectedModelSlug"`
	ChatHistoryEnabled bool   `json:"chatHistoryEnabled"`
	MemoryEnabled      *bool  `json:"memoryEnabled"`
}

type modelLimitNotice struct {
	Reason      string `json:"reason"`
	ModelSlug   string `json:"modelSlug"`
	ModelName   string `json:"modelName"`
	PlanCode    string `json:"planCode"`
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	WindowHours int    `json:"windowHours"`
}

type attachmentUploadInitRequest struct {
	ConversationID string `json:"conversationId"`
	FileName       string `json:"fileName"`
	MimeType       string `json:"mimeType"`
	SizeBytes      int64  `json:"sizeBytes"`
}

type createChatImageRequest struct {
	ConversationID string   `json:"conversationId"`
	Prompt         string   `json:"prompt"`
	ModelSlug      string   `json:"modelSlug"`
	Size           string   `json:"size"`
	N              int      `json:"n"`
	Quality        string   `json:"quality"`
	Background     string   `json:"background"`
	AttachmentIDs  []string `json:"attachmentIds"`
	EditMessageID  string   `json:"editMessageId"`
}

type temporaryChatMessage struct {
	ID          string               `json:"id,omitempty"`
	Role        string               `json:"role"`
	Content     string               `json:"content"`
	Attachments []store.MessageAsset `json:"attachments"`
	ModelSlug   string               `json:"modelSlug,omitempty"`
}

type createTemporaryMessageRequest struct {
	History       []temporaryChatMessage `json:"history"`
	Content       string                 `json:"content"`
	ModelSlug     string                 `json:"modelSlug"`
	DeepSearch    bool                   `json:"deepSearch"`
	AttachmentIDs []string               `json:"attachmentIds"`
}

type createTemporaryChatImageRequest struct {
	History       []temporaryChatMessage `json:"history"`
	Prompt        string                 `json:"prompt"`
	ModelSlug     string                 `json:"modelSlug"`
	Size          string                 `json:"size"`
	N             int                    `json:"n"`
	Quality       string                 `json:"quality"`
	Background    string                 `json:"background"`
	AttachmentIDs []string               `json:"attachmentIds"`
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.Store.ListPlans(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "plans_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	releases, err := s.Store.ListDownloadReleases(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "downloads_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	if settings, err := s.Store.GetUserSettings(r.Context(), currentUserID(r)); err == nil && !settings.ChatHistoryEnabled {
		httpx.JSON(w, http.StatusOK, map[string]any{"conversations": []store.Conversation{}})
		return
	}
	items, err := s.Store.ListConversations(r.Context(), currentUserID(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "conversations_load_failed", err.Error())
		return
	}
	for index := range items {
		if !isPlaceholderConversationTitle(items[index].Title) {
			continue
		}
		messages, err := s.Store.ListMessages(r.Context(), currentUserID(r), items[index].ID)
		if err != nil {
			continue
		}
		for _, message := range messages {
			if normalizeProviderRole(message.Role) != "user" || strings.TrimSpace(message.Content) == "" {
				continue
			}
			if title := maybeConversationTitle(items[index].Title, message.Content); title != "" {
				_ = s.Store.RenameConversation(r.Context(), items[index].ID, title)
				items[index].Title = title
			}
			break
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"conversations": items})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var body createConversationRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	modelSlug := body.ModelSlug
	if body.DeepSearch {
		modelSlug = s.Config.DeepSearchRoute
	}
	item, err := s.Store.CreateConversation(r.Context(), currentUserID(r), body.Title, modelSlug, body.DeepSearch)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "conversation_create_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, item)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteConversation(r.Context(), currentUserID(r), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, http.StatusNotFound, "conversation_delete_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListMessages(r.Context(), currentUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "messages_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"messages": items})
}

func (s *Server) handleCreateChatMessage(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	status := "error"
	errorDetail := ""
	var body createMessageRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Content) == "" && len(body.AttachmentIDs) == 0 {
		httpx.Error(w, http.StatusBadRequest, "message_empty", "请输入消息内容或上传附件")
		return
	}
	userID := currentUserID(r)
	conversationID := chi.URLParam(r, "id")
	conversation, err := s.Store.GetConversation(r.Context(), userID, conversationID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "conversation_not_found", err.Error())
		return
	}
	modelSlug := body.ModelSlug
	if modelSlug == "" {
		modelSlug = conversation.ModelSlug
	}
	if body.DeepSearch {
		modelSlug = s.Config.DeepSearchRoute
	}
	account := currentPrincipal(r).email
	if account == "" {
		account = userID
	}
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "web_chat",
			Path:        r.URL.Path,
			UserID:      userID,
			Account:     account,
			Model:       modelSlug,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	if limitNotice, err := s.getModelLimitNotice(r.Context(), userID, modelSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_limit_check_failed", err.Error())
		return
	} else if limitNotice != nil {
		status = "quota_exhausted"
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "model_limit_exceeded",
			"message": buildModelLimitMessage(limitNotice),
			"limit":   limitNotice,
		})
		return
	}
	assets, err := s.Store.GetAttachmentAssets(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "attachment_lookup_failed", err.Error())
		return
	}
	if err := s.Store.AssignAttachmentsToConversation(r.Context(), userID, conversationID, body.AttachmentIDs); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "attachment_assign_failed", err.Error())
		return
	}
	editMessageID := strings.TrimSpace(body.EditMessageID)
	var userMessage *store.Message
	if editMessageID != "" {
		userMessage, err = s.Store.RewriteConversationFromMessage(r.Context(), userID, conversationID, editMessageID, body.Content, assets, modelSlug)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "message_edit_failed", err.Error())
			return
		}
	} else {
		userMessage, err = s.Store.CreateMessage(r.Context(), conversationID, "user", body.Content, "", assets, modelSlug)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "message_create_failed", err.Error())
			return
		}
	}
	nextTitle := maybeConversationTitle(conversation.Title, body.Content)
	if nextTitle != "" {
		_ = s.Store.RenameConversation(r.Context(), conversationID, nextTitle)
		conversation.Title = nextTitle
	}
	showVisibleReasoning := body.DeepSearch || conversation.DeepSearch
	run, err := s.Store.CreateChatRun(r.Context(), userID, conversationID, modelSlug, showVisibleReasoning, userMessage.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "chat_run_create_failed", "聊天运行任务创建失败，请稍后重试")
		return
	}
	_, _ = s.Store.AppendChatRunEvent(r.Context(), run.ID, "run_started", map[string]any{
		"runId": run.ID,
		"run":   run,
	})
	_, _ = s.Store.AppendChatRunEvent(r.Context(), run.ID, "user", map[string]any{
		"message": userMessage,
	})
	s.startChatRunGeneration(run.ID, userID, conversationID, modelSlug, showVisibleReasoning, nextTitle, account, r.URL.Path, startedAt)
	streaming := r.URL.Query().Get("stream") == "1" || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if streaming {
		status = "ok"
		s.streamChatRunEvents(w, r, userID, run.ID, 0)
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"runId":       run.ID,
		"run":         run,
		"userMessage": userMessage,
		"title":       nextTitle,
	})
}

func (s *Server) handleCreateChatImage(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	status := "error"
	errorDetail := ""
	var body createChatImageRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		httpx.Error(w, http.StatusBadRequest, "image_prompt_required", "请输入绘图提示词")
		return
	}
	userID := currentUserID(r)
	conversationID := strings.TrimSpace(body.ConversationID)
	if conversationID == "" {
		httpx.Error(w, http.StatusBadRequest, "conversation_required", "缺少会话标识，请重新发起")
		return
	}
	conversation, err := s.Store.GetConversation(r.Context(), userID, conversationID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "conversation_not_found", err.Error())
		return
	}
	modelSlug := strings.TrimSpace(body.ModelSlug)
	if modelSlug == "" {
		modelSlug = defaultPhotoRoute
	}
	account := currentPrincipal(r).email
	if account == "" {
		account = userID
	}
	defer func() {
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "web_chat",
			Path:        r.URL.Path,
			UserID:      userID,
			Account:     account,
			Model:       modelSlug,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	if limitNotice, err := s.getModelLimitNotice(r.Context(), userID, modelSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_limit_check_failed", err.Error())
		return
	} else if limitNotice != nil {
		status = "quota_exhausted"
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "model_limit_exceeded",
			"message": buildModelLimitMessage(limitNotice),
			"limit":   limitNotice,
		})
		return
	}
	assets, err := s.Store.GetAttachmentAssets(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "attachment_lookup_failed", err.Error())
		return
	}
	if err := s.Store.AssignAttachmentsToConversation(r.Context(), userID, conversationID, body.AttachmentIDs); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "attachment_assign_failed", err.Error())
		return
	}
	referenceImages, err := s.loadImageReferenceInputs(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "image_reference_load_failed", err.Error())
		return
	}
	editMessageID := strings.TrimSpace(body.EditMessageID)
	var userMessage *store.Message
	if editMessageID != "" {
		userMessage, err = s.Store.RewriteConversationFromMessage(r.Context(), userID, conversationID, editMessageID, prompt, assets, modelSlug)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "image_prompt_edit_failed", err.Error())
			return
		}
	} else {
		userMessage, err = s.Store.CreateMessage(r.Context(), conversationID, "user", prompt, "", assets, modelSlug)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "image_prompt_create_failed", err.Error())
			return
		}
	}
	nextTitle := maybeConversationTitle(conversation.Title, prompt)
	if nextTitle != "" {
		_ = s.Store.RenameConversation(r.Context(), conversationID, nextTitle)
		conversation.Title = nextTitle
	}
	imageRequest := imageGenerationRequest{
		Model:           modelSlug,
		Prompt:          prompt,
		Size:            strings.TrimSpace(body.Size),
		N:               body.N,
		Quality:         strings.TrimSpace(body.Quality),
		Background:      strings.TrimSpace(body.Background),
		ReferenceImages: referenceImages,
	}
	streaming := r.URL.Query().Get("stream") == "1" || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if streaming {
		run, err := s.Store.CreateChatRun(r.Context(), userID, conversationID, modelSlug, false, userMessage.ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "chat_run_create_failed", "图片生成任务创建失败，请稍后重试")
			return
		}
		_, _ = s.Store.AppendChatRunEvent(r.Context(), run.ID, "run_started", map[string]any{
			"runId": run.ID,
			"run":   run,
		})
		_, _ = s.Store.AppendChatRunEvent(r.Context(), run.ID, "user", map[string]any{
			"message": userMessage,
		})
		_, _ = s.Store.AppendChatRunEvent(r.Context(), run.ID, "image_pending", map[string]any{
			"message": "正在分析图片需求",
			"model":   modelSlug,
		})
		s.startChatImageRunGeneration(run.ID, userID, conversationID, modelSlug, imageRequest, nextTitle, account, r.URL.Path, startedAt)
		status = "ok"
		s.streamChatRunEvents(w, r, userID, run.ID, 0)
		return
	}
	generationCtx := context.WithoutCancel(r.Context())
	imageRequest = s.planImageGenerationRequest(generationCtx, imageRequest)
	payload, err := s.generateImageResponse(generationCtx, modelSlug, imageRequest)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		httpx.Error(w, http.StatusBadGateway, "image_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	generatedAssets, payload := s.persistGeneratedImageAssets(generationCtx, userID, conversationID, payload)
	assistantMessage, err := s.Store.CreateMessage(generationCtx, conversationID, "assistant", imageAssistantMessageText(payload), "", generatedAssets, modelSlug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "image_message_create_failed", err.Error())
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"userMessage":      userMessage,
		"assistantMessage": assistantMessage,
		"generation":       payload,
		"title":            nextTitle,
	})
}

func (s *Server) handleCreateTemporaryChatMessage(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	status := "error"
	errorDetail := ""
	var body createTemporaryMessageRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Content) == "" && len(body.AttachmentIDs) == 0 {
		httpx.Error(w, http.StatusBadRequest, "message_empty", "请输入消息内容或上传附件")
		return
	}
	userID := currentUserID(r)
	modelSlug := body.ModelSlug
	if modelSlug == "" {
		modelSlug = s.Config.DefaultChatRoute
	}
	if body.DeepSearch {
		modelSlug = s.Config.DeepSearchRoute
	}
	defer func() {
		account := currentPrincipal(r).email
		if account == "" {
			account = userID
		}
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "web_chat_temp",
			Path:        r.URL.Path,
			UserID:      userID,
			Account:     account,
			Model:       modelSlug,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	if limitNotice, err := s.getModelLimitNotice(r.Context(), userID, modelSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_limit_check_failed", err.Error())
		return
	} else if limitNotice != nil {
		status = "quota_exhausted"
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "model_limit_exceeded",
			"message": buildModelLimitMessage(limitNotice),
			"limit":   limitNotice,
		})
		return
	}
	assets, err := s.Store.GetAttachmentAssets(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "attachment_lookup_failed", err.Error())
		return
	}
	history := normalizeTemporaryHistory(body.History)
	userMessage := newTransientMessage("user", body.Content, "", assets, modelSlug)
	contextLimit := s.resolveModelContextLimit(r.Context(), userID, modelSlug)
	aiHistory := s.buildAIHistoryFromTemporaryHistory(r.Context(), userID, history, 0)
	remainingImages := maxAIVisionImagesPerRequest - countAIMessageImages(aiHistory)
	aiHistory = append(aiHistory, s.buildAIHistoryEntryWithAttachments(r.Context(), userID, "user", body.Content, assets, &remainingImages))
	aiHistory = trimAIHistoryToContextLimit(aiHistory, contextLimit)
	showVisibleReasoning := body.DeepSearch
	streaming := r.URL.Query().Get("stream") == "1" || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
			return
		}
		sendChatStreamEvent(w, map[string]any{"type": "user", "message": userMessage})
		flusher.Flush()
		result, sources, err := s.generateTemporaryResponse(r.Context(), modelSlug, aiHistory, showVisibleReasoning)
		if err != nil {
			status = classifyUpstreamErrorStatus(err)
			errorDetail = summarizeErrorDetail(err)
			sendChatStreamEvent(w, map[string]any{"type": "error", "message": sanitizeUserFacingChatError(err)})
			flusher.Flush()
			return
		}
		if len(sources) > 0 {
			sendChatStreamEvent(w, map[string]any{"type": "search_sources", "sources": sources})
			flusher.Flush()
		}
		assistantMessage := newTransientMessageWithSources("assistant", result.Content, result.ReasoningContent, nil, sources, modelSlug)
		if strings.TrimSpace(result.ReasoningContent) != "" {
			sendChatStreamEvent(w, map[string]any{"type": "assistant_reasoning", "reasoning": result.ReasoningContent})
			flusher.Flush()
		}
		sendChatStreamEvent(w, map[string]any{"type": "assistant_delta", "delta": result.Content})
		flusher.Flush()
		sendChatStreamEvent(w, map[string]any{
			"type":             "done",
			"assistantMessage": assistantMessage,
			"title":            maybeConversationTitle("新聊天", firstUserPromptFromTemporaryHistory(history, body.Content)),
		})
		flusher.Flush()
		status = "ok"
		return
	}
	result, sources, err := s.generateTemporaryResponse(r.Context(), modelSlug, aiHistory, showVisibleReasoning)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		httpx.Error(w, http.StatusBadGateway, "upstream_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	status = "ok"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"userMessage":      userMessage,
		"assistantMessage": newTransientMessageWithSources("assistant", result.Content, result.ReasoningContent, nil, sources, modelSlug),
		"title":            maybeConversationTitle("新聊天", firstUserPromptFromTemporaryHistory(history, body.Content)),
	})
}

func (s *Server) handleCreateTemporaryChatImage(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	status := "error"
	errorDetail := ""
	var body createTemporaryChatImageRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		httpx.Error(w, http.StatusBadRequest, "image_prompt_required", "请输入绘图提示词")
		return
	}
	userID := currentUserID(r)
	modelSlug := strings.TrimSpace(body.ModelSlug)
	if modelSlug == "" {
		modelSlug = defaultPhotoRoute
	}
	defer func() {
		account := currentPrincipal(r).email
		if account == "" {
			account = userID
		}
		s.recordAPIStat(r.Context(), apiStatEntry{
			Source:      "web_chat_temp",
			Path:        r.URL.Path,
			UserID:      userID,
			Account:     account,
			Model:       modelSlug,
			Status:      status,
			LatencyMs:   time.Since(startedAt).Milliseconds(),
			ErrorDetail: errorDetail,
		})
	}()
	if limitNotice, err := s.getModelLimitNotice(r.Context(), userID, modelSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_limit_check_failed", err.Error())
		return
	} else if limitNotice != nil {
		status = "quota_exhausted"
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "model_limit_exceeded",
			"message": buildModelLimitMessage(limitNotice),
			"limit":   limitNotice,
		})
		return
	}
	requestAssets, err := s.Store.GetAttachmentAssets(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "attachment_lookup_failed", err.Error())
		return
	}
	referenceImages, err := s.loadImageReferenceInputs(r.Context(), userID, body.AttachmentIDs)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "image_reference_load_failed", err.Error())
		return
	}
	imageRequest := imageGenerationRequest{
		Model:           modelSlug,
		Prompt:          buildTemporaryImagePrompt(body.History, prompt),
		Size:            strings.TrimSpace(body.Size),
		N:               body.N,
		Quality:         strings.TrimSpace(body.Quality),
		Background:      strings.TrimSpace(body.Background),
		ReferenceImages: referenceImages,
	}
	imageRequest = s.planImageGenerationRequest(r.Context(), imageRequest)
	payload, err := s.generateImageResponse(r.Context(), modelSlug, imageRequest)
	if err != nil {
		status = classifyUpstreamErrorStatus(err)
		errorDetail = summarizeErrorDetail(err)
		httpx.Error(w, http.StatusBadGateway, "image_generation_failed", sanitizeUserFacingChatError(err))
		return
	}
	assets, payload := s.persistGeneratedImageAssets(r.Context(), userID, "", payload)
	status = "ok"
	httpx.JSON(w, http.StatusOK, map[string]any{
		"userMessage":      newTransientMessage("user", prompt, "", requestAssets, modelSlug),
		"assistantMessage": newTransientMessage("assistant", imageAssistantMessageText(payload), "", assets, modelSlug),
		"generation":       payload,
		"title":            maybeConversationTitle("新聊天", firstUserPromptFromTemporaryHistory(normalizeTemporaryHistory(body.History), prompt)),
	})
}

func (s *Server) handleAttachmentUploadInit(w http.ResponseWriter, r *http.Request) {
	var body attachmentUploadInitRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	body.FileName = sanitizeFileName(body.FileName)
	body.MimeType = strings.TrimSpace(body.MimeType)
	if body.FileName == "" || body.MimeType == "" || body.SizeBytes <= 0 {
		httpx.Error(w, http.StatusBadRequest, "upload_invalid", "文件名、文件类型和文件大小不能为空")
		return
	}
	if body.SizeBytes > 25*1024*1024 {
		httpx.Error(w, http.StatusBadRequest, "upload_too_large", "文件大小不能超过 25MB")
		return
	}
	userID := currentUserID(r)
	token, err := auth.GenerateCSRFToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "upload_token_failed", err.Error())
		return
	}
	objectKey := fmt.Sprintf("users/%s/%d-%s", userID, time.Now().UnixNano(), body.FileName)
	attachment, err := s.Store.CreateAttachment(r.Context(), userID, body.ConversationID, objectKey, s.Config.MinIOBucket, body.FileName, body.MimeType, body.SizeBytes)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "attachment_create_failed", err.Error())
		return
	}
	if err := s.Redis.Set(r.Context(), attachmentUploadSessionKey(attachment.ID), fmt.Sprintf("%s:%s", userID, token), 15*time.Minute).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "upload_session_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"attachment": attachment,
		"upload": map[string]any{
			"url":              fmt.Sprintf("/chat/attachments/%s/upload?token=%s", attachment.ID, token),
			"method":           http.MethodPut,
			"expiresInSeconds": 900,
		},
	})
}

func (s *Server) handleAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	attachmentID := chi.URLParam(r, "id")
	token := r.URL.Query().Get("token")
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "upload_token_missing", "上传凭证缺失")
		return
	}
	expected, err := s.Redis.Get(r.Context(), attachmentUploadSessionKey(attachmentID)).Result()
	if err != nil || expected != fmt.Sprintf("%s:%s", userID, token) {
		httpx.Error(w, http.StatusUnauthorized, "upload_token_invalid", "上传凭证无效或已过期")
		return
	}
	attachment, err := s.Store.GetAttachmentByID(r.Context(), userID, attachmentID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "attachment_not_found", err.Error())
		return
	}
	if attachment.Status == "ready" {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	info, err := s.MinIO.PutObject(r.Context(), attachment.Bucket, attachment.ObjectKey, r.Body, r.ContentLength, minio.PutObjectOptions{
		ContentType: attachment.MimeType,
	})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "object_upload_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"sizeBytes": info.Size,
	})
}

func (s *Server) handleAttachmentComplete(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	attachmentID := chi.URLParam(r, "id")
	attachment, err := s.Store.GetAttachmentByID(r.Context(), userID, attachmentID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "attachment_not_found", err.Error())
		return
	}
	if attachment.Status == "ready" {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"attachment": map[string]any{
				"id":       attachment.ID,
				"fileName": attachment.FileName,
				"mimeType": attachment.MimeType,
				"status":   attachment.Status,
			},
		})
		return
	}
	if _, err := s.MinIO.StatObject(r.Context(), attachment.Bucket, attachment.ObjectKey, minio.StatObjectOptions{}); err != nil {
		httpx.Error(w, http.StatusBadRequest, "upload_incomplete", "附件文件尚未上传完成")
		return
	}
	extractedText := s.extractAttachmentText(r.Context(), attachment)
	if err := s.Store.CompleteAttachment(r.Context(), userID, attachmentID, extractedText); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "attachment_complete_failed", err.Error())
		return
	}
	_, _ = s.Redis.Del(r.Context(), attachmentUploadSessionKey(attachmentID)).Result()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"attachment": map[string]any{
			"id":       attachment.ID,
			"fileName": attachment.FileName,
			"mimeType": attachment.MimeType,
			"status":   "ready",
		},
	})
}

func (s *Server) generateAssistantMessage(ctx context.Context, userID string, conversationID string, modelSlug string) (string, *store.Message, error) {
	history, err := s.Store.ListMessages(ctx, userID, conversationID)
	if err != nil {
		return "", nil, err
	}
	reply, err := s.generateAIResponse(ctx, modelSlug, s.buildAIHistoryForConversation(ctx, userID, conversationID, modelSlug, history))
	if err != nil {
		return "", nil, err
	}
	assistantMessage, err := s.Store.CreateMessage(ctx, conversationID, "assistant", reply, "", nil, modelSlug)
	if err != nil {
		return "", nil, err
	}
	return reply, assistantMessage, nil
}

func (s *Server) generateAssistantMessageWithReasoning(ctx context.Context, userID string, conversationID string, modelSlug string, includeVisibleReasoning bool) (aiResponseEnvelope, *store.Message, error) {
	history, err := s.Store.ListMessages(ctx, userID, conversationID)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	result, err := s.generateAIResponseEnvelope(ctx, modelSlug, s.buildAIHistoryForConversation(ctx, userID, conversationID, modelSlug, history), includeVisibleReasoning)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	assistantMessage, err := s.Store.CreateMessage(ctx, conversationID, "assistant", result.Content, result.ReasoningContent, nil, modelSlug)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	return result, assistantMessage, nil
}

func buildAIHistory(history []store.Message) []aiMessage {
	return buildAIHistoryWithContextLimit(history, 0)
}

func buildAIHistoryWithContextLimit(history []store.Message, contextLimit int) []aiMessage {
	aiHistory := make([]aiMessage, 0, len(history))
	for _, item := range history {
		aiHistory = append(aiHistory, buildAIHistoryEntry(item.Role, item.Content, item.Attachments))
	}
	return trimAIHistoryToContextLimit(aiHistory, contextLimit)
}

func (s *Server) buildAIHistoryForConversation(ctx context.Context, userID string, conversationID string, modelSlug string, history []store.Message) []aiMessage {
	aiHistory := s.buildAIHistoryWithVision(ctx, userID, history, s.resolveModelContextLimit(ctx, userID, modelSlug))
	if memoryMessage, ok := s.buildAccountMemoryMessage(ctx, userID, conversationID); ok {
		aiHistory = append([]aiMessage{memoryMessage}, aiHistory...)
	}
	return aiHistory
}

const (
	maxAIVisionImageBytes       int64 = 10 * 1024 * 1024
	maxAIVisionImagesPerRequest       = 8
)

func (s *Server) buildAIHistoryWithVision(ctx context.Context, userID string, history []store.Message, contextLimit int) []aiMessage {
	remainingImages := maxAIVisionImagesPerRequest
	reversed := make([]aiMessage, 0, len(history))
	for index := len(history) - 1; index >= 0; index-- {
		item := history[index]
		entry := s.buildAIHistoryEntryWithAttachments(ctx, userID, item.Role, item.Content, item.Attachments, &remainingImages)
		reversed = append(reversed, entry)
	}
	aiHistory := make([]aiMessage, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		aiHistory = append(aiHistory, reversed[index])
	}
	return trimAIHistoryToContextLimit(aiHistory, contextLimit)
}

func (s *Server) buildAIHistoryFromTemporaryHistory(ctx context.Context, userID string, history []temporaryChatMessage, contextLimit int) []aiMessage {
	remainingImages := maxAIVisionImagesPerRequest
	reversed := make([]aiMessage, 0, len(history))
	for index := len(history) - 1; index >= 0; index-- {
		item := history[index]
		reversed = append(reversed, s.buildAIHistoryEntryWithAttachments(ctx, userID, item.Role, item.Content, item.Attachments, &remainingImages))
	}
	aiHistory := make([]aiMessage, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		aiHistory = append(aiHistory, reversed[index])
	}
	return trimAIHistoryToContextLimit(aiHistory, contextLimit)
}

func (s *Server) buildAIHistoryEntryWithAttachments(ctx context.Context, userID string, role string, content string, attachments []store.MessageAsset, remainingImages *int) aiMessage {
	content = strings.TrimSpace(content)
	if len(attachments) == 0 {
		return aiMessage{Role: normalizeProviderRole(role), Content: content}
	}
	var builder strings.Builder
	builder.WriteString(content)
	visionAttachments := make([]aiMessageAttachment, 0)
	for _, asset := range attachments {
		if strings.TrimSpace(asset.ID) == "" {
			appendAttachmentMarker(&builder, asset.FileName, asset.MimeType)
			continue
		}
		attachment, err := s.Store.GetAttachmentByID(ctx, userID, asset.ID)
		if err != nil || attachment == nil || attachment.Status != "ready" {
			appendAttachmentMarker(&builder, asset.FileName, asset.MimeType)
			continue
		}
		if strings.HasPrefix(strings.ToLower(attachment.MimeType), "image/") {
			appendAttachmentMarker(&builder, attachment.FileName, attachment.MimeType)
			if remainingImages == nil || *remainingImages <= 0 || attachment.SizeBytes > maxAIVisionImageBytes {
				continue
			}
			object, err := s.MinIO.GetObject(ctx, attachment.Bucket, attachment.ObjectKey, minio.GetObjectOptions{})
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(object, maxAIVisionImageBytes+1))
			closeErr := object.Close()
			if readErr != nil || closeErr != nil || int64(len(data)) > maxAIVisionImageBytes || len(data) == 0 {
				continue
			}
			visionAttachments = append(visionAttachments, aiMessageAttachment{
				FileName: attachment.FileName,
				MimeType: attachment.MimeType,
				Data:     data,
			})
			*remainingImages = *remainingImages - 1
			continue
		}
		extracted := strings.TrimSpace(attachment.ExtractedText)
		if extracted == "" {
			appendAttachmentMarker(&builder, attachment.FileName, attachment.MimeType)
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("[Attachment: ")
		builder.WriteString(attachment.FileName)
		builder.WriteString("]\n")
		builder.WriteString(clipRunes(extracted, 12000))
	}
	return aiMessage{
		Role:        normalizeProviderRole(role),
		Content:     strings.TrimSpace(builder.String()),
		Attachments: visionAttachments,
	}
}

func appendAttachmentMarker(builder *strings.Builder, fileName string, mimeType string) {
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString(fmt.Sprintf("[Attachment: %s (%s)]", fileName, mimeType))
}

func countAIMessageImages(messages []aiMessage) int {
	count := 0
	for _, message := range messages {
		count += len(message.Attachments)
	}
	return count
}

func (s *Server) buildAccountMemoryMessage(ctx context.Context, userID string, conversationID string) (aiMessage, bool) {
	settings, err := s.Store.GetUserSettings(ctx, userID)
	if err != nil || settings == nil || !settings.ChatHistoryEnabled || !settings.MemoryEnabled {
		return aiMessage{}, false
	}
	messages, err := s.Store.ListRecentUserMemoryMessages(ctx, userID, conversationID, 24)
	if err != nil || len(messages) == 0 {
		return aiMessage{}, false
	}
	return buildAccountMemorySystemMessage(messages)
}

func buildAccountMemorySystemMessage(messages []store.Message) (aiMessage, bool) {
	lines := make([]string, 0, len(messages))
	for _, item := range messages {
		if item.Role != "user" {
			continue
		}
		content := normalizeAccountMemoryContent(item.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s", content))
	}
	if len(lines) == 0 {
		return aiMessage{}, false
	}
	return aiMessage{
		Role: "system",
		Content: "以下是同一账号在其他对话中留下的账号级记忆，仅用于理解用户曾经表达过的信息、偏好、项目背景和长期上下文。只有与当前问题相关时才参考；不要逐字复述这些记忆，不要主动暴露“我读取了记忆”；如果当前对话与旧记忆冲突，以当前对话为准。\n" +
			strings.Join(lines, "\n"),
	}, true
}

func normalizeAccountMemoryContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	content = strings.ReplaceAll(content, "\u00a0", " ")
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > 180 {
		content = strings.TrimSpace(string(runes[:180])) + "..."
	}
	return content
}

func trimAIHistoryToContextLimit(messages []aiMessage, contextLimit int) []aiMessage {
	if contextLimit <= 0 || len(messages) == 0 {
		return messages
	}
	selected := make([]aiMessage, 0, len(messages))
	used := 0
	omitted := 0
	for index := len(messages) - 1; index >= 0; index-- {
		item := messages[index]
		cost := estimateMessageContextLength(item)
		if len(selected) == 0 || used+cost <= contextLimit {
			selected = append([]aiMessage{item}, selected...)
			used += cost
			continue
		}
		omitted++
	}
	if omitted > 0 {
		notice := aiMessage{
			Role: "system",
			Content: fmt.Sprintf(
				"后台为当前模型设置了 %d 的上下文输入长度限制，已省略更早的 %d 条历史消息。请基于保留的最近上下文完整回答；上下文限制只影响输入历史，不要把最终文章或回答中途截断。",
				contextLimit,
				omitted,
			),
		}
		selected = append([]aiMessage{notice}, selected...)
	}
	return selected
}

func estimateMessageContextLength(message aiMessage) int {
	return len([]rune(message.Role)) + len([]rune(message.Content)) + 16
}

func maybeConversationTitle(currentTitle string, content string) string {
	if !isPlaceholderConversationTitle(currentTitle) {
		return ""
	}
	title := normalizeConversationTitle(content)
	if title == "" || title == "新聊天" {
		return ""
	}
	return title
}

func isPlaceholderConversationTitle(title string) bool {
	switch strings.TrimSpace(title) {
	case "", "新聊天", "新对话", "临时聊天", "Untitled chat":
		return true
	default:
		return false
	}
}

func sendChatStreamEvent(w http.ResponseWriter, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}

func extractGeneratedImageAssets(payload map[string]any) []store.MessageAsset {
	data, ok := payload["data"].([]any)
	if !ok || len(data) == 0 {
		return nil
	}
	out := make([]store.MessageAsset, 0, len(data))
	for index, item := range data {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rawURL, ok := entry["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
			out = append(out, store.MessageAsset{
				ID:       fmt.Sprintf("generated-image-%d", index+1),
				FileName: fmt.Sprintf("generated-image-%d.png", index+1),
				MimeType: "image/png",
				URL:      rawURL,
			})
			continue
		}
		if b64, ok := entry["b64_json"].(string); ok && strings.TrimSpace(b64) != "" {
			out = append(out, store.MessageAsset{
				ID:       fmt.Sprintf("generated-image-%d", index+1),
				FileName: fmt.Sprintf("generated-image-%d.png", index+1),
				MimeType: "image/png",
				URL:      "data:image/png;base64," + b64,
			})
		}
	}
	return out
}

func imageAssistantMessageText(payload map[string]any) string {
	if assets := extractGeneratedImageAssets(payload); len(assets) > 0 {
		return fmt.Sprintf("已为你生成 %d 张图片，请查看下方结果。", len(assets))
	}
	return "图片已生成，请查看结果。"
}

func buildAIHistoryFromTemporaryHistory(history []temporaryChatMessage) []aiMessage {
	aiHistory := make([]aiMessage, 0, len(history))
	for _, item := range history {
		aiHistory = append(aiHistory, buildAIHistoryEntry(item.Role, item.Content, item.Attachments))
	}
	return aiHistory
}

func buildAIHistoryEntry(role string, content string, attachments []store.MessageAsset) aiMessage {
	content = strings.TrimSpace(content)
	if len(attachments) > 0 {
		var builder strings.Builder
		builder.WriteString(content)
		for _, asset := range attachments {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(fmt.Sprintf("[Attachment: %s (%s)]", asset.FileName, asset.MimeType))
		}
		content = builder.String()
	}
	return aiMessage{
		Role:    normalizeProviderRole(role),
		Content: content,
	}
}

func normalizeConversationTitle(content string) string {
	title := strings.TrimSpace(content)
	if title == "" {
		return "新聊天"
	}
	title = strings.ReplaceAll(title, "\u00a0", " ")
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, " \t\r\n-•*#>\"'“”‘’`()[]{}")
	title = stripConversationPrefix(title)
	title = stripConversationActionPrefix(title)
	title = firstMeaningfulLine(title)
	title = strings.Trim(title, " \t\r\n-•*#>\"'“”‘’`()[]{}。，！？!?；;")
	if title == "" {
		return "新聊天"
	}
	runes := []rune(title)
	if len(runes) > 26 {
		title = strings.TrimSpace(string(runes[:26]))
		title = strings.TrimRight(title, "，,、:：;； ")
	}
	if title == "" {
		return "新聊天"
	}
	return title
}

func stripConversationPrefix(title string) string {
	prefixes := []string{
		"请帮我",
		"帮我",
		"给我",
		"麻烦你",
		"请你",
		"请",
		"我想让你",
		"我想请你",
		"我想",
		"可以帮我",
		"能不能帮我",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(title, prefix) && len([]rune(title)) > len([]rune(prefix))+3 {
			return strings.TrimSpace(strings.TrimPrefix(title, prefix))
		}
	}
	return title
}

func stripConversationActionPrefix(title string) string {
	prefixes := []string{
		"系统梳理一下",
		"梳理一下",
		"梳理下",
		"整理",
		"整理一下",
		"整理下",
		"总结一下",
		"总结下",
		"概括一下",
		"概括下",
		"分析一下",
		"分析下",
		"解释一下",
		"解释下",
		"介绍一下",
		"介绍下",
		"说一下",
		"说下",
		"看一下",
		"看下",
		"写一个",
		"写一下",
		"写下",
		"做一个",
		"生成一个",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(title, prefix) && len([]rune(title)) > len([]rune(prefix))+4 {
			return strings.TrimSpace(strings.TrimPrefix(title, prefix))
		}
	}
	return title
}

func firstMeaningfulLine(title string) string {
	separators := []rune{'\n', '\r', '。', '！', '？', '!', '?', '；', ';', '，', ',', '：', ':'}
	index := strings.IndexAny(title, string(separators))
	if index <= 0 {
		return title
	}
	candidate := strings.TrimSpace(title[:index])
	if len([]rune(candidate)) >= 5 {
		return candidate
	}
	return title
}

func normalizeTemporaryHistory(history []temporaryChatMessage) []temporaryChatMessage {
	if len(history) == 0 {
		return nil
	}
	if len(history) > 60 {
		history = history[len(history)-60:]
	}
	out := make([]temporaryChatMessage, 0, len(history))
	for _, item := range history {
		if strings.TrimSpace(item.Content) == "" && len(item.Attachments) == 0 {
			continue
		}
		item.Role = normalizeProviderRole(item.Role)
		out = append(out, item)
	}
	return out
}

func buildTemporaryImagePrompt(history []temporaryChatMessage, prompt string) string {
	history = normalizeTemporaryHistory(history)
	if len(history) == 0 {
		return prompt
	}
	if len(history) > 6 {
		history = history[len(history)-6:]
	}
	lines := make([]string, 0, len(history))
	for _, item := range history {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", temporaryRoleLabel(item.Role), item.Content))
	}
	if len(lines) == 0 {
		return prompt
	}
	return "请结合以下对话上下文理解用户想要的图片风格与内容，再完成最后的图片需求。\n\n上下文：\n" +
		strings.Join(lines, "\n") +
		"\n\n最终图片需求：\n" + prompt
}

func temporaryRoleLabel(role string) string {
	switch normalizeProviderRole(role) {
	case "assistant":
		return "助手"
	case "system":
		return "系统"
	case "developer":
		return "开发者"
	default:
		return "用户"
	}
}

func firstUserPromptFromTemporaryHistory(history []temporaryChatMessage, fallback string) string {
	for _, item := range history {
		if normalizeProviderRole(item.Role) == "user" && strings.TrimSpace(item.Content) != "" {
			return item.Content
		}
	}
	return fallback
}

func newTransientMessage(role string, content string, reasoningContent string, attachments []store.MessageAsset, modelSlug string) *store.Message {
	return newTransientMessageWithSources(role, content, reasoningContent, attachments, nil, modelSlug)
}

func newTransientMessageWithSources(role string, content string, reasoningContent string, attachments []store.MessageAsset, sources []store.SearchSource, modelSlug string) *store.Message {
	return &store.Message{
		ID:               uuid.NewString(),
		Role:             role,
		Content:          content,
		ReasoningContent: reasoningContent,
		Attachments:      attachments,
		Sources:          sources,
		ModelSlug:        modelSlug,
		CreatedAt:        time.Now().UTC(),
	}
}

func attachmentUploadSessionKey(attachmentID string) string {
	return "attachment:upload:" + attachmentID
}

func sanitizeFileName(fileName string) string {
	fileName = strings.TrimSpace(filepath.Base(fileName))
	fileName = strings.ReplaceAll(fileName, " ", "-")
	if fileName == "" || fileName == "." || fileName == string(filepath.Separator) {
		return "upload.bin"
	}
	return fileName
}

const maxImageReferenceBytes int64 = 25 * 1024 * 1024

func (s *Server) loadImageReferenceInputs(ctx context.Context, userID string, attachmentIDs []string) ([]imageReferenceInput, error) {
	if len(attachmentIDs) == 0 {
		return nil, nil
	}
	references := make([]imageReferenceInput, 0, len(attachmentIDs))
	for _, attachmentID := range dedupeStrings(attachmentIDs) {
		attachmentID = strings.TrimSpace(attachmentID)
		if attachmentID == "" {
			continue
		}
		attachment, err := s.Store.GetAttachmentByID(ctx, userID, attachmentID)
		if err != nil {
			return nil, err
		}
		if attachment.Status != "ready" || !strings.HasPrefix(strings.ToLower(attachment.MimeType), "image/") {
			continue
		}
		if attachment.SizeBytes > maxImageReferenceBytes {
			return nil, fmt.Errorf("参考图 %s 超过 25MB", attachment.FileName)
		}
		object, err := s.MinIO.GetObject(ctx, attachment.Bucket, attachment.ObjectKey, minio.GetObjectOptions{})
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(object, maxImageReferenceBytes+1))
		closeErr := object.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(data)) > maxImageReferenceBytes {
			return nil, fmt.Errorf("参考图 %s 超过 25MB", attachment.FileName)
		}
		if len(data) == 0 {
			continue
		}
		references = append(references, imageReferenceInput{
			FileName: attachment.FileName,
			MimeType: attachment.MimeType,
			Data:     data,
		})
	}
	return references, nil
}

func (s *Server) extractAttachmentText(ctx context.Context, attachment *store.AttachmentRecord) string {
	if attachment == nil {
		return ""
	}
	if !isTextLikeAttachment(attachment.FileName, attachment.MimeType) {
		if strings.HasPrefix(attachment.MimeType, "image/") {
			return fmt.Sprintf("[Image upload: %s]", attachment.FileName)
		}
		return fmt.Sprintf("[Attachment upload: %s (%s)]", attachment.FileName, attachment.MimeType)
	}
	object, err := s.MinIO.GetObject(ctx, attachment.Bucket, attachment.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Sprintf("[Attachment upload: %s (%s)]", attachment.FileName, attachment.MimeType)
	}
	defer object.Close()
	raw, err := io.ReadAll(io.LimitReader(object, 64*1024))
	if err != nil || len(raw) == 0 {
		return fmt.Sprintf("[Attachment upload: %s (%s)]", attachment.FileName, attachment.MimeType)
	}
	return string(raw)
}

func isTextLikeAttachment(fileName string, mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/javascript", "application/x-javascript", "application/csv", "text/csv":
		return true
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".txt", ".md", ".json", ".csv", ".xml", ".yaml", ".yml", ".log", ".js", ".ts", ".tsx", ".jsx", ".go", ".py", ".java", ".sql", ".html", ".css":
		return true
	default:
		return false
	}
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.ListAPIKeys(r.Context(), currentUserID(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "api_keys_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"apiKeys": keys})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body createAPIKeyRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if body.RateLimitPerMinute <= 0 {
		body.RateLimitPerMinute = 60
	}
	raw, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "api_key_generate_failed", err.Error())
		return
	}
	item, err := s.Store.CreateAPIKey(r.Context(), currentUserID(r), body.Name, body.Scopes, body.RateLimitPerMinute, prefix, hash)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "api_key_create_failed", err.Error())
		return
	}
	item.RevealedKey = raw
	httpx.JSON(w, http.StatusCreated, item)
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeAPIKey(r.Context(), currentUserID(r), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "api_key_revoke_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeveloperUsage(w http.ResponseWriter, r *http.Request) {
	keys, _ := s.Store.ListAPIKeys(r.Context(), currentUserID(r))
	quota, _ := s.Store.GetQuotaSummary(r.Context(), currentUserID(r))
	infiniteCodeUsage := s.buildInfiniteCodeUsage(r.Context(), currentUserID(r))
	httpx.JSON(w, http.StatusOK, map[string]any{
		"apiKeyCount":  len(keys),
		"quota":        quota,
		"infiniteCode": infiniteCodeUsage,
	})
}

func (s *Server) buildInfiniteCodeUsage(ctx context.Context, userID string) map[string]any {
	planCode, err := s.Store.ResolveUserPlanCode(ctx, userID)
	if err != nil || strings.TrimSpace(planCode) == "" {
		planCode = "free"
	}
	config, err := s.Store.GetInfiniteCodeQuotaConfig(ctx)
	if err != nil {
		config = map[string]store.InfiniteCodeQuotaPlan{}
	}
	planQuota, ok := config[planCode]
	if !ok {
		planQuota = store.InfiniteCodeQuotaPlan{Credits: 0, ResetHours: 24}
	}
	anchor := time.Now().UTC()
	if sub, err := s.Store.GetSubscriptionByUser(ctx, userID); err == nil && !sub.StartedAt.IsZero() {
		anchor = sub.StartedAt.UTC()
	}
	windowStart, nextResetAt := infiniteCodeWindowBounds(anchor, planQuota.ResetHours, time.Now().UTC())
	entries, _ := s.loadAPIStats(ctx, 300)
	used := 0
	for _, entry := range entries {
		if entry.UserID != userID || entry.Source != "developer_api" || entry.Status != "ok" {
			continue
		}
		if entry.Path != "/v1/chat/completions" && entry.Path != "/v1/messages" {
			continue
		}
		if entry.Timestamp.Before(windowStart) {
			continue
		}
		used++
	}
	remaining := planQuota.Credits - used
	if remaining < 0 {
		remaining = 0
	}
	planName := planCode
	if plan, err := s.Store.GetPlan(ctx, planCode); err == nil && strings.TrimSpace(plan.Name) != "" {
		planName = plan.Name
	}
	return map[string]any{
		"planCode":      planCode,
		"planName":      planName,
		"credits":       planQuota.Credits,
		"used":          used,
		"remaining":     remaining,
		"resetHours":    planQuota.ResetHours,
		"windowStartAt": windowStart,
		"nextResetAt":   nextResetAt,
		"hint":          fmt.Sprintf("%s 每 %d 小时恢复 %d 次额度", planName, planQuota.ResetHours, planQuota.Credits),
	}
}

func infiniteCodeWindowBounds(anchor time.Time, resetHours int, now time.Time) (time.Time, time.Time) {
	if resetHours <= 0 {
		resetHours = 24
	}
	if anchor.IsZero() {
		anchor = now
	}
	duration := time.Duration(resetHours) * time.Hour
	windowStart := anchor
	if now.Before(anchor) {
		for windowStart.After(now) {
			windowStart = windowStart.Add(-duration)
		}
		return windowStart, windowStart.Add(duration)
	}
	elapsed := now.Sub(anchor)
	cycles := int(elapsed / duration)
	windowStart = anchor.Add(time.Duration(cycles) * duration)
	nextResetAt := windowStart.Add(duration)
	return windowStart, nextResetAt
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	sub, err := s.Store.GetSubscriptionByUser(r.Context(), currentUserID(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "subscription_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, sub)
}

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var body createOrderRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	amountCents := 0
	description := ""
	if body.Type == "plan" {
		plan, err := s.Store.GetPlan(r.Context(), body.PlanCode)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "plan_not_found", err.Error())
			return
		}
		amountCents = plan.PriceCents
		description = plan.Name
	} else {
		amountCents = int(body.RechargeAmount * 100)
		description = "Infinite Code 算力额度"
	}
	order, err := s.Store.CreatePaymentOrder(r.Context(), currentUserID(r), body.Type, body.PlanCode, body.RechargeAmount, amountCents, description, body.SubMethod, map[string]any{
		"createdVia": "web",
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "order_create_failed", err.Error())
		return
	}
	ifpayConfig, err := s.Store.GetIFPayConfig(r.Context())
	merchantID, _ := ifpayConfig["merchantId"].(string)
	secretKey, _ := ifpayConfig["secretKey"].(string)
	if err != nil || merchantID == "" || secretKey == "" {
		_ = s.Store.UpdatePaymentOrderStatus(r.Context(), order.ID, "awaiting_configuration", "", "", map[string]any{"reason": "ifpay_not_configured"})
		order.Status = "awaiting_configuration"
		httpx.JSON(w, http.StatusCreated, map[string]any{
			"order": order,
			"payment": map[string]any{
				"configured": false,
				"message":    "IF-Pay 尚未配置完成",
			},
		})
		return
	}
	_ = s.Store.UpdatePaymentOrderStatus(r.Context(), order.ID, "processing", "", "", map[string]any{"provider": "ifpay"})
	order.Status = "processing"
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"order": order,
		"payment": map[string]any{
			"configured": true,
			"provider":   "ifpay",
			"message":    "请使用已配置的 IF-Pay 商户参数完成支付请求",
		},
	})
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	order, err := s.Store.GetPaymentOrder(r.Context(), currentUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "order_not_found", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, order)
}

func (s *Server) handleRedeemPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := s.Store.PreviewRedeemCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "redeem_code_not_found", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, preview)
}

func (s *Server) handleRedeemClaim(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &payload); err != nil {
		badRequest(w, err)
		return
	}
	current := currentPrincipal(r)
	if current.kind != "user" || current.subjectID == "" {
		preview, err := s.Store.PreviewRedeemCode(r.Context(), payload.Code)
		if err != nil {
			httpx.Error(w, http.StatusNotFound, "redeem_code_not_found", err.Error())
			return
		}
		action := "login_required"
		if preview.AccountType == "no_account" {
			action = "register_required"
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"claimed": false,
			"action":  action,
			"preview": preview,
		})
		return
	}
	preview, err := s.Store.ClaimRedeemCode(r.Context(), payload.Code, current.subjectID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "redeem_claim_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, preview)
}

func (s *Server) handleGetUserSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Store.GetUserSettings(r.Context(), currentUserID(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateUserSettings(w http.ResponseWriter, r *http.Request) {
	var body updateSettingsRequest
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	memoryEnabled := true
	if existing, err := s.Store.GetUserSettings(r.Context(), currentUserID(r)); err == nil && existing != nil {
		memoryEnabled = existing.MemoryEnabled
	}
	if body.MemoryEnabled != nil {
		memoryEnabled = *body.MemoryEnabled
	}
	err := s.Store.UpdateUserSettings(r.Context(), currentUserID(r), store.UserSettings{
		Theme:              body.Theme,
		Language:           body.Language,
		DeepSearchDefault:  body.DeepSearchDefault,
		SelectedModelSlug:  body.SelectedModelSlug,
		ChatHistoryEnabled: body.ChatHistoryEnabled,
		MemoryEnabled:      memoryEnabled,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleExportUserData(w http.ResponseWriter, r *http.Request) {
	payload, err := s.Store.ExportUserData(r.Context(), currentUserID(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "export_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="infinite-ai-export-%s.json"`, time.Now().Format("20060102150405")))
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleClearChats(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ClearUserChats(r.Context(), currentUserID(r)); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteUserAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteUserAccount(r.Context(), currentUserID(r)); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "account_delete_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) getModelLimitNotice(ctx context.Context, userID string, modelSlug string) (*modelLimitNotice, error) {
	limits, err := s.Store.GetModelMembershipLimits(ctx)
	if err != nil {
		return nil, err
	}
	if len(limits) == 0 {
		return nil, nil
	}
	planCode, err := s.Store.ResolveUserPlanCode(ctx, userID)
	if err != nil {
		return nil, err
	}
	planLimits, ok := limits[planCode]
	if !ok {
		return nil, nil
	}
	limit, ok := planLimits[modelSlug]
	if !ok {
		return nil, nil
	}
	used, err := s.Store.CountSuccessfulModelResponsesSince(ctx, userID, modelSlug, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	used += s.countTemporarySuccessfulModelResponsesSince(ctx, userID, modelSlug, time.Now().Add(-24*time.Hour))
	if limit > 0 && used < limit {
		return nil, nil
	}
	modelName := modelSlug
	if route, routeErr := s.Store.FindActiveModelRoute(ctx, modelSlug, false); routeErr == nil && strings.TrimSpace(route.Name) != "" {
		modelName = route.Name
	}
	reason := "unavailable"
	if limit > 0 {
		reason = "exhausted"
	}
	return &modelLimitNotice{
		Reason:      reason,
		ModelSlug:   modelSlug,
		ModelName:   modelName,
		PlanCode:    planCode,
		Limit:       limit,
		Used:        used,
		WindowHours: 24,
	}, nil
}

func (s *Server) resolveModelContextLimit(ctx context.Context, userID string, modelSlug string) int {
	limits, err := s.Store.GetModelContextLimits(ctx)
	if err != nil || limits == nil {
		return 0
	}
	if userLimits, ok := limits.Users[userID]; ok {
		if limit := userLimits[modelSlug]; limit > 0 {
			return limit
		}
	}
	planCode, err := s.Store.ResolveUserPlanCode(ctx, userID)
	if err == nil {
		if planLimits, ok := limits.Plans[planCode]; ok {
			if limit := planLimits[modelSlug]; limit > 0 {
				return limit
			}
		}
	}
	if limit := limits.Models[modelSlug]; limit > 0 {
		return limit
	}
	if limits.Default > 0 {
		return limits.Default
	}
	return 0
}

func buildModelLimitMessage(limit *modelLimitNotice) string {
	if limit == nil {
		return "当前模型暂时无法使用，请稍后重试"
	}
	if limit.Reason == "unavailable" {
		return fmt.Sprintf("%s当前暂不支持使用 %s，请升级套餐后再试", planCodeLabel(limit.PlanCode), limit.ModelName)
	}
	return fmt.Sprintf("%s在 24 小时内可使用 %s %d 次，你的可用次数已用完，请升级或等待 24 小时后再试", planCodeLabel(limit.PlanCode), limit.ModelName, limit.Limit)
}

func (s *Server) countTemporarySuccessfulModelResponsesSince(ctx context.Context, userID string, modelSlug string, since time.Time) int {
	entries, err := s.loadAPIStats(ctx, 300)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.Source != "web_chat_temp" || entry.Status != "ok" {
			continue
		}
		if entry.UserID != userID || entry.Model != modelSlug {
			continue
		}
		if entry.Timestamp.Before(since) {
			continue
		}
		count++
	}
	return count
}

func planCodeLabel(planCode string) string {
	switch strings.TrimSpace(planCode) {
	case "go":
		return "Go 版"
	case "plus":
		return "Plus 版"
	case "pro_basic":
		return "Pro 基础版"
	case "pro_max":
		return "Pro 满血版"
	default:
		return "免费版"
	}
}
