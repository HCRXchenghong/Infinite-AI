package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

func (s *Store) ResolveUserPlanCode(ctx context.Context, userID string) (string, error) {
	sub, err := s.GetSubscriptionByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, db.ErrNotFound) {
			return "free", nil
		}
		return "free", err
	}
	if sub == nil {
		return "free", nil
	}
	if sub.Status != "active" {
		return "free", nil
	}
	if sub.EndsAt != nil && sub.EndsAt.Before(time.Now()) {
		return "free", nil
	}
	planCode := strings.TrimSpace(sub.PlanCode)
	if planCode == "" {
		return "free", nil
	}
	return planCode, nil
}

func (s *Store) CountSuccessfulModelResponsesSince(ctx context.Context, userID string, modelSlug string, since time.Time) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE c.user_id = $1
		  AND m.model_slug = $2
		  AND m.role IN ('assistant', 'ai')
		  AND m.created_at >= $3
	`, userID, modelSlug, since).Scan(&count)
	return count, err
}

func (s *Store) GetModelMembershipLimits(ctx context.Context) (map[string]map[string]int, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = 'model_membership_limits'`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]map[string]int{}, nil
		}
		return nil, err
	}
	limits := map[string]map[string]int{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &limits); err != nil {
			return nil, err
		}
	}
	return normalizeModelMembershipLimits(limits), nil
}

func (s *Store) UpdateModelMembershipLimits(ctx context.Context, limits map[string]map[string]int) error {
	normalized := normalizeModelMembershipLimits(limits)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('model_membership_limits', $1::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, marshalJSON(normalized))
	return err
}

func (s *Store) GetModelContextLimits(ctx context.Context) (*ModelContextLimits, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = 'model_context_limits'`).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return normalizeModelContextLimits(nil), nil
		}
		return nil, err
	}
	var limits ModelContextLimits
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &limits); err != nil {
			return nil, err
		}
	}
	return normalizeModelContextLimits(&limits), nil
}

func (s *Store) UpdateModelContextLimits(ctx context.Context, limits ModelContextLimits) error {
	normalized := normalizeModelContextLimits(&limits)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('model_context_limits', $1::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, marshalJSON(normalized))
	return err
}

func normalizeModelMembershipLimits(limits map[string]map[string]int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(limits))
	for planCode, models := range limits {
		planCode = strings.TrimSpace(planCode)
		if planCode == "" {
			continue
		}
		nextModels := map[string]int{}
		for modelSlug, limit := range models {
			modelSlug = strings.TrimSpace(modelSlug)
			if modelSlug == "" || limit < 0 {
				continue
			}
			nextModels[modelSlug] = limit
		}
		out[planCode] = nextModels
	}
	return out
}

func normalizeModelContextLimits(limits *ModelContextLimits) *ModelContextLimits {
	out := &ModelContextLimits{
		Default: 0,
		Models:  map[string]int{},
		Plans:   map[string]map[string]int{},
		Users:   map[string]map[string]int{},
	}
	if limits == nil {
		return out
	}
	if limits.Default > 0 {
		out.Default = limits.Default
	}
	out.Models = normalizePositiveIntMap(limits.Models)
	out.Plans = normalizePositiveNestedIntMap(limits.Plans)
	out.Users = normalizePositiveNestedIntMap(limits.Users)
	return out
}

func normalizePositiveIntMap(value map[string]int) map[string]int {
	out := make(map[string]int, len(value))
	for key, limit := range value {
		key = strings.TrimSpace(key)
		if key == "" || limit <= 0 {
			continue
		}
		out[key] = limit
	}
	return out
}

func normalizePositiveNestedIntMap(value map[string]map[string]int) map[string]map[string]int {
	out := make(map[string]map[string]int, len(value))
	for outerKey, inner := range value {
		outerKey = strings.TrimSpace(outerKey)
		if outerKey == "" {
			continue
		}
		nextInner := map[string]int{}
		for innerKey, limit := range inner {
			innerKey = strings.TrimSpace(innerKey)
			if innerKey == "" || limit <= 0 {
				continue
			}
			nextInner[innerKey] = limit
		}
		if len(nextInner) > 0 {
			out[outerKey] = nextInner
		}
	}
	return out
}
