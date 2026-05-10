package app

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/minio/minio-go/v7"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) handleGetConversationShare(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	conversationID := chi.URLParam(r, "id")
	_, collaborationLimit, _ := s.Store.ResolveShareCollaborationLimit(r.Context(), userID)
	share, err := s.Store.GetConversationShareForOwner(r.Context(), userID, conversationID)
	if err != nil {
		if store.IsNotFound(err) {
			httpx.JSON(w, http.StatusOK, map[string]any{
				"share": map[string]any{
					"id":                   "",
					"conversationId":       conversationID,
					"isActive":             false,
					"requireAccessCode":    false,
					"accessCode":           "",
					"collaborationEnabled": false,
					"collaborationLimit":   collaborationLimit,
					"currentCollaborators": 0,
					"shareURL":             "",
				},
			})
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "share_load_failed", "分享设置加载失败，请稍后重试")
		return
	}
	collaboratorCount, _ := s.Store.CountConversationShareCollaborators(r.Context(), share.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"share": buildOwnerConversationSharePayload(r, share, collaborationLimit, collaboratorCount),
	})
}

func (s *Server) handleUpsertConversationShare(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	conversationID := chi.URLParam(r, "id")
	var body struct {
		Enabled              bool   `json:"enabled"`
		RequireAccessCode    bool   `json:"requireAccessCode"`
		AccessCode           string `json:"accessCode"`
		CollaborationEnabled bool   `json:"collaborationEnabled"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	_, collaborationLimit, err := s.Store.ResolveShareCollaborationLimit(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "share_limit_load_failed", "分享协作额度加载失败，请稍后重试")
		return
	}
	if body.CollaborationEnabled && collaborationLimit <= 0 {
		httpx.Error(w, http.StatusForbidden, "share_collaboration_unavailable", "当前套餐暂不支持开启协作")
		return
	}
	if body.CollaborationEnabled && strings.TrimSpace(body.AccessCode) == "" {
		if existing, loadErr := s.Store.GetConversationShareForOwner(r.Context(), userID, conversationID); loadErr != nil || strings.TrimSpace(existing.AccessCode) == "" {
			httpx.Error(w, http.StatusBadRequest, "share_collaboration_code_required", "请输入协作码")
			return
		}
	}
	share, err := s.Store.UpsertConversationShare(
		r.Context(),
		userID,
		conversationID,
		body.Enabled,
		body.AccessCode,
		body.CollaborationEnabled,
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "share_save_failed", "分享设置保存失败，请稍后重试")
		return
	}
	collaboratorCount, _ := s.Store.CountConversationShareCollaborators(r.Context(), share.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"share": buildOwnerConversationSharePayload(r, share, collaborationLimit, collaboratorCount),
	})
}

func (s *Server) handleGetPublicConversationShare(w http.ResponseWriter, r *http.Request) {
	share, conversation, messages, err := s.loadPublicConversationShare(w, r)
	if err != nil {
		return
	}
	principal := currentPrincipal(r)
	owner, _ := s.Store.GetUserByID(r.Context(), share.UserID)
	ownerPlanCode, collaborationLimit, _ := s.Store.ResolveShareCollaborationLimit(r.Context(), share.UserID)
	collaboratorCount, _ := s.Store.CountConversationShareCollaborators(r.Context(), share.ID)
	viewerIsOwner := principal.kind == "user" && principal.subjectID == share.UserID
	viewerCanCollaborate := principal.kind == "user" && share.CollaborationEnabled && collaborationLimit > 0
	viewerJoinedCollaboration := viewerIsOwner
	if principal.kind == "user" && strings.TrimSpace(principal.subjectID) != "" && !viewerIsOwner {
		viewerJoinedCollaboration, _ = s.Store.IsConversationShareCollaborator(r.Context(), share.ID, principal.subjectID)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"share": map[string]any{
			"id":                        share.ID,
			"title":                     conversation.Title,
			"conversationId":            conversation.ID,
			"modelSlug":                 conversation.ModelSlug,
			"deepSearch":                conversation.DeepSearch,
			"requireAccessCode":         share.RequireAccessCode,
			"collaborationEnabled":      share.CollaborationEnabled && collaborationLimit > 0,
			"collaborationLimit":        collaborationLimit,
			"currentCollaborators":      collaboratorCount,
			"viewerCanCollaborate":      viewerCanCollaborate,
			"viewerIsOwner":             viewerIsOwner,
			"viewerJoinedCollaboration": viewerJoinedCollaboration,
			"ownerPlanCode":             ownerPlanCode,
			"ownerDisplayName":          strings.TrimSpace(owner.DisplayName),
		},
		"messages": s.publicShareMessages(messages, share.ID),
	})
}

func (s *Server) handleGetPublicConversationShareAsset(w http.ResponseWriter, r *http.Request) {
	share, _, _, err := s.loadPublicConversationShare(w, r)
	if err != nil {
		return
	}
	assetID := chi.URLParam(r, "assetId")
	asset, err := s.Store.GetSharedConversationAttachment(r.Context(), share.ID, assetID)
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

func (s *Server) handleJoinSharedConversationCollaboration(w http.ResponseWriter, r *http.Request) {
	principal := currentPrincipal(r)
	if principal.kind != "user" || strings.TrimSpace(principal.subjectID) == "" {
		httpx.Error(w, http.StatusUnauthorized, "login_required", "登录后才可以参与协作")
		return
	}
	var body struct {
		CollaborationCode string `json:"collaborationCode"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	share, _, _, err := s.loadPublicConversationShare(w, r)
	if err != nil {
		return
	}
	if _, err := s.authorizeSharedConversationCollaboration(w, r, share, principal, body.CollaborationCode); err != nil {
		return
	}
	collaboratorCount, _ := s.Store.CountConversationShareCollaborators(r.Context(), share.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"share": map[string]any{
			"id":                   share.ID,
			"collaborationEnabled": share.CollaborationEnabled,
			"currentCollaborators": collaboratorCount,
		},
	})
}

func (s *Server) handleCreateSharedConversationMessage(w http.ResponseWriter, r *http.Request) {
	principal := currentPrincipal(r)
	if principal.kind != "user" || strings.TrimSpace(principal.subjectID) == "" {
		httpx.Error(w, http.StatusUnauthorized, "login_required", "登录后才可以参与协作")
		return
	}
	var body struct {
		Content           string `json:"content"`
		CollaborationCode string `json:"collaborationCode"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	share, conversation, _, err := s.loadPublicConversationShare(w, r)
	if err != nil {
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		httpx.Error(w, http.StatusBadRequest, "message_content_required", "请输入消息内容")
		return
	}
	if _, err := s.authorizeSharedConversationCollaboration(w, r, share, principal, body.CollaborationCode); err != nil {
		return
	}
	modelSlug := strings.TrimSpace(conversation.ModelSlug)
	if modelSlug == "" {
		modelSlug = "infinite-ai-standard"
	}
	if limitNotice, err := s.getModelLimitNotice(r.Context(), share.UserID, modelSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_limit_check_failed", err.Error())
		return
	} else if limitNotice != nil {
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "model_limit_exceeded",
			"message": buildModelLimitMessage(limitNotice),
			"limit":   limitNotice,
		})
		return
	}
	userMessage, err := s.Store.CreateMessage(r.Context(), conversation.ID, "user", content, "", nil, modelSlug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "message_create_failed", "协作消息保存失败，请稍后重试")
		return
	}
	nextTitle := maybeConversationTitle(conversation.Title, content)
	if nextTitle != "" {
		_ = s.Store.RenameConversation(r.Context(), conversation.ID, nextTitle)
		conversation.Title = nextTitle
	}
	result, assistantMessage, err := s.generateAssistantMessageWithReasoning(r.Context(), share.UserID, conversation.ID, modelSlug, conversation.DeepSearch)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "assistant_generate_failed", sanitizeUserFacingChatError(err))
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"userMessage":      userMessage,
		"assistantMessage": assistantMessage,
		"title":            conversation.Title,
		"reasoning":        result.ReasoningContent,
	})
}

func (s *Server) authorizeSharedConversationCollaboration(w http.ResponseWriter, r *http.Request, share *store.ConversationShare, principal principal, collaborationCode string) (int, error) {
	if share == nil {
		httpx.Error(w, http.StatusNotFound, "share_not_found", "分享不存在或已关闭")
		return 0, fmt.Errorf("share not found")
	}
	if !share.CollaborationEnabled {
		httpx.Error(w, http.StatusForbidden, "share_collaboration_disabled", "当前分享未开启协作")
		return 0, fmt.Errorf("share collaboration disabled")
	}
	_, collaborationLimit, err := s.Store.ResolveShareCollaborationLimit(r.Context(), share.UserID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "share_limit_load_failed", "分享协作额度加载失败，请稍后重试")
		return 0, err
	}
	if collaborationLimit <= 0 {
		httpx.Error(w, http.StatusForbidden, "share_collaboration_unavailable", "当前套餐暂不支持协作")
		return 0, fmt.Errorf("share collaboration unavailable")
	}
	if principal.subjectID == share.UserID {
		return collaborationLimit, nil
	}
	if strings.TrimSpace(collaborationCode) == "" {
		httpx.Error(w, http.StatusForbidden, "share_collaboration_code_required", "请输入协作码")
		return 0, fmt.Errorf("share collaboration code required")
	}
	allowed, err := s.Store.ConversationShareAccessAllowed(r.Context(), share.ID, collaborationCode)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "share_collaboration_check_failed", "协作校验失败，请稍后重试")
		return 0, err
	}
	if !allowed {
		httpx.Error(w, http.StatusForbidden, "share_collaboration_code_invalid", "请输入正确的协作码")
		return 0, fmt.Errorf("share collaboration code invalid")
	}
	if err := s.Store.EnsureConversationShareCollaborator(r.Context(), share.ID, principal.subjectID, collaborationLimit); err != nil {
		if err == store.ErrShareCollaboratorLimitReached {
			httpx.Error(w, http.StatusForbidden, "share_collaboration_limit_reached", "当前协作人数已满")
			return 0, err
		}
		httpx.Error(w, http.StatusInternalServerError, "share_collaborator_join_failed", "加入协作失败，请稍后重试")
		return 0, err
	}
	return collaborationLimit, nil
}

func (s *Server) loadPublicConversationShare(w http.ResponseWriter, r *http.Request) (*store.ConversationShare, *store.Conversation, []store.Message, error) {
	shareID := chi.URLParam(r, "id")
	share, err := s.Store.GetActiveConversationShareByID(r.Context(), shareID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "share_not_found", "分享不存在或已关闭")
		return nil, nil, nil, err
	}
	conversation, err := s.Store.GetConversationByID(r.Context(), share.ConversationID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "conversation_not_found", "聊天不存在或已被删除")
		return nil, nil, nil, err
	}
	messages, err := s.Store.ListMessagesByConversationID(r.Context(), conversation.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "messages_load_failed", "聊天内容加载失败，请稍后重试")
		return nil, nil, nil, err
	}
	return share, conversation, messages, nil
}

func buildOwnerConversationSharePayload(r *http.Request, share *store.ConversationShare, collaborationLimit int, collaboratorCount int) map[string]any {
	if share == nil {
		return map[string]any{}
	}
	userBaseURL := requestBaseURLForPort(r, "1001", "")
	shareURL := strings.TrimRight(userBaseURL, "/") + "/share/" + share.ID
	return map[string]any{
		"id":                   share.ID,
		"conversationId":       share.ConversationID,
		"isActive":             share.IsActive,
		"requireAccessCode":    share.RequireAccessCode,
		"accessCode":           share.AccessCode,
		"collaborationEnabled": share.CollaborationEnabled && collaborationLimit > 0,
		"collaborationLimit":   collaborationLimit,
		"currentCollaborators": collaboratorCount,
		"shareURL":             shareURL,
		"createdAt":            share.CreatedAt,
		"updatedAt":            share.UpdatedAt,
	}
}

func (s *Server) publicShareMessages(messages []store.Message, shareID string) []store.Message {
	out := make([]store.Message, 0, len(messages))
	for _, message := range messages {
		nextMessage := message
		if len(message.Attachments) > 0 {
			nextAttachments := make([]store.MessageAsset, 0, len(message.Attachments))
			for _, asset := range message.Attachments {
				nextAsset := asset
				if asset.ID != "" && strings.HasPrefix(strings.TrimSpace(asset.URL), "/chat/assets/") {
					nextAsset.URL = fmt.Sprintf("/chat/shares/%s/assets/%s", shareID, asset.ID)
				}
				nextAttachments = append(nextAttachments, nextAsset)
			}
			nextMessage.Attachments = nextAttachments
		}
		out = append(out, nextMessage)
	}
	return out
}
