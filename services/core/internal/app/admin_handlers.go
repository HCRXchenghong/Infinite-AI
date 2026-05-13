package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	payload, err := s.Store.DashboardSummary(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "dashboard_load_failed", err.Error())
		return
	}
	if entries, err := s.loadAPIStats(r.Context(), 120); err == nil {
		summary := summarizeAPIStats(entries)
		if stats, ok := payload["stats"].([]map[string]any); ok {
			for idx, stat := range stats {
				if stat["label"] == "系统平均延迟" {
					stats[idx]["value"] = fmt.Sprintf("%dms", summary["avgLatencyMs"])
					stats[idx]["trend"] = fmt.Sprintf("%d recent", summary["totalRequests"])
				}
			}
			payload["stats"] = stats
		}
	}
	httpx.JSON(w, http.StatusOK, payload)
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListUsersForAdmin(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "users_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": items})
}

func (s *Server) handleAdminInviteLinks(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListAffiliateInvitesForAdmin(r.Context(), 20)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "invite_links_load_failed", err.Error())
		return
	}
	userBaseURL := requestBaseURLForPort(r, "1001", s.Config.UserBaseURL)
	for _, item := range items {
		if code, ok := item["code"].(string); ok && code != "" {
			item["link"] = fmt.Sprintf("%s/register?aff=%s", userBaseURL, code)
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": items})
}

func (s *Server) handleAdminRevokeInviteLink(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		httpx.Error(w, http.StatusBadRequest, "invite_code_required", "缺少邀请码")
		return
	}
	if err := s.Store.RevokeAffiliateInviteForAdmin(r.Context(), code); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "invite_revoke_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanCode   string `json:"planCode"`
		ExpiryDate string `json:"expiryDate"`
		Status     string `json:"status"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateUserAdminConfig(r.Context(), chi.URLParam(r, "id"), body.PlanCode, body.ExpiryDate, strings.ToLower(body.Status)); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "user_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteUserAdmin(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "user_delete_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListModelRoutes(r.Context(), true)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "models_load_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"models": items})
}

func (s *Server) handleAdminUpsertModel(w http.ResponseWriter, r *http.Request) {
	var body store.ModelRoute
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if slug := chi.URLParam(r, "slug"); slug != "" {
		body.Slug = slug
	}
	body.Slug = strings.TrimSpace(body.Slug)
	if body.Slug == "" {
		httpx.Error(w, http.StatusBadRequest, "model_slug_required", "请填写模型标识")
		return
	}
	if err := s.Store.UpsertModelRoute(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminDeleteModel(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		httpx.Error(w, http.StatusBadRequest, "model_slug_required", "请填写模型标识")
		return
	}
	if err := s.Store.DeleteModelRoute(r.Context(), slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_delete_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminTestModelRoute(w http.ResponseWriter, r *http.Request) {
	var body store.ModelRoute
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if strings.TrimSpace(body.Protocol) == "" {
		body.Protocol = "openai"
	}
	if strings.TrimSpace(body.ModelType) == "" {
		body.ModelType = "chat"
	}
	if strings.TrimSpace(body.UpstreamModel) == "" && body.ModelType != "image" {
		httpx.Error(w, http.StatusBadRequest, "model_upstream_required", "请先填写上游模型名后再测试")
		return
	}
	if len(body.Endpoints) == 0 {
		httpx.Error(w, http.StatusBadRequest, "model_endpoints_required", "请至少配置一个端点后再测试")
		return
	}
	httpx.JSON(w, http.StatusOK, s.probeModelRoute(r.Context(), body))
}

func (s *Server) handleListPublicModels(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListActiveModelRoutes(r.Context(), false)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "models_load_failed", err.Error())
		return
	}
	filtered := make([]store.ModelRoute, 0, len(items))
	for _, item := range items {
		if item.ModelType == "image" {
			continue
		}
		filtered = append(filtered, item)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"models": filtered})
}

func (s *Server) handleAdminAPIStats(w http.ResponseWriter, r *http.Request) {
	entries, err := s.loadAPIStats(r.Context(), 100)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "api_stats_failed", err.Error())
		return
	}
	logs := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		logs = append(logs, map[string]any{
			"timestamp":   entry.Timestamp.Format("2006-01-02 15:04:05"),
			"source":      entry.Source,
			"path":        entry.Path,
			"userId":      entry.UserID,
			"keyId":       entry.KeyID,
			"account":     entry.Account,
			"model":       entry.Model,
			"status":      entry.Status,
			"latencyMs":   entry.LatencyMs,
			"errorDetail": entry.ErrorDetail,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"summary": summarizeAPIStats(entries),
		"logs":    logs,
	})
}

func (s *Server) handleAdminServiceAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.loadServiceAlerts(r.Context(), 200)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "service_alerts_failed", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(alerts))
	for _, alert := range alerts {
		items = append(items, map[string]any{
			"id":          alert.ID,
			"createdAt":   alert.CreatedAt.Format("2006-01-02 15:04:05"),
			"source":      alert.Source,
			"path":        alert.Path,
			"model":       alert.Model,
			"status":      alert.Status,
			"account":     alert.Account,
			"userId":      alert.UserID,
			"keyId":       alert.KeyID,
			"latencyMs":   alert.LatencyMs,
			"errorDetail": alert.ErrorDetail,
			"readAt":      formatOptionalTime(alert.ReadAt),
			"resolvedAt":  formatOptionalTime(alert.ResolvedAt),
			"resolvedBy":  alert.ResolvedBy,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"alerts": items})
}

func (s *Server) handleAdminReadServiceAlert(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "id")
	now := time.Now().UTC()
	if err := s.updateServiceAlert(r.Context(), alertID, func(alert *serviceAlert) bool {
		if alert == nil || alert.Status == "resolved" {
			return false
		}
		if alert.Status == "unread" {
			alert.Status = "read"
			alert.ReadAt = &now
			return true
		}
		return false
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "service_alert_read_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminResolveServiceAlert(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "id")
	current := currentPrincipal(r)
	now := time.Now().UTC()
	if err := s.updateServiceAlert(r.Context(), alertID, func(alert *serviceAlert) bool {
		if alert == nil || alert.Status == "resolved" {
			return false
		}
		alert.Status = "resolved"
		if alert.ReadAt == nil {
			alert.ReadAt = &now
		}
		alert.ResolvedAt = &now
		if current.email != "" {
			alert.ResolvedBy = current.email
		} else {
			alert.ResolvedBy = current.subjectID
		}
		return true
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "service_alert_resolve_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func formatOptionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func (s *Server) handleAdminMemberStats(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.MemberLogs(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "member_stats_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"logs": items})
}

func (s *Server) handleAdminSystemLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListSystemLogs(r.Context(), 0)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "system_logs_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"logs": items})
}

func (s *Server) handleAdminMembership(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListUsersForAdmin(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"members": items})
}

func (s *Server) handleAdminCreateRedeemCampaign(w http.ResponseWriter, r *http.Request) {
	var body store.RedeemCampaignInput
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	result, err := s.Store.CreateRedeemCampaign(r.Context(), currentAdminID(r), body)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "redeem_campaign_create_failed", err.Error())
		return
	}
	result.Link = fmt.Sprintf("%s/redeem?code=%s", requestBaseURLForPort(r, "1004", s.Config.RedeemBaseURL), result.Code)
	httpx.JSON(w, http.StatusCreated, result)
}

func (s *Server) handleAdminFinance(w http.ResponseWriter, r *http.Request) {
	payload, err := s.Store.FinanceSnapshot(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "finance_failed", err.Error())
		return
	}
	payload["webhookHintURL"] = fmt.Sprintf("%s/webhooks/ifpay", requestBaseURLForPort(r, "1002", s.Config.APIBaseURL))
	httpx.JSON(w, http.StatusOK, payload)
}

func (s *Server) handleAdminUpdateIFPay(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateIFPayConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "ifpay_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	payload, err := s.Store.GlobalSettings(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, payload)
}

func (s *Server) handleAdminUpdateRegisterSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateRegisterEnabled(r.Context(), body.Enabled); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "register_setting_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateAuthSecurity(w http.ResponseWriter, r *http.Request) {
	var body store.AuthSecuritySettings
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateAuthSecuritySettings(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "auth_security_setting_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateEmailGateway(w http.ResponseWriter, r *http.Request) {
	var body store.EmailGatewayConfig
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateEmailGatewayConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "email_gateway_setting_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminTestEmailGateway(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	result, err := s.debugEmailGateway(r.Context(), currentPrincipal(r), strings.TrimSpace(body.Email))
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "email_gateway_test_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (s *Server) handleAdminUpdateSMSGateway(w http.ResponseWriter, r *http.Request) {
	var body store.SMSGatewayConfig
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateSMSGatewayConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "sms_gateway_setting_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminTestSMSGateway(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	result, err := s.debugSMSGateway(r.Context(), currentPrincipal(r), strings.TrimSpace(body.Phone))
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "sms_gateway_test_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (s *Server) handleAdminUpdateModelMembershipLimits(w http.ResponseWriter, r *http.Request) {
	var body map[string]map[string]int
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateModelMembershipLimits(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_membership_limits_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateModelContextLimits(w http.ResponseWriter, r *http.Request) {
	var body store.ModelContextLimits
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateModelContextLimits(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "model_context_limits_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateSearchProvider(w http.ResponseWriter, r *http.Request) {
	var body store.SearchProviderConfig
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateSearchProviderConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "search_provider_update_failed", "联网检索配置保存失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateInfiniteCodeQuotaConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]store.InfiniteCodeQuotaPlan
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateInfiniteCodeQuotaConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "infinite_code_quota_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateInfiniteCodeModelLimits(w http.ResponseWriter, r *http.Request) {
	var body map[string]map[string]int
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateInfiniteCodeModelLimits(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "infinite_code_model_limits_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateShareCollaborationConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]store.ShareCollaborationPlan
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if err := s.Store.UpdateShareCollaborationConfig(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "share_collaboration_config_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminUpdateOAuth(w http.ResponseWriter, r *http.Request) {
	var body store.OAuthProvider
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	body.Slug = chi.URLParam(r, "slug")
	if body.ProviderKind == "" {
		body.ProviderKind = "oauth2"
	}
	if err := s.Store.UpsertOAuthProvider(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "oauth_update_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminCreateOAuth(w http.ResponseWriter, r *http.Request) {
	var body store.OAuthProvider
	if err := httpx.Decode(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	body.Slug = strings.TrimSpace(body.Slug)
	if body.Slug == "" {
		httpx.Error(w, http.StatusBadRequest, "oauth_slug_required", "请填写 OAuth Provider 标识")
		return
	}
	if body.ProviderKind == "" {
		body.ProviderKind = "oauth2"
	}
	if err := s.Store.UpsertOAuthProvider(r.Context(), body); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "oauth_create_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	code := "admin_root_" + uuid.NewString()[:8]
	if err := s.Store.CreateAffiliateInvite(r.Context(), currentAdminID(r), "", code, map[string]any{"kind": "admin_invite"}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "invite_create_failed", err.Error())
		return
	}
	link := fmt.Sprintf("%s/register?aff=%s", requestBaseURLForPort(r, "1001", s.Config.UserBaseURL), code)
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"code": code,
		"link": link,
	})
}
