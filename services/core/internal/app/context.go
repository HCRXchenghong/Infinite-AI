package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
)

type contextKey string

const principalContextKey contextKey = "principal"

type principal struct {
	subjectID string
	kind      string
	role      string
	email     string
}

func (s *Server) internalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenValue := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if tokenValue == "" {
			httpx.Error(w, http.StatusUnauthorized, "internal_token_missing", "内部访问令牌缺失")
			return
		}
		claims, err := auth.ParseInternalJWT(s.Config.InternalJWTSecret, tokenValue)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "internal_token_invalid", "内部访问令牌无效")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey, principal{
			subjectID: claims.Subject,
			kind:      claims.Kind,
			role:      claims.Role,
			email:     claims.Email,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireKinds(kinds ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := principalFromContext(r.Context())
			for _, kind := range kinds {
				if current.kind == kind {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.Error(w, http.StatusForbidden, "principal_forbidden", "当前账号无权访问该接口")
		})
	}
}

func requireAdminRoles(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := principalFromContext(r.Context())
			if current.kind != "admin" {
				httpx.Error(w, http.StatusForbidden, "admin_required", "需要管理员登录后才能访问")
				return
			}
			for _, role := range roles {
				if current.role == role || current.role == "super_admin" {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.Error(w, http.StatusForbidden, "role_forbidden", "当前管理员角色无权访问该接口")
		})
	}
}

func principalFromContext(ctx context.Context) principal {
	value := ctx.Value(principalContextKey)
	current, _ := value.(principal)
	return current
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(s.internalOnly)
	r.Use(s.systemLogMiddleware("core"))

	r.Group(func(r chi.Router) {
		r.Use(requireKinds("public", "user", "admin"))
		r.Get("/billing/plans", s.handleListPlans)
		r.Get("/downloads/releases", s.handleListDownloads)
		r.Get("/chat/models", s.handleListPublicModels)
		r.Get("/chat/shares/{id}", s.handleGetPublicConversationShare)
		r.Get("/chat/shares/{id}/assets/{assetId}", s.handleGetPublicConversationShareAsset)
		r.Post("/chat/shares/{id}/collaboration", s.handleJoinSharedConversationCollaboration)
		r.Post("/chat/shares/{id}/messages", s.handleCreateSharedConversationMessage)
		r.Get("/redeem/codes/{code}", s.handleRedeemPreview)
		r.Post("/redeem/claim", s.handleRedeemClaim)
		r.Post("/webhooks/ifpay", s.handleIFPayWebhook)
	})

	r.Group(func(r chi.Router) {
		r.Use(requireKinds("user"))
		r.Get("/chat/conversations", s.handleListConversations)
		r.Post("/chat/conversations", s.handleCreateConversation)
		r.Delete("/chat/conversations/{id}", s.handleDeleteConversation)
		r.Get("/chat/conversations/{id}/messages", s.handleListMessages)
		r.Post("/chat/conversations/{id}/messages", s.handleCreateChatMessage)
		r.Get("/chat/conversations/{id}/share", s.handleGetConversationShare)
		r.Put("/chat/conversations/{id}/share", s.handleUpsertConversationShare)
		r.Get("/chat/conversations/{id}/runs/active", s.handleListActiveChatRuns)
		r.Get("/chat/runs/{id}", s.handleGetChatRun)
		r.Get("/chat/runs/{id}/events", s.handleChatRunEvents)
		r.Post("/chat/runs/{id}/cancel", s.handleCancelChatRun)
		r.Get("/chat/assets/{id}", s.handleChatAssetDownload)
		r.Get("/chat/artifacts/{id}", s.handleGetChatArtifact)
		r.Post("/chat/artifacts/{id}/versions", s.handleCreateChatArtifactVersion)
		r.Get("/chat/artifacts/{id}/download", s.handleDownloadChatArtifact)
		r.Post("/chat/temporary/messages", s.handleCreateTemporaryChatMessage)
		r.Post("/chat/images/generations", s.handleCreateChatImage)
		r.Post("/chat/temporary/images/generations", s.handleCreateTemporaryChatImage)
		r.Post("/chat/attachments/upload-init", s.handleAttachmentUploadInit)
		r.Put("/chat/attachments/{id}/upload", s.handleAttachmentUpload)
		r.Post("/chat/attachments/{id}/complete", s.handleAttachmentComplete)
		r.Get("/developer/api-keys", s.handleListAPIKeys)
		r.Post("/developer/api-keys", s.handleCreateAPIKey)
		r.Delete("/developer/api-keys/{id}", s.handleRevokeAPIKey)
		r.Get("/developer/usage", s.handleDeveloperUsage)
		r.Get("/billing/subscription", s.handleGetSubscription)
		r.Post("/billing/orders", s.handleCreateOrder)
		r.Get("/billing/orders/{id}", s.handleGetOrder)
		r.Get("/user/settings", s.handleGetUserSettings)
		r.Put("/user/settings", s.handleUpdateUserSettings)
		r.Post("/user/export", s.handleExportUserData)
		r.Delete("/user/chat", s.handleClearChats)
		r.Delete("/user/account", s.handleDeleteUserAccount)
	})

	r.Group(func(r chi.Router) {
		r.Use(requireKinds("admin"))
		r.With(requireAdminRoles("ops_admin", "super_admin")).Get("/admin/dashboard", s.handleAdminDashboard)
		r.With(requireAdminRoles("support_admin", "finance_admin", "super_admin")).Get("/admin/users", s.handleAdminUsers)
		r.With(requireAdminRoles("support_admin", "super_admin")).Get("/admin/invite-links", s.handleAdminInviteLinks)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Patch("/admin/users/{id}", s.handleAdminUpdateUser)
		r.With(requireAdminRoles("support_admin", "super_admin")).Delete("/admin/users/{id}", s.handleAdminDeleteUser)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Get("/admin/models", s.handleAdminModels)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Post("/admin/models", s.handleAdminUpsertModel)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/models/{slug}", s.handleAdminUpsertModel)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Delete("/admin/models/{slug}", s.handleAdminDeleteModel)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Post("/admin/models/test", s.handleAdminTestModelRoute)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Get("/admin/api-stats", s.handleAdminAPIStats)
		r.With(requireAdminRoles("ops_admin", "support_admin", "super_admin")).Get("/admin/service-alerts", s.handleAdminServiceAlerts)
		r.With(requireAdminRoles("ops_admin", "support_admin", "super_admin")).Post("/admin/service-alerts/{id}/read", s.handleAdminReadServiceAlert)
		r.With(requireAdminRoles("ops_admin", "support_admin", "super_admin")).Post("/admin/service-alerts/{id}/resolve", s.handleAdminResolveServiceAlert)
		r.With(requireAdminRoles("support_admin", "finance_admin", "super_admin")).Get("/admin/member-stats", s.handleAdminMemberStats)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Get("/admin/system-logs", s.handleAdminSystemLogs)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Get("/admin/membership", s.handleAdminMembership)
		r.With(requireAdminRoles("finance_admin", "super_admin")).Post("/admin/redeem/campaigns", s.handleAdminCreateRedeemCampaign)
		r.With(requireAdminRoles("finance_admin", "super_admin")).Get("/admin/finance", s.handleAdminFinance)
		r.With(requireAdminRoles("finance_admin", "super_admin")).Put("/admin/finance/ifpay", s.handleAdminUpdateIFPay)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Get("/admin/settings", s.handleAdminSettings)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/settings/register", s.handleAdminUpdateRegisterSetting)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/settings/auth-security", s.handleAdminUpdateAuthSecurity)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/settings/email-gateway", s.handleAdminUpdateEmailGateway)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Post("/admin/settings/email-gateway/test", s.handleAdminTestEmailGateway)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/settings/sms-gateway", s.handleAdminUpdateSMSGateway)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Post("/admin/settings/sms-gateway/test", s.handleAdminTestSMSGateway)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Put("/admin/settings/model-membership-limits", s.handleAdminUpdateModelMembershipLimits)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Put("/admin/settings/model-context-limits", s.handleAdminUpdateModelContextLimits)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Put("/admin/settings/search-provider", s.handleAdminUpdateSearchProvider)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Put("/admin/settings/infinite-code-quota", s.handleAdminUpdateInfiniteCodeQuotaConfig)
		r.With(requireAdminRoles("ops_admin", "support_admin", "finance_admin", "super_admin")).Put("/admin/settings/share-collaboration", s.handleAdminUpdateShareCollaborationConfig)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Post("/admin/settings/oauth", s.handleAdminCreateOAuth)
		r.With(requireAdminRoles("ops_admin", "super_admin")).Put("/admin/settings/oauth/{slug}", s.handleAdminUpdateOAuth)
		r.With(requireAdminRoles("support_admin", "super_admin")).Post("/admin/invite-links", s.handleAdminCreateInvite)
		r.With(requireAdminRoles("support_admin", "super_admin")).Delete("/admin/invite-links/{code}", s.handleAdminRevokeInviteLink)
	})

	r.Post("/v1/chat/completions", s.handleDeveloperChatCompletion)
	r.Post("/v1/responses", s.handleDeveloperResponses)
	r.Post("/v1/messages", s.handleDeveloperAnthropicMessage)
	r.Post("/v1/images/generations", s.handleDeveloperImageGeneration)
	r.Post("/v1/images/edits", s.handleDeveloperImageEdit)

	return r
}
