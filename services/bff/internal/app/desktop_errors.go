package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
)

const desktopServiceAlertsRedisKey = "infinite:service:alerts"

type infiniteCodeDesktopErrorRequest struct {
	Category     string            `json:"category"`
	Action       string            `json:"action"`
	Message      string            `json:"message"`
	Stack        string            `json:"stack"`
	Path         string            `json:"path"`
	SessionID    string            `json:"sessionId"`
	Directory    string            `json:"directory"`
	Model        string            `json:"model"`
	Provider     string            `json:"provider"`
	Version      string            `json:"version"`
	Platform     string            `json:"platform"`
	AccountEmail string            `json:"accountEmail"`
	Details      map[string]string `json:"details"`
}

type desktopServiceAlert struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	Source      string     `json:"source"`
	Path        string     `json:"path"`
	Model       string     `json:"model"`
	Status      string     `json:"status"`
	Account     string     `json:"account"`
	UserID      string     `json:"userId,omitempty"`
	KeyID       string     `json:"keyId,omitempty"`
	LatencyMs   int64      `json:"latencyMs"`
	ErrorDetail string     `json:"errorDetail"`
	ReadAt      *time.Time `json:"readAt,omitempty"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	ResolvedBy  string     `json:"resolvedBy,omitempty"`
}

func (s *Server) handleInfiniteCodeDesktopError(w http.ResponseWriter, r *http.Request) {
	var body infiniteCodeDesktopErrorRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	principal, ok := s.optionalInfiniteCodeTokenPrincipal(r)
	account := firstNonEmpty(principal.Email, body.AccountEmail, principal.UserID, "未知账号")
	s.recordDesktopServiceAlert(r.Context(), desktopServiceAlert{
		ID:          uuid.NewString(),
		CreatedAt:   time.Now().UTC(),
		Source:      "desktop-app",
		Path:        firstNonEmpty(body.Path, body.Action, "desktop-app"),
		Model:       firstNonEmpty(body.Model, body.Provider),
		Status:      "unread",
		Account:     account,
		UserID:      principal.UserID,
		ErrorDetail: desktopErrorDetail(body, ok, r),
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) optionalInfiniteCodeTokenPrincipal(r *http.Request) (auth.InfiniteCodeTokenPrincipal, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || s.Redis == nil {
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	raw, err := s.Redis.Get(r.Context(), auth.InfiniteCodeTokenRedisKey("access", token)).Result()
	if err != nil {
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	var principal auth.InfiniteCodeTokenPrincipal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil || strings.TrimSpace(principal.UserID) == "" {
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	if user, err := s.Store.GetUserByID(r.Context(), principal.UserID); err == nil && user.Status != "active" {
		return auth.InfiniteCodeTokenPrincipal{}, false
	}
	return principal, true
}

func (s *Server) recordDesktopServiceAlert(ctx context.Context, alert desktopServiceAlert) {
	if s.Redis == nil {
		return
	}
	raw, err := json.Marshal(alert)
	if err != nil {
		return
	}
	pipe := s.Redis.TxPipeline()
	pipe.LPush(context.WithoutCancel(ctx), desktopServiceAlertsRedisKey, raw)
	pipe.LTrim(context.WithoutCancel(ctx), desktopServiceAlertsRedisKey, 0, 499)
	_, _ = pipe.Exec(context.WithoutCancel(ctx))
}

func desktopErrorDetail(body infiniteCodeDesktopErrorRequest, authenticated bool, r *http.Request) string {
	lines := []string{
		"source=desktop-app",
		fmt.Sprintf("authenticated=%t", authenticated),
	}
	for _, item := range []struct {
		key   string
		value string
	}{
		{"category", body.Category},
		{"action", body.Action},
		{"path", body.Path},
		{"sessionId", body.SessionID},
		{"directory", body.Directory},
		{"model", body.Model},
		{"provider", body.Provider},
		{"version", body.Version},
		{"platform", body.Platform},
		{"remoteAddr", r.RemoteAddr},
		{"message", body.Message},
		{"stack", body.Stack},
	} {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		lines = append(lines, item.key+"="+compactDesktopErrorValue(item.value, 4000))
	}
	keys := make([]string, 0, len(body.Details))
	for key := range body.Details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(body.Details[key]) == "" {
			continue
		}
		lines = append(lines, "detail."+key+"="+compactDesktopErrorValue(body.Details[key], 1000))
	}
	return strings.Join(lines, "\n")
}

func compactDesktopErrorValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}
