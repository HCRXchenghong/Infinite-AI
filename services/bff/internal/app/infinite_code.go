package app

import (
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

const (
	infiniteCodeDeviceTTL   = 10 * time.Minute
	infiniteCodeAccessTTL   = 12 * time.Hour
	infiniteCodeRefreshTTL  = 30 * 24 * time.Hour
	infiniteCodePollSeconds = 5
	infiniteCodeClientID    = "infinite-code-cli"
)

type infiniteCodeDeviceCodeRequest struct {
	ClientID string `json:"client_id"`
}

type infiniteCodeDeviceTokenRequest struct {
	GrantType    string `json:"grant_type"`
	DeviceCode   string `json:"device_code"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

type infiniteCodeDeviceState struct {
	ClientID  string    `json:"clientId"`
	UserCode  string    `json:"userCode"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	UserID    string    `json:"userId,omitempty"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	Denied    bool      `json:"denied,omitempty"`
}

type infiniteCodeQuotaCheck struct {
	PlanCode   string
	PlanName   string
	Credits    int
	Used       int
	Remaining  int
	ResetHours int
	WindowFrom time.Time
	NextReset  time.Time
}

func (s *Server) handleInfiniteCodeDeviceCode(w http.ResponseWriter, r *http.Request) {
	var body infiniteCodeDeviceCodeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	clientID := strings.TrimSpace(body.ClientID)
	if clientID != infiniteCodeClientID {
		infiniteCodeDeviceError(w, "invalid_client", "客户端不匹配")
		return
	}
	deviceCode, err := randomURLToken(32)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "device_code_failed", "授权码生成失败，请稍后重试")
		return
	}
	userCode, err := randomInfiniteCodeUserCode()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "user_code_failed", "授权码生成失败，请稍后重试")
		return
	}
	now := time.Now().UTC()
	state := infiniteCodeDeviceState{
		ClientID:  clientID,
		UserCode:  userCode,
		CreatedAt: now,
		ExpiresAt: now.Add(infiniteCodeDeviceTTL),
	}
	raw, _ := json.Marshal(state)
	if err := s.Redis.Set(r.Context(), infiniteCodeDeviceKey(deviceCode), raw, infiniteCodeDeviceTTL).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "device_code_store_failed", "授权状态保存失败，请稍后重试")
		return
	}
	if err := s.Redis.Set(r.Context(), infiniteCodeUserCodeKey(userCode), deviceCode, infiniteCodeDeviceTTL).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "user_code_store_failed", "授权状态保存失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          "/auth/device",
		"verification_uri_complete": "/auth/device?code=" + userCode,
		"expires_in":                int(infiniteCodeDeviceTTL.Seconds()),
		"interval":                  infiniteCodePollSeconds,
	})
}

func (s *Server) handleInfiniteCodeDeviceToken(w http.ResponseWriter, r *http.Request) {
	var body infiniteCodeDeviceTokenRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	switch strings.TrimSpace(body.GrantType) {
	case "urn:ietf:params:oauth:grant-type:device_code":
		s.handleInfiniteCodeDeviceTokenPoll(w, r, body)
	case "refresh_token":
		s.handleInfiniteCodeRefreshToken(w, r, body)
	default:
		infiniteCodeDeviceError(w, "unsupported_grant_type", "不支持的授权类型")
	}
}

func (s *Server) handleInfiniteCodeDeviceTokenPoll(w http.ResponseWriter, r *http.Request, body infiniteCodeDeviceTokenRequest) {
	deviceCode := strings.TrimSpace(body.DeviceCode)
	if deviceCode == "" {
		infiniteCodeDeviceError(w, "invalid_request", "缺少 device_code")
		return
	}
	state, ok := s.loadInfiniteCodeDeviceState(w, r, deviceCode)
	if !ok {
		return
	}
	if strings.TrimSpace(body.ClientID) != state.ClientID || body.ClientID != infiniteCodeClientID {
		infiniteCodeDeviceError(w, "invalid_client", "客户端不匹配")
		return
	}
	if state.Denied {
		infiniteCodeDeviceError(w, "access_denied", "用户拒绝授权")
		return
	}
	if strings.TrimSpace(state.UserID) == "" {
		infiniteCodeDeviceError(w, "authorization_pending", "等待用户确认授权")
		return
	}
	accessToken, refreshToken, ok := s.issueInfiniteCodeTokens(w, r, auth.InfiniteCodeTokenPrincipal{
		UserID:   state.UserID,
		Email:    state.Email,
		Name:     state.Name,
		ClientID: state.ClientID,
	})
	if !ok {
		return
	}
	_, _ = s.Redis.Del(r.Context(), infiniteCodeDeviceKey(deviceCode), infiniteCodeUserCodeKey(state.UserCode)).Result()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(infiniteCodeAccessTTL.Seconds()),
	})
}

func (s *Server) handleInfiniteCodeRefreshToken(w http.ResponseWriter, r *http.Request, body infiniteCodeDeviceTokenRequest) {
	refreshToken := strings.TrimSpace(body.RefreshToken)
	if refreshToken == "" {
		infiniteCodeDeviceError(w, "invalid_request", "缺少 refresh_token")
		return
	}
	raw, err := s.Redis.Get(r.Context(), auth.InfiniteCodeTokenRedisKey("refresh", refreshToken)).Result()
	if err != nil {
		infiniteCodeDeviceError(w, "expired_token", "登录已过期，请重新登录")
		return
	}
	var principal auth.InfiniteCodeTokenPrincipal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil || strings.TrimSpace(principal.UserID) == "" {
		infiniteCodeDeviceError(w, "invalid_grant", "登录状态无效，请重新登录")
		return
	}
	if strings.TrimSpace(body.ClientID) != principal.ClientID || body.ClientID != infiniteCodeClientID {
		infiniteCodeDeviceError(w, "invalid_client", "客户端不匹配")
		return
	}
	accessToken, nextRefreshToken, ok := s.issueInfiniteCodeTokens(w, r, principal)
	if !ok {
		return
	}
	_, _ = s.Redis.Del(r.Context(), auth.InfiniteCodeTokenRedisKey("refresh", refreshToken)).Result()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": nextRefreshToken,
		"expires_in":    int(infiniteCodeAccessTTL.Seconds()),
	})
}

func (s *Server) handleInfiniteCodeAuthorize(w http.ResponseWriter, r *http.Request) {
	userCode := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if userCode == "" {
		userCode = strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	}
	currentSession, currentUser, _ := s.currentUserFromCookie(r)
	if r.Method == http.MethodGet {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, sessionCSRFToken(currentSession), "", false)
		return
	}
	if userCode == "" {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, sessionCSRFToken(currentSession), "授权链接无效或已过期，请回到 Infinite Code 重新登录", false)
		return
	}
	deviceCode, err := s.Redis.Get(r.Context(), infiniteCodeUserCodeKey(userCode)).Result()
	if err != nil {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, sessionCSRFToken(currentSession), "授权链接无效或已过期，请回到 Infinite Code 重新登录", false)
		return
	}
	state, ok := s.loadInfiniteCodeDeviceState(w, r, deviceCode)
	if !ok {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, sessionCSRFToken(currentSession), "授权链接无效或已过期，请回到 Infinite Code 重新登录", false)
		return
	}
	if currentSession != nil && r.FormValue("csrf_token") != currentSession.CSRFToken {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, sessionCSRFToken(currentSession), "授权请求已失效，请刷新页面后重试", false)
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	if action == "deny" {
		state.Denied = true
		s.storeInfiniteCodeDeviceState(r, deviceCode, state)
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, "", "已拒绝授权，可以关闭此页面", true)
		return
	}
	if currentUser == nil {
		s.renderInfiniteCodeDevicePage(w, userCode, currentUser, "", "请先在这个浏览器登录 Infinite-AI，再确认授权", false)
		return
	}
	state.UserID = currentUser.ID
	state.Email = currentUser.Email
	state.Name = currentUser.DisplayName
	state.Denied = false
	if !s.storeInfiniteCodeDeviceState(r, deviceCode, state) {
		httpx.Error(w, http.StatusInternalServerError, "device_authorize_failed", "授权状态保存失败，请稍后重试")
		return
	}
	s.renderInfiniteCodeDevicePage(w, userCode, currentUser, "", "授权成功，可以回到 Infinite Code 继续使用", true)
}

func (s *Server) handleInfiniteCodeAPIUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.infiniteCodeTokenPrincipal(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":    principal.UserID,
		"email": principal.Email,
	})
}

func (s *Server) handleInfiniteCodeAPIOrgs(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.infiniteCodeTokenPrincipal(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(principal.Name)
	if name == "" {
		name = "Infinite-AI"
	}
	httpx.JSON(w, http.StatusOK, []map[string]any{
		{
			"id":   "default",
			"name": name + " 的 Infinite-AI",
		},
	})
}

func (s *Server) handleInfiniteCodeAPIConfig(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.infiniteCodeTokenPrincipal(w, r)
	if !ok {
		return
	}
	quota, err := s.resolveInfiniteCodeQuota(r, principal.UserID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "quota_failed", "额度信息获取失败，请稍后重试")
		return
	}
	models, defaultModel := s.infiniteCodeProviderModels(r, principal.UserID, quota)
	if defaultModel == "" {
		defaultModel = s.Config.DefaultChatRoute
	}
	origin := strings.TrimRight(requestBaseURL(r, s.Config.UserBaseURL), "/")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"config": map[string]any{
			"$schema":           origin + "/config.json",
			"enabled_providers": []string{"infinite-code"},
			"model":             "infinite-code/" + defaultModel,
			"small_model":       "infinite-code/" + defaultModel,
			"provider": map[string]any{
				"infinite-code": map[string]any{
					"name": "Infinite-AI",
					"env":  []string{"INFINITE_CODE_CONSOLE_TOKEN"},
					"options": map[string]any{
						"baseURL": origin + "/v1",
						"apiKey":  "{env:INFINITE_CODE_CONSOLE_TOKEN}",
					},
					"models": models,
				},
			},
		},
	})
}

func (s *Server) handleInfiniteCodeDesktopBilling(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.infiniteCodeTokenPrincipal(w, r)
	if !ok {
		return
	}
	quota, err := s.resolveInfiniteCodeQuota(r, principal.UserID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "quota_failed", "额度信息获取失败，请稍后重试")
		return
	}
	plans, err := s.Store.ListPlans(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "plans_failed", "套餐信息获取失败，请稍后重试")
		return
	}
	quotaConfig, err := s.Store.GetInfiniteCodeQuotaConfig(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "quota_config_failed", "套餐额度信息获取失败，请稍后重试")
		return
	}
	currentPlan := quota.PlanCode
	subscription := quota.PlanName
	if subscription == "" {
		subscription = currentPlan
	}
	origin := requestBaseURL(r, s.Config.UserBaseURL)
	if origin == "" {
		origin = strings.TrimRight(s.Config.UserBaseURL, "/")
	}
	recentUsage, err := s.infiniteCodeRecentUsage(r, principal.UserID, 5)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "usage_failed", "用量信息获取失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"balance":          quota.Remaining,
		"monthlyLimit":     quota.Credits,
		"monthlyUsage":     quota.Used,
		"usagePercent":     infiniteCodeUsagePercent(quota),
		"subscription":     subscription,
		"subscriptionPlan": currentPlan,
		"plans":            infiniteCodeDesktopPlans(origin, plans, currentPlan, quotaConfig),
		"recentUsage":      recentUsage,
		"modelAccess":      s.infiniteCodeDesktopModelAccess(r, principal.UserID, quota),
		"nextResetAt":      quota.NextReset,
		"windowStartAt":    quota.WindowFrom,
	})
}

func (s *Server) infiniteCodeProviderModels(r *http.Request, userID string, quota *infiniteCodeQuotaCheck) (map[string]any, string) {
	routes, err := s.Store.ListActiveModelRoutes(r.Context(), false)
	if err != nil {
		return fallbackInfiniteCodeModels(s.Config.DefaultChatRoute), s.Config.DefaultChatRoute
	}
	contextLimits, _ := s.Store.GetModelContextLimits(r.Context())
	modelLimits, _ := s.Store.GetInfiniteCodeModelLimits(r.Context())
	autoSlug := infiniteCodeAutoModelSlug(routes, s.Config.DefaultChatRoute)
	preferredDefault := s.Config.DefaultChatRoute
	if quota != nil && quota.PlanCode == "free" && autoSlug != "" {
		preferredDefault = autoSlug
	}
	defaultModel := ""
	firstEnabled := ""
	models := map[string]any{}
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active {
			continue
		}
		slug := strings.TrimSpace(route.Slug)
		if slug == "" {
			continue
		}
		access := s.infiniteCodeModelAccess(r, userID, slug, autoSlug, quota, modelLimits)
		if access["enabled"] == true && firstEnabled == "" {
			firstEnabled = slug
		}
		if access["enabled"] == true && (defaultModel == "" || slug == preferredDefault) {
			defaultModel = slug
		}
		contextLimit := resolveInfiniteCodeContextLimit(contextLimits, userID, slug)
		if contextLimit <= 0 {
			contextLimit = 128000
		}
		outputLimit := 8192
		if contextLimit >= 200000 {
			outputLimit = 16384
		}
		reasoning := route.ModelType == "reasoning" || strings.Contains(strings.ToLower(route.Slug+" "+route.Name+" "+route.Description), "pro")
		model := map[string]any{
			"name":         firstNonEmpty(route.Name, slug),
			"id":           slug,
			"family":       firstNonEmpty(route.UpstreamModel, route.Protocol, "infinite-ai"),
			"release_date": time.Now().UTC().Format("2006-01-02"),
			"attachment":   true,
			"reasoning":    reasoning,
			"temperature":  true,
			"tool_call":    true,
			"sort_order":   route.SortOrder,
			"access":       access,
			"cost": map[string]any{
				"input":  0,
				"output": 0,
			},
			"limit": map[string]any{
				"context": contextLimit,
				"output":  outputLimit,
			},
			"modalities": map[string]any{
				"input":  []string{"text", "image"},
				"output": []string{"text"},
			},
			"provider": map[string]any{
				"npm": "@ai-sdk/openai-compatible",
			},
		}
		if reasoning {
			model["variants"] = infiniteCodeReasoningVariants(quota)
		}
		models[slug] = model
	}
	if len(models) == 0 {
		return fallbackInfiniteCodeModels(s.Config.DefaultChatRoute), s.Config.DefaultChatRoute
	}
	if defaultModel == "" {
		defaultModel = firstEnabled
	}
	return models, defaultModel
}

func (s *Server) infiniteCodeModelAccess(r *http.Request, userID string, slug string, autoSlug string, quota *infiniteCodeQuotaCheck, limits map[string]map[string]int) map[string]any {
	planCode := "free"
	planName := "免费版"
	windowHours := 24
	windowFrom := time.Now().Add(-24 * time.Hour)
	if quota != nil {
		planCode = quota.PlanCode
		planName = quota.PlanName
		windowHours = quota.ResetHours
		windowFrom = quota.WindowFrom
	}
	out := map[string]any{
		"enabled":     true,
		"reason":      "",
		"planCode":    planCode,
		"planName":    planName,
		"windowHours": windowHours,
	}
	if quota != nil {
		out["quotaLimit"] = quota.Credits
		out["quotaUsed"] = quota.Used
		out["quotaRemaining"] = quota.Remaining
		out["nextResetAt"] = quota.NextReset
		if quota.Remaining <= 0 {
			out["enabled"] = false
			out["reason"] = "quota_exhausted"
			return out
		}
	}
	planLimits := limits[planCode]
	if planCode == "free" && autoSlug != "" && slug != autoSlug {
		out["enabled"] = false
		out["reason"] = "free_auto_only"
		return out
	}
	limit, ok := planLimits[slug]
	if !ok && quota != nil {
		limit = quota.Credits
		ok = true
	}
	if !ok {
		return out
	}
	out["limit"] = limit
	if limit <= 0 {
		out["enabled"] = false
		out["reason"] = "plan_unavailable"
		return out
	}
	used, err := s.Store.CountInfiniteCodeModelUsageSince(r.Context(), userID, slug, windowFrom)
	if err != nil {
		out["enabled"] = false
		out["reason"] = "usage_unavailable"
		return out
	}
	out["used"] = used
	out["remaining"] = limit - used
	if limit-used <= 0 {
		out["enabled"] = false
		out["reason"] = "daily_limit_exhausted"
		out["remaining"] = 0
	}
	return out
}

func infiniteCodeReasoningVariants(quota *infiniteCodeQuotaCheck) map[string]any {
	planCode := "free"
	if quota != nil {
		planCode = quota.PlanCode
	}
	levels := []string{}
	switch planCode {
	case "pro_basic", "pro_max":
		levels = []string{"low", "medium", "high"}
	case "plus":
		levels = []string{"low", "medium"}
	case "go":
		levels = []string{"low"}
	}
	out := map[string]any{}
	for _, level := range levels {
		out[level] = map[string]any{"reasoningEffort": level}
	}
	return out
}

func infiniteCodeAutoModelSlug(routes []store.ModelRoute, defaultRoute string) string {
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active {
			continue
		}
		value := strings.ToLower(strings.Join([]string{route.Slug, route.Name, route.Description}, " "))
		if strings.Contains(value, "auto") || strings.Contains(value, "自动") || strings.Contains(value, "智能") {
			return strings.TrimSpace(route.Slug)
		}
	}
	if strings.TrimSpace(defaultRoute) != "" {
		return strings.TrimSpace(defaultRoute)
	}
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active {
			continue
		}
		if strings.TrimSpace(route.Slug) != "" {
			return strings.TrimSpace(route.Slug)
		}
	}
	return ""
}

func (s *Server) infiniteCodeRecentUsage(r *http.Request, userID string, limit int) ([]map[string]any, error) {
	rows, err := s.Store.DB.Query(r.Context(), `
		SELECT COALESCE(metadata->>'model', ''), ABS(amount), created_at
		FROM quota_ledgers
		WHERE user_id = $1
		  AND event_type = 'infinite_code_usage'
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var model string
		var cost float64
		var createdAt time.Time
		if err := rows.Scan(&model, &cost, &createdAt); err != nil {
			return nil, err
		}
		if strings.TrimSpace(model) == "" {
			model = "Infinite Code"
		}
		out = append(out, map[string]any{
			"model":       model,
			"cost":        cost,
			"timeCreated": createdAt,
		})
	}
	return out, rows.Err()
}

func (s *Server) infiniteCodeDesktopModelAccess(r *http.Request, userID string, quota *infiniteCodeQuotaCheck) []map[string]any {
	routes, err := s.Store.ListActiveModelRoutes(r.Context(), false)
	if err != nil {
		return nil
	}
	limits, _ := s.Store.GetInfiniteCodeModelLimits(r.Context())
	autoSlug := infiniteCodeAutoModelSlug(routes, s.Config.DefaultChatRoute)
	out := []map[string]any{}
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active || strings.TrimSpace(route.Slug) == "" {
			continue
		}
		access := s.infiniteCodeModelAccess(r, userID, route.Slug, autoSlug, quota, limits)
		out = append(out, map[string]any{
			"id":          route.Slug,
			"name":        firstNonEmpty(route.Name, route.Slug),
			"description": route.Description,
			"sortOrder":   route.SortOrder,
			"enabled":     access["enabled"] == true,
			"reason":      access["reason"],
			"limit":       access["limit"],
			"used":        access["used"],
			"remaining":   access["remaining"],
			"windowHours": access["windowHours"],
		})
	}
	return out
}

func infiniteCodeUsagePercent(quota *infiniteCodeQuotaCheck) float64 {
	if quota == nil || quota.Credits <= 0 {
		return 0
	}
	return float64(quota.Used) / float64(quota.Credits) * 100
}

func infiniteCodeDesktopPlans(origin string, plans []store.Plan, currentPlan string, quotaConfig map[string]store.InfiniteCodeQuotaPlan) []map[string]any {
	base := strings.TrimRight(origin, "/")
	return append([]map[string]any{
		{
			"id":          "recharge",
			"name":        "额度充值",
			"description": "为当前账号补充 Infinite Code 可用额度。",
			"price":       "按需充值",
			"interval":    "",
			"current":     false,
			"checkoutUrl": base + "/plans",
		},
	}, func() []map[string]any {
		out := make([]map[string]any, 0, len(plans))
		for _, plan := range plans {
			quota := quotaConfig[plan.Code]
			out = append(out, map[string]any{
				"id":            plan.Code,
				"name":          firstNonEmpty(plan.Name, plan.Code),
				"description":   plan.Description,
				"price":         infiniteCodePlanPrice(plan),
				"interval":      plan.Interval,
				"includedUsage": quota.Credits,
				"current":       plan.Code == currentPlan,
				"checkoutUrl":   base + "/plans?plan=" + plan.Code,
			})
		}
		return out
	}()...)
}

func infiniteCodePlanPrice(plan store.Plan) string {
	if plan.PriceCents <= 0 {
		return "免费"
	}
	value := float64(plan.PriceCents) / 100
	if value == float64(int(value)) {
		return "¥" + strconv.Itoa(int(value))
	}
	return "¥" + strconv.FormatFloat(value, 'f', 2, 64)
}

func (s *Server) currentUserFromCookie(r *http.Request) (*store.SessionView, *store.User, error) {
	sessionCookie, err := r.Cookie(s.Config.SessionCookieName)
	if err != nil {
		return nil, nil, err
	}
	session, err := s.Store.GetUserSession(r.Context(), sessionCookie.Value)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.Store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		return session, nil, err
	}
	return session, user, nil
}

func sessionCSRFToken(session *store.SessionView) string {
	if session == nil {
		return ""
	}
	return session.CSRFToken
}

func (s *Server) infiniteCodeTokenPrincipal(w http.ResponseWriter, r *http.Request) (auth.InfiniteCodeTokenPrincipal, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "token_missing", "请先登录 Infinite Code")
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	raw, err := s.Redis.Get(r.Context(), auth.InfiniteCodeTokenRedisKey("access", token)).Result()
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "token_invalid", "登录状态已过期，请重新登录")
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	var principal auth.InfiniteCodeTokenPrincipal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil || strings.TrimSpace(principal.UserID) == "" {
		httpx.Error(w, http.StatusUnauthorized, "token_invalid", "登录状态无效，请重新登录")
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	if user, err := s.Store.GetUserByID(r.Context(), principal.UserID); err == nil && user.Status != "active" {
		httpx.Error(w, http.StatusForbidden, "user_inactive", "账号当前不可用，请联系管理员")
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	return principal, true
}

func (s *Server) issueInfiniteCodeTokens(w http.ResponseWriter, r *http.Request, principal auth.InfiniteCodeTokenPrincipal) (string, string, bool) {
	accessToken, err := auth.GenerateInfiniteCodeAccessToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "access_token_failed", "访问令牌生成失败，请稍后重试")
		return "", "", false
	}
	refreshToken, err := auth.GenerateInfiniteCodeRefreshToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "refresh_token_failed", "刷新令牌生成失败，请稍后重试")
		return "", "", false
	}
	raw, _ := json.Marshal(principal)
	if err := s.Redis.Set(r.Context(), auth.InfiniteCodeTokenRedisKey("access", accessToken), raw, infiniteCodeAccessTTL).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "access_token_store_failed", "访问令牌保存失败，请稍后重试")
		return "", "", false
	}
	if err := s.Redis.Set(r.Context(), auth.InfiniteCodeTokenRedisKey("refresh", refreshToken), raw, infiniteCodeRefreshTTL).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "refresh_token_store_failed", "刷新令牌保存失败，请稍后重试")
		return "", "", false
	}
	return accessToken, refreshToken, true
}

func (s *Server) loadInfiniteCodeDeviceState(w http.ResponseWriter, r *http.Request, deviceCode string) (infiniteCodeDeviceState, bool) {
	raw, err := s.Redis.Get(r.Context(), infiniteCodeDeviceKey(deviceCode)).Result()
	if err != nil {
		infiniteCodeDeviceError(w, "expired_token", "授权码已过期，请重新登录")
		return infiniteCodeDeviceState{}, false
	}
	var state infiniteCodeDeviceState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		infiniteCodeDeviceError(w, "invalid_grant", "授权状态无效，请重新登录")
		return infiniteCodeDeviceState{}, false
	}
	if time.Now().UTC().After(state.ExpiresAt) {
		infiniteCodeDeviceError(w, "expired_token", "授权码已过期，请重新登录")
		return infiniteCodeDeviceState{}, false
	}
	return state, true
}

func (s *Server) storeInfiniteCodeDeviceState(r *http.Request, deviceCode string, state infiniteCodeDeviceState) bool {
	ttl := time.Until(state.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Second
	}
	raw, _ := json.Marshal(state)
	return s.Redis.Set(r.Context(), infiniteCodeDeviceKey(deviceCode), raw, ttl).Err() == nil
}

func randomInfiniteCodeUserCode() (string, error) {
	token, err := randomURLToken(5)
	if err != nil {
		return "", err
	}
	token = strings.ToUpper(strings.ReplaceAll(token, "_", ""))
	if len(token) > 8 {
		token = token[:8]
	}
	if len(token) < 6 {
		return randomInfiniteCodeUserCode()
	}
	return token[:4] + "-" + token[4:], nil
}

func infiniteCodeDeviceKey(deviceCode string) string {
	return "infinite-code:device:" + deviceCode
}

func infiniteCodeUserCodeKey(userCode string) string {
	return "infinite-code:user-code:" + strings.ToUpper(strings.TrimSpace(userCode))
}

func infiniteCodeDeviceError(w http.ResponseWriter, code string, message string) {
	httpx.JSON(w, http.StatusBadRequest, map[string]any{
		"error":             code,
		"error_description": message,
	})
}

func fallbackInfiniteCodeModels(defaultRoute string) map[string]any {
	if strings.TrimSpace(defaultRoute) == "" {
		defaultRoute = "infinite-ai-standard"
	}
	return map[string]any{
		defaultRoute: map[string]any{
			"name":         "Infinite-AI Standard",
			"id":           defaultRoute,
			"family":       "infinite-ai",
			"release_date": time.Now().UTC().Format("2006-01-02"),
			"attachment":   true,
			"reasoning":    false,
			"temperature":  true,
			"tool_call":    true,
			"cost": map[string]any{
				"input":  0,
				"output": 0,
			},
			"limit": map[string]any{
				"context": 128000,
				"output":  8192,
			},
			"modalities": map[string]any{
				"input":  []string{"text", "image"},
				"output": []string{"text"},
			},
			"provider": map[string]any{
				"npm": "@ai-sdk/openai-compatible",
			},
		},
	}
}

func resolveInfiniteCodeContextLimit(limits *store.ModelContextLimits, userID string, modelSlug string) int {
	if limits == nil {
		return 0
	}
	if userLimits, ok := limits.Users[userID]; ok {
		if limit := userLimits[modelSlug]; limit > 0 {
			return limit
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

func (s *Server) resolveInfiniteCodeQuota(r *http.Request, userID string) (*infiniteCodeQuotaCheck, error) {
	planCode, err := s.Store.ResolveUserPlanCode(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	config, err := s.Store.GetInfiniteCodeQuotaConfig(r.Context())
	if err != nil {
		return nil, err
	}
	planQuota, ok := config[planCode]
	if !ok {
		planQuota = store.InfiniteCodeQuotaPlan{Credits: 0, ResetHours: 24}
	}
	if planQuota.ResetHours <= 0 {
		planQuota.ResetHours = 24
	}
	anchor := time.Now().UTC()
	if sub, err := s.Store.GetSubscriptionByUser(r.Context(), userID); err == nil && !sub.StartedAt.IsZero() {
		anchor = sub.StartedAt.UTC()
	}
	windowStart, nextResetAt := infiniteCodeWindowBounds(anchor, planQuota.ResetHours, time.Now().UTC())
	used, err := s.Store.CountInfiniteCodeUsageSince(r.Context(), userID, windowStart)
	if err != nil {
		return nil, err
	}
	remaining := planQuota.Credits - used
	if remaining < 0 {
		remaining = 0
	}
	planName := planCode
	if plan, err := s.Store.GetPlan(r.Context(), planCode); err == nil && strings.TrimSpace(plan.Name) != "" {
		planName = plan.Name
	}
	return &infiniteCodeQuotaCheck{
		PlanCode:   planCode,
		PlanName:   planName,
		Credits:    planQuota.Credits,
		Used:       used,
		Remaining:  remaining,
		ResetHours: planQuota.ResetHours,
		WindowFrom: windowStart,
		NextReset:  nextResetAt,
	}, nil
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
	return windowStart, windowStart.Add(duration)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) renderInfiniteCodeDevicePage(w http.ResponseWriter, userCode string, currentUser *store.User, csrfToken string, message string, completed bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	code := html.EscapeString(strings.ToUpper(strings.TrimSpace(userCode)))
	messageHTML := ""
	if strings.TrimSpace(message) != "" && !completed {
		messageHTML = `<div class="notice">` + html.EscapeString(message) + `</div>`
	}
	loginLink := strings.TrimRight(s.Config.UserBaseURL, "/") + "/login"
	body := ""
	title := "连接 Infinite Code"
	script := ""
	if code != "" {
		script = `<script>try{history.replaceState(null,document.title,"/auth/device")}catch(_){}</script>`
	}
	if completed {
		title = "授权完成"
		if strings.HasPrefix(strings.TrimSpace(message), "授权成功") {
			title = "授权成功"
		}
		if strings.HasPrefix(strings.TrimSpace(message), "已拒绝") {
			title = "已拒绝授权"
		}
		body = `
      <div class="notice success">` + html.EscapeString(message) + `</div>
    `
	} else if currentUser == nil {
		body = `
      <p class="copy">先登录 Infinite-AI，再把这个终端会话连接到你的账号。</p>
      <a class="button" href="` + html.EscapeString(loginLink) + `">登录 Infinite-AI</a>
    `
	} else if code == "" {
		body = `
      <p class="copy">请回到 Infinite Code 重新开始登录。</p>
    `
	} else {
		body = `
      <div class="account">
        <div>
          <span>当前账号</span>
          <strong>` + html.EscapeString(currentUser.Email) + `</strong>
        </div>
      </div>
      <form method="post" action="/auth/device">
        <input type="hidden" name="code" value="` + code + `" />
        <input type="hidden" name="csrf_token" value="` + html.EscapeString(csrfToken) + `" />
        <button class="button" type="submit" name="action" value="allow">授权并继续</button>
        <button class="link" type="submit" name="action" value="deny">拒绝本次连接</button>
      </form>
    `
	}
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>Infinite Code 授权</title>
  <style>
    :root{color-scheme:dark;--bg:#111111;--panel:#171717;--panel-soft:#111111;--line:#333333;--text:#ececec;--muted:#888888;--button:#ececec;--button-hover:#ffffff}
    *{box-sizing:border-box}
    body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg);color:var(--text);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;padding:24px}
    main{width:min(448px,100%);border:1px solid var(--line);border-radius:16px;background:var(--panel);padding:32px;box-shadow:0 24px 70px rgba(0,0,0,.42)}
    h1{margin:0 0 8px;text-align:center;font-size:24px;font-weight:600;letter-spacing:-.02em;line-height:1.25}
    .sub{margin:0 0 28px;text-align:center;color:var(--muted);font-size:14px;line-height:1.7}
    .notice{margin:0 0 16px;padding:12px 14px;border-radius:12px;background:var(--panel-soft);border:1px solid var(--line);color:var(--text);font-size:14px;line-height:1.55}
    .notice.success{text-align:center}
    .copy{margin:0 0 16px;text-align:center;color:var(--muted);font-size:14px;line-height:1.7}
    .account{margin:0 0 16px;padding:14px 16px;border-radius:12px;background:var(--panel-soft);border:1px solid var(--line)}
    .account span{display:block;margin-bottom:6px;color:var(--muted);font-size:12px}
    .account strong{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:14px;font-weight:500;color:var(--text)}
    form{display:grid;gap:12px}
    .button{width:100%;display:inline-flex;align-items:center;justify-content:center;border:0;border-radius:12px;background:var(--button);color:#000000;padding:13px 16px;font-size:14px;font-weight:600;text-decoration:none;cursor:pointer;transition:background .15s ease}
    .button:hover{background:var(--button-hover)}
    .link{width:100%;border:1px solid var(--line);border-radius:12px;background:transparent;color:var(--muted);font-size:14px;cursor:pointer;padding:12px 16px}
    .link:hover{background:#212121;color:var(--text)}
    .foot{margin-top:22px;padding-top:18px;border-top:1px solid var(--line);color:var(--muted);font-size:12px;line-height:1.7;text-align:center}
    @media (max-width:520px){body{padding:16px}main{padding:24px;border-radius:16px}}
  </style>
</head>
<body>
  <main>
    <h1>` + title + `</h1>
    <p class="sub">授权后，Infinite Code 将使用你后台配置的模型、账号和套餐额度。</p>
    ` + messageHTML + body + `
    <div class="foot">这次连接只用于 Infinite Code 调用你的 Infinite-AI 后台模型；每次模型请求都会按后台套餐额度扣减。</div>
  </main>
  ` + script + `
</body>
</html>`))
}
