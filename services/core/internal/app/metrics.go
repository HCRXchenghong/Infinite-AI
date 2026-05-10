package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

const apiStatsRedisKey = "infinite:api:stats"
const serviceAlertsRedisKey = "infinite:service:alerts"

type apiStatEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source"`
	Path        string    `json:"path"`
	UserID      string    `json:"userId,omitempty"`
	KeyID       string    `json:"keyId,omitempty"`
	Account     string    `json:"account,omitempty"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	LatencyMs   int64     `json:"latencyMs"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
}

type serviceAlert struct {
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

func (s *Server) recordAPIStat(ctx context.Context, entry apiStatEntry) {
	if s.Redis == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	pipe := s.Redis.TxPipeline()
	pipe.LPush(ctx, apiStatsRedisKey, raw)
	pipe.LTrim(ctx, apiStatsRedisKey, 0, 299)
	_, _ = pipe.Exec(ctx)
	if shouldCreateServiceAlert(entry) {
		s.recordServiceAlert(ctx, entry)
	}
}

func shouldCreateServiceAlert(entry apiStatEntry) bool {
	if entry.Status == "" || entry.Status == "ok" || entry.Status == "quota_exhausted" || entry.Status == "forbidden" || entry.Status == "rate_limited" {
		return false
	}
	if entry.ErrorDetail == "" {
		return false
	}
	return true
}

func (s *Server) recordServiceAlert(ctx context.Context, entry apiStatEntry) {
	if s.Redis == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	createdAt := entry.Timestamp
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	alert := serviceAlert{
		ID:          uuid.NewString(),
		CreatedAt:   createdAt,
		Source:      entry.Source,
		Path:        entry.Path,
		Model:       entry.Model,
		Status:      "unread",
		Account:     entry.Account,
		UserID:      entry.UserID,
		KeyID:       entry.KeyID,
		LatencyMs:   entry.LatencyMs,
		ErrorDetail: entry.ErrorDetail,
	}
	if alert.Account == "" {
		if entry.UserID != "" {
			alert.Account = entry.UserID
		} else {
			alert.Account = "未知账号"
		}
	}
	raw, err := json.Marshal(alert)
	if err != nil {
		return
	}
	pipe := s.Redis.TxPipeline()
	pipe.LPush(ctx, serviceAlertsRedisKey, raw)
	pipe.LTrim(ctx, serviceAlertsRedisKey, 0, 499)
	_, _ = pipe.Exec(ctx)
}

func (s *Server) loadAPIStats(ctx context.Context, limit int64) ([]apiStatEntry, error) {
	if s.Redis == nil {
		return []apiStatEntry{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := s.Redis.LRange(ctx, apiStatsRedisKey, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]apiStatEntry, 0, len(items))
	for _, item := range items {
		var entry apiStatEntry
		if err := json.Unmarshal([]byte(item), &entry); err == nil {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (s *Server) loadServiceAlerts(ctx context.Context, limit int64) ([]serviceAlert, error) {
	if s.Redis == nil {
		return []serviceAlert{}, nil
	}
	if limit <= 0 {
		limit = 200
	}
	items, err := s.Redis.LRange(ctx, serviceAlertsRedisKey, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]serviceAlert, 0, len(items))
	for _, item := range items {
		var alert serviceAlert
		if err := json.Unmarshal([]byte(item), &alert); err == nil {
			out = append(out, alert)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Server) updateServiceAlert(ctx context.Context, alertID string, mutator func(*serviceAlert) bool) error {
	if s.Redis == nil {
		return nil
	}
	items, err := s.Redis.LRange(ctx, serviceAlertsRedisKey, 0, 499).Result()
	if err != nil {
		return err
	}
	for index, item := range items {
		var alert serviceAlert
		if err := json.Unmarshal([]byte(item), &alert); err != nil {
			continue
		}
		if alert.ID != alertID {
			continue
		}
		if !mutator(&alert) {
			return nil
		}
		raw, err := json.Marshal(alert)
		if err != nil {
			return err
		}
		return s.Redis.LSet(ctx, serviceAlertsRedisKey, int64(index), raw).Err()
	}
	return nil
}

func summarizeAPIStats(entries []apiStatEntry) map[string]any {
	if len(entries) == 0 {
		return map[string]any{
			"totalRequests": 0,
			"successRate":   "0.0%",
			"avgLatencyMs":  0,
			"errorCount":    0,
		}
	}
	var successCount int
	var latencyTotal int64
	for _, entry := range entries {
		latencyTotal += entry.LatencyMs
		if entry.Status == "ok" {
			successCount++
		}
	}
	errorCount := len(entries) - successCount
	return map[string]any{
		"totalRequests": len(entries),
		"successRate":   formatRate(successCount, len(entries)),
		"avgLatencyMs":  latencyTotal / int64(len(entries)),
		"errorCount":    errorCount,
	}
}

func formatRate(numerator int, denominator int) string {
	if denominator <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(numerator)/float64(denominator)*100)
}
