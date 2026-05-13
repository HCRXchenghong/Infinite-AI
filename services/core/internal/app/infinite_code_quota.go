package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

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

type infiniteCodeQuotaReservation struct {
	UserID      string
	ReferenceID string
	Model       string
	Path        string
	Check       *infiniteCodeQuotaCheck
}

type infiniteCodeModelAccessCheck struct {
	Enabled     bool
	Reason      string
	ModelSlug   string
	ModelName   string
	PlanCode    string
	PlanName    string
	Limit       int
	Used        int
	Remaining   int
	WindowHours int
}

func (s *Server) reserveInfiniteCodeQuota(w http.ResponseWriter, r *http.Request, rawToken string, userID string, model string) (*infiniteCodeQuotaReservation, bool) {
	if !strings.HasPrefix(rawToken, auth.InfiniteCodeAccessTokenPrefix) {
		return nil, true
	}
	if strings.TrimSpace(userID) == "" {
		httpx.Error(w, http.StatusUnauthorized, "infinite_code_user_missing", "Infinite Code 登录状态无效，请重新登录")
		return nil, false
	}
	check, err := s.resolveInfiniteCodeQuota(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "infinite_code_quota_failed", "Infinite Code 配额检查失败，请稍后重试")
		return nil, false
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = s.Config.DefaultChatRoute
	}
	if ok := s.enforceInfiniteCodeModelAccess(w, r, userID, model, check); !ok {
		return nil, false
	}
	referenceID := infiniteCodeQuotaReferenceID(r)
	usedAfter, allowed, err := s.Store.ReserveInfiniteCodeUsage(
		r.Context(),
		userID,
		check.WindowFrom,
		check.Credits,
		"infinite_code",
		referenceID,
		infiniteCodeQuotaMetadata(r, model, check, "reserve"),
	)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "infinite_code_quota_failed", "Infinite Code 配额扣减失败，请稍后重试")
		return nil, false
	}
	check.Used = usedAfter
	check.Remaining = check.Credits - usedAfter
	if check.Remaining < 0 {
		check.Remaining = 0
	}
	if !allowed {
		writeInfiniteCodeQuotaExhausted(w, check, model)
		return nil, false
	}
	return &infiniteCodeQuotaReservation{
		UserID:      userID,
		ReferenceID: referenceID,
		Model:       model,
		Path:        r.URL.Path,
		Check:       check,
	}, true
}

func (s *Server) enforceInfiniteCodeModelAccess(w http.ResponseWriter, r *http.Request, userID string, model string, quota *infiniteCodeQuotaCheck) bool {
	access, err := s.resolveInfiniteCodeModelAccess(r.Context(), userID, model, quota)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "infinite_code_model_access_failed", "Infinite Code 模型权限检查失败，请稍后重试")
		return false
	}
	if access.Enabled {
		return true
	}
	status := http.StatusForbidden
	if access.Reason == "daily_limit_exhausted" {
		status = http.StatusTooManyRequests
	}
	writeInfiniteCodeModelUnavailable(w, status, access)
	return false
}

func (s *Server) resolveInfiniteCodeModelAccess(ctx context.Context, userID string, model string, quota *infiniteCodeQuotaCheck) (*infiniteCodeModelAccessCheck, error) {
	route, err := s.Store.FindActiveModelRoute(ctx, model, false)
	if err != nil {
		return nil, err
	}
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
	access := &infiniteCodeModelAccessCheck{
		Enabled:     true,
		ModelSlug:   route.Slug,
		ModelName:   firstNonEmpty(route.Name, route.Slug),
		PlanCode:    planCode,
		PlanName:    planName,
		WindowHours: windowHours,
	}
	limits, err := s.Store.GetInfiniteCodeModelLimits(ctx)
	if err != nil {
		return nil, err
	}
	planLimits := limits[planCode]
	if planCode == "free" {
		autoSlug, err := s.infiniteCodeAutoModelSlug(ctx)
		if err != nil {
			return nil, err
		}
		if autoSlug != "" && route.Slug != autoSlug {
			access.Enabled = false
			access.Reason = "free_auto_only"
			return access, nil
		}
	}
	limit, ok := planLimits[route.Slug]
	if !ok && quota != nil {
		limit = quota.Credits
		ok = true
	}
	if !ok {
		return access, nil
	}
	access.Limit = limit
	if limit <= 0 {
		access.Enabled = false
		access.Reason = "plan_unavailable"
		return access, nil
	}
	used, err := s.Store.CountInfiniteCodeModelUsageSince(ctx, userID, route.Slug, windowFrom)
	if err != nil {
		return nil, err
	}
	access.Used = used
	access.Remaining = limit - used
	if access.Remaining <= 0 {
		access.Enabled = false
		access.Reason = "daily_limit_exhausted"
		access.Remaining = 0
	}
	return access, nil
}

func (s *Server) infiniteCodeAutoModelSlug(ctx context.Context) (string, error) {
	routes, err := s.Store.ListActiveModelRoutes(ctx, false)
	if err != nil {
		return "", err
	}
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active {
			continue
		}
		value := strings.ToLower(strings.Join([]string{route.Slug, route.Name, route.Description}, " "))
		if strings.Contains(value, "auto") || strings.Contains(value, "自动") || strings.Contains(value, "智能") {
			return strings.TrimSpace(route.Slug), nil
		}
	}
	if strings.TrimSpace(s.Config.DefaultChatRoute) != "" {
		return strings.TrimSpace(s.Config.DefaultChatRoute), nil
	}
	for _, route := range routes {
		if route.ModelType == "image" || route.ModelType == "embedding" || !route.Active {
			continue
		}
		if strings.TrimSpace(route.Slug) != "" {
			return strings.TrimSpace(route.Slug), nil
		}
	}
	return "", nil
}

func (s *Server) refundInfiniteCodeQuota(ctx context.Context, reservation *infiniteCodeQuotaReservation, reason string, cause error) {
	if reservation == nil || strings.TrimSpace(reservation.UserID) == "" || strings.TrimSpace(reservation.ReferenceID) == "" {
		return
	}
	metadata := map[string]any{
		"model":      reservation.Model,
		"path":       reservation.Path,
		"reason":     reason,
		"refundedAt": time.Now().UTC(),
	}
	if cause != nil {
		metadata["error"] = summarizeErrorDetail(cause)
	}
	_ = s.Store.RefundInfiniteCodeUsage(context.WithoutCancel(ctx), reservation.UserID, reservation.ReferenceID, metadata)
}

func writeInfiniteCodeQuotaExhausted(w http.ResponseWriter, check *infiniteCodeQuotaCheck, model string) {
	message := fmt.Sprintf("%s 当前周期的 Infinite Code 额度已用完，请升级套餐或等待 %s 后恢复", check.PlanName, check.NextReset.Local().Format("2006-01-02 15:04"))
	quota := map[string]any{
		"planCode":      check.PlanCode,
		"planName":      check.PlanName,
		"credits":       check.Credits,
		"used":          check.Used,
		"remaining":     check.Remaining,
		"resetHours":    check.ResetHours,
		"windowStartAt": check.WindowFrom,
		"nextResetAt":   check.NextReset,
		"model":         model,
	}
	httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "quota_exhausted",
			"code":    "infinite_code_quota_exhausted",
			"quota":   quota,
		},
		"message": message,
		"quota":   quota,
	})
}

func writeInfiniteCodeModelUnavailable(w http.ResponseWriter, status int, access *infiniteCodeModelAccessCheck) {
	message := fmt.Sprintf("%s 当前套餐暂不能使用 %s", access.PlanName, access.ModelName)
	switch access.Reason {
	case "free_auto_only":
		message = "免费版仅可使用 Auto 模型，请升级套餐后使用其他模型"
	case "daily_limit_exhausted":
		message = fmt.Sprintf("%s 今日 %s 可用次数已用完，请升级套餐或明天再试", access.PlanName, access.ModelName)
	case "plan_unavailable":
		message = fmt.Sprintf("%s 当前套餐不支持 %s，请升级套餐后使用", access.PlanName, access.ModelName)
	}
	payload := map[string]any{
		"planCode":    access.PlanCode,
		"planName":    access.PlanName,
		"model":       access.ModelSlug,
		"modelName":   access.ModelName,
		"reason":      access.Reason,
		"limit":       access.Limit,
		"used":        access.Used,
		"remaining":   access.Remaining,
		"windowHours": access.WindowHours,
	}
	httpx.JSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "model_unavailable",
			"code":    "infinite_code_model_unavailable",
			"access":  payload,
		},
		"message": message,
		"access":  payload,
	})
}

func infiniteCodeQuotaReferenceID(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get("x-infinite-code-request"))
	if requestID == "" {
		requestID = strings.TrimSpace(r.Header.Get("x-request-id"))
	}
	if requestID == "" {
		requestID = "request"
	}
	return "infinite-code:" + requestID + ":" + uuid.NewString()
}

func infiniteCodeQuotaMetadata(r *http.Request, model string, check *infiniteCodeQuotaCheck, action string) map[string]any {
	return map[string]any{
		"action":        action,
		"model":         model,
		"path":          r.URL.Path,
		"project":       strings.TrimSpace(r.Header.Get("x-infinite-code-project")),
		"session":       strings.TrimSpace(r.Header.Get("x-infinite-code-session")),
		"request":       strings.TrimSpace(r.Header.Get("x-infinite-code-request")),
		"client":        strings.TrimSpace(r.Header.Get("x-infinite-code-client")),
		"userAgent":     r.UserAgent(),
		"planCode":      check.PlanCode,
		"planName":      check.PlanName,
		"credits":       check.Credits,
		"resetHours":    check.ResetHours,
		"windowStartAt": check.WindowFrom,
		"nextResetAt":   check.NextReset,
	}
}

func (s *Server) resolveInfiniteCodeQuota(ctx context.Context, userID string) (*infiniteCodeQuotaCheck, error) {
	planCode, err := s.Store.ResolveUserPlanCode(ctx, userID)
	if err != nil {
		return nil, err
	}
	config, err := s.Store.GetInfiniteCodeQuotaConfig(ctx)
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
	if sub, err := s.Store.GetSubscriptionByUser(ctx, userID); err == nil && !sub.StartedAt.IsZero() {
		anchor = sub.StartedAt.UTC()
	}
	windowStart, nextResetAt := infiniteCodeWindowBounds(anchor, planQuota.ResetHours, time.Now().UTC())
	used, err := s.Store.CountInfiniteCodeUsageSince(ctx, userID, windowStart)
	if err != nil {
		return nil, err
	}
	remaining := planQuota.Credits - used
	if remaining < 0 {
		remaining = 0
	}
	planName := planCode
	if plan, err := s.Store.GetPlan(ctx, planCode); err == nil && strings.TrimSpace(plan.Name) != "" {
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
