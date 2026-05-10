package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/minio/minio-go/v7"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) handleListActiveChatRuns(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	conversationID := chi.URLParam(r, "id")
	if _, err := s.Store.GetConversation(r.Context(), userID, conversationID); err != nil {
		httpx.Error(w, http.StatusNotFound, "conversation_not_found", "聊天不存在或已被删除")
		return
	}
	runs, err := s.Store.ListActiveChatRuns(r.Context(), userID, conversationID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "chat_runs_load_failed", "聊天运行状态加载失败，请稍后重试")
		return
	}
	if s.finishCancelingRuns(runs) {
		runs, err = s.Store.ListActiveChatRuns(r.Context(), userID, conversationID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "chat_runs_load_failed", "聊天运行状态加载失败，请稍后重试")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) finishCancelingRuns(runs []store.ChatRun) bool {
	finished := false
	for _, run := range runs {
		if run.CancelRequested || strings.EqualFold(run.Status, "canceling") {
			s.finishCanceledRun(run.ID, run.ConversationID, run.ModelSlug, "", "")
			finished = true
		}
	}
	return finished
}

func (s *Server) handleGetChatRun(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	runID := chi.URLParam(r, "id")
	run, err := s.Store.GetChatRun(r.Context(), userID, runID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "chat_run_not_found", "聊天运行任务不存在或已结束")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleChatRunEvents(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	runID := chi.URLParam(r, "id")
	if err := s.Store.EnsureChatRunOwned(r.Context(), userID, runID); err != nil {
		httpx.Error(w, http.StatusNotFound, "chat_run_not_found", "聊天运行任务不存在或已结束")
		return
	}
	afterSeq, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("after")), 10, 64)
	s.streamChatRunEvents(w, r, userID, runID, afterSeq)
}

func (s *Server) handleCancelChatRun(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	runID := chi.URLParam(r, "id")
	if err := s.Store.MarkChatRunCancelRequested(r.Context(), userID, runID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "chat_run_not_found", "聊天运行任务不存在或已结束")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "chat_run_cancel_failed", "停止输出失败，请稍后重试")
		return
	}
	run, err := s.Store.GetChatRun(r.Context(), userID, runID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "chat_run_cancel_failed", "停止输出失败，请稍后重试")
		return
	}
	s.cancelActiveChatRun(runID)
	if !store.IsChatRunTerminal(run.Status) {
		s.finishCanceledRun(runID, run.ConversationID, run.ModelSlug, "", "")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChatAssetDownload(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	assetID := chi.URLParam(r, "id")
	asset, err := s.Store.GetAttachmentByID(r.Context(), userID, assetID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "asset_not_found", "图片文件不存在或已被删除")
		return
	}
	object, err := s.MinIO.GetObject(r.Context(), asset.Bucket, asset.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "asset_download_failed", "图片文件读取失败，请稍后重试")
		return
	}
	defer object.Close()
	w.Header().Set("Content-Type", asset.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, sanitizeFileName(asset.FileName)))
	if asset.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(asset.SizeBytes, 10))
	}
	_, _ = io.Copy(w, object)
}

func (s *Server) handleGetChatArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := s.Store.GetChatArtifact(r.Context(), currentUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "artifact_not_found", "代码预览不存在或已被删除")
		return
	}
	versions, _ := s.Store.ListChatArtifactVersions(r.Context(), currentUserID(r), artifact.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"artifact": artifact, "versions": versions})
}

func (s *Server) handleCreateChatArtifactVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Files []store.ArtifactFile `json:"files"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	files := normalizeArtifactFiles(body.Files)
	if len(files) == 0 {
		httpx.Error(w, http.StatusBadRequest, "artifact_files_empty", "请至少保留一个可预览文件")
		return
	}
	version, err := s.Store.CreateChatArtifactVersion(r.Context(), currentUserID(r), chi.URLParam(r, "id"), files)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "artifact_version_save_failed", "代码版本保存失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"version": version})
}

func (s *Server) handleDownloadChatArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := s.Store.GetChatArtifact(r.Context(), currentUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "artifact_not_found", "代码预览不存在或已被删除")
		return
	}
	fileName := sanitizeFileName(artifact.Title)
	if fileName == "" {
		fileName = "infinite-ai-artifact"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, fileName))
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()
	for _, file := range artifact.Files {
		path := strings.TrimLeft(filepath.ToSlash(file.Path), "/")
		if path == "" {
			continue
		}
		writer, err := zipWriter.Create(path)
		if err != nil {
			continue
		}
		_, _ = writer.Write([]byte(file.Content))
	}
}

func (s *Server) streamChatRunEvents(w http.ResponseWriter, r *http.Request, userID string, runID string, afterSeq int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "stream_unsupported", "当前环境不支持流式输出")
		return
	}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	idleDeadline := time.NewTimer(85 * time.Second)
	defer idleDeadline.Stop()
	lastSeq := afterSeq
	lastHeartbeat := time.Now()
	for {
		events, err := s.Store.ListChatRunEventsAfter(r.Context(), userID, runID, lastSeq, 100)
		if err != nil {
			sendChatStreamEvent(w, map[string]any{"type": "error", "message": "聊天运行事件读取失败，请刷新后重试"})
			flusher.Flush()
			return
		}
		terminal := false
		for _, event := range events {
			if event.Seq > lastSeq {
				lastSeq = event.Seq
			}
			sendPersistedChatRunEvent(w, event)
			flusher.Flush()
			if isTerminalRunEvent(event.EventType) {
				terminal = true
			}
		}
		if terminal {
			return
		}
		if time.Since(lastHeartbeat) >= 10*time.Second {
			lastHeartbeat = time.Now()
			sendChatStreamEvent(w, map[string]any{"type": "ping", "runId": runID, "seq": lastSeq, "message": "运行中"})
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-idleDeadline.C:
			sendChatStreamEvent(w, map[string]any{"type": "ping", "runId": runID, "seq": lastSeq})
			flusher.Flush()
			return
		case <-ticker.C:
		}
	}
}

func sendPersistedChatRunEvent(w http.ResponseWriter, event store.ChatRunEvent) {
	payload := map[string]any{}
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["type"] = event.EventType
	payload["seq"] = event.Seq
	payload["runId"] = event.RunID
	sendChatStreamEvent(w, payload)
}

func isTerminalRunEvent(eventType string) bool {
	switch eventType {
	case "done", "error", "canceled":
		return true
	default:
		return false
	}
}

func (s *Server) registerChatRunCancel(runID string, cancel context.CancelFunc) {
	s.runCancelMu.Lock()
	defer s.runCancelMu.Unlock()
	s.runCancels[runID] = cancel
}

func (s *Server) unregisterChatRunCancel(runID string) {
	s.runCancelMu.Lock()
	defer s.runCancelMu.Unlock()
	delete(s.runCancels, runID)
}

func (s *Server) cancelActiveChatRun(runID string) {
	s.runCancelMu.Lock()
	cancel := s.runCancels[runID]
	s.runCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) startChatRunGeneration(runID string, userID string, conversationID string, modelSlug string, includeVisibleReasoning bool, nextTitle string, account string, statPath string, startedAt time.Time) {
	ctx, cancel := context.WithCancel(context.Background())
	s.registerChatRunCancel(runID, cancel)
	go func() {
		defer s.unregisterChatRunCancel(runID)
		defer cancel()
		s.runChatGeneration(ctx, runID, userID, conversationID, modelSlug, includeVisibleReasoning, nextTitle, account, statPath, startedAt)
	}()
}

func (s *Server) runChatGeneration(ctx context.Context, runID string, userID string, conversationID string, modelSlug string, includeVisibleReasoning bool, nextTitle string, account string, statPath string, startedAt time.Time) {
	if !includeVisibleReasoning {
		var emittedContent strings.Builder
		result, sources, err := s.generateRunResponseStream(ctx, runID, userID, conversationID, modelSlug, func(delta string) error {
			if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
				return context.Canceled
			}
			emittedContent.WriteString(delta)
			_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "assistant_delta", map[string]any{"delta": delta})
			return nil
		})
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || s.Store.ChatRunCancelRequested(context.Background(), runID) {
				s.finishCanceledRun(runID, conversationID, modelSlug, "", emittedContent.String())
				return
			}
			message := sanitizeUserFacingChatError(err)
			assistantMessage, _ := s.Store.CreateMessage(context.Background(), conversationID, "assistant", message, "", nil, modelSlug)
			_ = s.Store.FailChatRun(context.Background(), runID, message)
			_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{
				"message":          message,
				"assistantMessage": assistantMessage,
			})
			s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
			return
		}
		if emittedContent.Len() == 0 && strings.TrimSpace(result.Content) != "" {
			if ok := s.emitRunTextChunks(ctx, runID, "assistant_delta", "delta", result.Content, &emittedContent); !ok {
				s.finishCanceledRun(runID, conversationID, modelSlug, "", emittedContent.String())
				return
			}
		}
		if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
			s.finishCanceledRun(runID, conversationID, modelSlug, "", emittedContent.String())
			return
		}
		content := result.Content
		if strings.TrimSpace(content) == "" {
			content = emittedContent.String()
		}
		assistantMessage, err := s.Store.CreateMessageWithExtras(context.Background(), conversationID, "assistant", content, "", nil, sources, nil, modelSlug)
		if err != nil {
			_ = s.Store.FailChatRun(context.Background(), runID, "助手消息保存失败")
			_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{"message": "助手消息保存失败，请刷新后重试"})
			s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
			return
		}
		artifacts := s.createArtifactsForAssistantMessage(context.Background(), userID, conversationID, assistantMessage.ID, content)
		if len(artifacts) > 0 {
			assistantMessage.Artifacts = artifacts
			_ = s.Store.AttachArtifactsToMessage(context.Background(), assistantMessage.ID, artifacts)
		}
		_ = s.Store.CompleteChatRun(context.Background(), runID, assistantMessage.ID)
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "done", map[string]any{
			"assistantMessage": assistantMessage,
			"title":            nextTitle,
		})
		return
	}

	result, sources, err := s.generateRunResponse(ctx, runID, userID, conversationID, modelSlug, includeVisibleReasoning)
	if err != nil {
		if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
			s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
			return
		}
		message := sanitizeUserFacingChatError(err)
		assistantMessage, _ := s.Store.CreateMessage(context.Background(), conversationID, "assistant", message, "", nil, modelSlug)
		assistantID := ""
		if assistantMessage != nil {
			assistantID = assistantMessage.ID
		}
		_ = s.Store.FailChatRun(context.Background(), runID, message)
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{
			"message":          message,
			"assistantMessage": assistantMessage,
		})
		s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
		if assistantID == "" {
			return
		}
		return
	}

	reasoning, content := result.ReasoningContent, result.Content
	if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
		s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
		return
	}
	var emittedReasoning strings.Builder
	var emittedContent strings.Builder
	if strings.TrimSpace(reasoning) != "" {
		if ok := s.emitRunTextChunks(ctx, runID, "assistant_reasoning", "reasoning", reasoning, &emittedReasoning); !ok {
			s.finishCanceledRun(runID, conversationID, modelSlug, emittedReasoning.String(), emittedContent.String())
			return
		}
	}
	if ok := s.emitRunTextChunks(ctx, runID, "assistant_delta", "delta", content, &emittedContent); !ok {
		s.finishCanceledRun(runID, conversationID, modelSlug, emittedReasoning.String(), emittedContent.String())
		return
	}
	if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
		s.finishCanceledRun(runID, conversationID, modelSlug, emittedReasoning.String(), emittedContent.String())
		return
	}

	assistantMessage, err := s.Store.CreateMessageWithExtras(context.Background(), conversationID, "assistant", content, reasoning, nil, sources, nil, modelSlug)
	if err != nil {
		_ = s.Store.FailChatRun(context.Background(), runID, "助手消息保存失败")
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{"message": "助手消息保存失败，请刷新后重试"})
		s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
		return
	}
	artifacts := s.createArtifactsForAssistantMessage(context.Background(), userID, conversationID, assistantMessage.ID, content)
	if len(artifacts) > 0 {
		assistantMessage.Artifacts = artifacts
		_ = s.Store.AttachArtifactsToMessage(context.Background(), assistantMessage.ID, artifacts)
	}
	_ = s.Store.CompleteChatRun(context.Background(), runID, assistantMessage.ID)
	_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "done", map[string]any{
		"assistantMessage": assistantMessage,
		"title":            nextTitle,
	})
}

func (s *Server) startChatImageRunGeneration(runID string, userID string, conversationID string, modelSlug string, request imageGenerationRequest, nextTitle string, account string, statPath string, startedAt time.Time) {
	ctx, cancel := context.WithCancel(context.Background())
	s.registerChatRunCancel(runID, cancel)
	go func() {
		defer s.unregisterChatRunCancel(runID)
		defer cancel()
		s.runChatImageGeneration(ctx, runID, userID, conversationID, modelSlug, request, nextTitle, account, statPath, startedAt)
	}()
}

func (s *Server) runChatImageGeneration(ctx context.Context, runID string, userID string, conversationID string, modelSlug string, request imageGenerationRequest, nextTitle string, account string, statPath string, startedAt time.Time) {
	request = s.planImageGenerationRequest(ctx, request)
	if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
		s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
		return
	}
	_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "image_pending", map[string]any{
		"message": "正在生成照片",
		"model":   modelSlug,
	})
	payload, err := s.generateImageResponse(ctx, modelSlug, request)
	if err != nil {
		if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
			s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
			return
		}
		message := sanitizeUserFacingChatError(err)
		assistantMessage, _ := s.Store.CreateMessage(context.Background(), conversationID, "assistant", message, "", nil, modelSlug)
		_ = s.Store.FailChatRun(context.Background(), runID, message)
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{
			"message":          message,
			"assistantMessage": assistantMessage,
		})
		s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
		return
	}
	if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
		s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
		return
	}
	assets, payload := s.persistGeneratedImageAssets(context.Background(), userID, conversationID, payload)
	if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
		s.finishCanceledRun(runID, conversationID, modelSlug, "", "")
		return
	}
	assistantMessage, err := s.Store.CreateMessage(context.Background(), conversationID, "assistant", imageAssistantMessageText(payload), "", assets, modelSlug)
	if err != nil {
		_ = s.Store.FailChatRun(context.Background(), runID, "图片消息保存失败")
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "error", map[string]any{"message": "图片消息保存失败，请刷新后重试"})
		s.recordAsyncRunFailure(userID, account, modelSlug, statPath, startedAt, err)
		return
	}
	_ = s.Store.CompleteChatRun(context.Background(), runID, assistantMessage.ID)
	_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "done", map[string]any{
		"assistantMessage": assistantMessage,
		"generation":       payload,
		"title":            nextTitle,
	})
}

func (s *Server) recordAsyncRunFailure(userID string, account string, modelSlug string, statPath string, startedAt time.Time, err error) {
	if err == nil {
		return
	}
	if strings.TrimSpace(account) == "" {
		account = userID
	}
	if strings.TrimSpace(statPath) == "" {
		statPath = "/chat/runs"
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	s.recordAPIStat(context.Background(), apiStatEntry{
		Source:      "web_chat",
		Path:        statPath,
		UserID:      userID,
		Account:     account,
		Model:       modelSlug,
		Status:      classifyUpstreamErrorStatus(err),
		LatencyMs:   time.Since(startedAt).Milliseconds(),
		ErrorDetail: summarizeErrorDetail(err),
	})
}

func (s *Server) finishCanceledRun(runID string, conversationID string, modelSlug string, partialReasoning string, partialContent string) {
	claimed, err := s.Store.TryCancelChatRun(context.Background(), runID)
	if err != nil || !claimed {
		return
	}
	content := strings.TrimSpace(partialContent)
	if content == "" {
		content = "已停止输出。"
	} else {
		content += "\n\n（已停止输出）"
	}
	assistantMessage, _ := s.Store.CreateMessage(context.Background(), conversationID, "assistant", content, strings.TrimSpace(partialReasoning), nil, modelSlug)
	assistantID := ""
	if assistantMessage != nil {
		assistantID = assistantMessage.ID
	}
	_ = s.Store.AttachChatRunAssistantMessage(context.Background(), runID, assistantID)
	_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "canceled", map[string]any{
		"message":          "已停止输出。",
		"assistantMessage": assistantMessage,
	})
}

func (s *Server) emitRunTextChunks(ctx context.Context, runID string, eventType string, field string, value string, emitted *strings.Builder) bool {
	for _, chunk := range splitChunks(value, 28) {
		if ctx.Err() != nil || s.Store.ChatRunCancelRequested(context.Background(), runID) {
			return false
		}
		if emitted != nil {
			emitted.WriteString(chunk)
		}
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, eventType, map[string]any{field: chunk})
		select {
		case <-ctx.Done():
			return false
		case <-time.After(35 * time.Millisecond):
		}
	}
	return true
}

func (s *Server) generateRunResponse(ctx context.Context, runID string, userID string, conversationID string, modelSlug string, includeVisibleReasoning bool) (aiResponseEnvelope, []store.SearchSource, error) {
	history, err := s.Store.ListMessages(ctx, userID, conversationID)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	aiHistory := s.buildAIHistoryForConversation(ctx, userID, conversationID, modelSlug, history)
	result, sources, err := s.generateResponseWithOptionalSearch(ctx, modelSlug, aiHistory, includeVisibleReasoning, latestUserPrompt(history), func(sources []store.SearchSource) {
		_, _ = s.Store.AppendChatRunEvent(context.Background(), runID, "search_sources", map[string]any{"sources": sources})
	})
	if err != nil {
		return aiResponseEnvelope{}, sources, err
	}
	return result, sources, nil
}

func (s *Server) generateRunResponseStream(ctx context.Context, runID string, userID string, conversationID string, modelSlug string, emitDelta func(string) error) (aiResponseEnvelope, []store.SearchSource, error) {
	history, err := s.Store.ListMessages(ctx, userID, conversationID)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	aiHistory := s.buildAIHistoryForConversation(ctx, userID, conversationID, modelSlug, history)
	result, err := s.generateAIResponseEnvelopeStream(ctx, modelSlug, aiHistory, false, emitDelta)
	if err != nil {
		return aiResponseEnvelope{}, nil, err
	}
	return result, nil, nil
}

func (s *Server) generateTemporaryResponse(ctx context.Context, modelSlug string, aiHistory []aiMessage, includeVisibleReasoning bool) (aiResponseEnvelope, []store.SearchSource, error) {
	return s.generateResponseWithOptionalSearch(ctx, modelSlug, aiHistory, includeVisibleReasoning, latestAIUserPrompt(aiHistory), nil)
}

func (s *Server) generateResponseWithOptionalSearch(ctx context.Context, modelSlug string, aiHistory []aiMessage, includeVisibleReasoning bool, searchQuery string, emitSources func([]store.SearchSource)) (aiResponseEnvelope, []store.SearchSource, error) {
	var sources []store.SearchSource
	if includeVisibleReasoning {
		searchConfig := s.enabledDeepSearchConfig(ctx)
		if searchConfig != nil {
			result, officialSources, err := s.generateOpenAIWebSearchResponse(ctx, modelSlug, aiHistory, searchConfig.ResultCount)
			if err != nil && ctx.Err() == nil {
				result, officialSources, err = s.generateAnthropicWebSearchResponse(ctx, modelSlug, aiHistory, searchConfig.ResultCount)
			}
			if err == nil {
				if len(officialSources) > 0 {
					if emitSources != nil {
						emitSources(officialSources)
					}
				}
				return result, officialSources, nil
			}
			if ctx.Err() != nil {
				return aiResponseEnvelope{}, nil, ctx.Err()
			}
		}
		aiHistory, sources = s.applyDeepSearchFallback(ctx, aiHistory, searchQuery, searchConfig, emitSources)
	}
	result, err := s.generateAIResponseEnvelope(ctx, modelSlug, aiHistory, includeVisibleReasoning)
	if err != nil {
		return aiResponseEnvelope{}, sources, err
	}
	return result, sources, nil
}

func latestUserPrompt(history []store.Message) string {
	for index := len(history) - 1; index >= 0; index-- {
		if normalizeProviderRole(history[index].Role) == "user" {
			return strings.TrimSpace(history[index].Content)
		}
	}
	return ""
}

func latestAIUserPrompt(history []aiMessage) string {
	for index := len(history) - 1; index >= 0; index-- {
		if normalizeProviderRole(history[index].Role) == "user" {
			return strings.TrimSpace(history[index].Content)
		}
	}
	return ""
}

func (s *Server) applyDeepSearchFallback(ctx context.Context, aiHistory []aiMessage, searchQuery string, searchConfig *store.SearchProviderConfig, emitSources func([]store.SearchSource)) ([]aiMessage, []store.SearchSource) {
	sources := s.collectDeepSearchSourcesWithConfig(ctx, searchQuery, searchConfig)
	if len(sources) > 0 {
		if emitSources != nil {
			emitSources(sources)
		}
		return append([]aiMessage{buildDeepSearchSourceMessage(sources)}, aiHistory...), sources
	}
	return append([]aiMessage{buildDeepSearchUnavailableMessage()}, aiHistory...), nil
}

func buildDeepSearchSourceMessage(sources []store.SearchSource) aiMessage {
	var builder strings.Builder
	builder.WriteString("以下是 Infinite-AI 已完成的联网检索结果。请只在与问题相关时引用这些来源，并在最终回答中用 [1]、[2] 这样的编号标注依据。不要把来源卡片内容混入深度思路摘要。")
	for _, source := range sources {
		builder.WriteString("\n\n[")
		builder.WriteString(strconv.Itoa(source.Index))
		builder.WriteString("] ")
		builder.WriteString(source.Title)
		if source.URL != "" {
			builder.WriteString("\n链接：")
			builder.WriteString(source.URL)
		}
		if source.Snippet != "" {
			builder.WriteString("\n摘要：")
			builder.WriteString(source.Snippet)
		}
	}
	return aiMessage{Role: "system", Content: builder.String()}
}

func buildDeepSearchUnavailableMessage() aiMessage {
	return aiMessage{Role: "system", Content: "本轮已开启联网检索，但官方原生 Web Search 不可用，且本地 SearXNG 没有返回可引用来源。请不要声称已经完成联网核验；如果问题依赖实时信息，请在回答中明确说明当前没有可用检索来源。"}
}

func normalizeArtifactFiles(files []store.ArtifactFile) []store.ArtifactFile {
	out := make([]store.ArtifactFile, 0, len(files))
	for _, file := range files {
		path := strings.TrimSpace(filepath.ToSlash(file.Path))
		path = strings.TrimLeft(path, "/")
		content := file.Content
		if path == "" {
			continue
		}
		if len([]rune(content)) > 300_000 {
			content = string([]rune(content)[:300_000])
		}
		out = append(out, store.ArtifactFile{
			Path:     path,
			Language: strings.TrimSpace(file.Language),
			Content:  content,
		})
	}
	return out
}

func prettyJSON(value any) string {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return string(raw)
}
