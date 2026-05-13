package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const infiniteCodeQuotaConfigKey = "infinite_code_quota_config"
const infiniteCodeModelLimitsKey = "infinite_code_model_limits"

func defaultInfiniteCodeQuotaConfig() map[string]InfiniteCodeQuotaPlan {
	return map[string]InfiniteCodeQuotaPlan{
		"free":      {Credits: 20, ResetHours: 24},
		"go":        {Credits: 80, ResetHours: 24},
		"plus":      {Credits: 240, ResetHours: 24},
		"pro_basic": {Credits: 800, ResetHours: 24},
		"pro_max":   {Credits: 3000, ResetHours: 24},
	}
}

func normalizeInfiniteCodeQuotaConfig(config map[string]InfiniteCodeQuotaPlan) map[string]InfiniteCodeQuotaPlan {
	defaults := defaultInfiniteCodeQuotaConfig()
	out := make(map[string]InfiniteCodeQuotaPlan, len(defaults))
	for planCode, plan := range defaults {
		out[planCode] = plan
	}
	for planCode, plan := range config {
		planCode = strings.TrimSpace(planCode)
		if planCode == "" {
			continue
		}
		if plan.Credits < 0 {
			plan.Credits = 0
		}
		if plan.ResetHours <= 0 {
			plan.ResetHours = out[planCode].ResetHours
			if plan.ResetHours <= 0 {
				plan.ResetHours = 24
			}
		}
		out[planCode] = plan
	}
	return out
}

func (s *Store) GetInfiniteCodeQuotaConfig(ctx context.Context) (map[string]InfiniteCodeQuotaPlan, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, infiniteCodeQuotaConfigKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultInfiniteCodeQuotaConfig(), nil
		}
		return nil, err
	}
	config := map[string]InfiniteCodeQuotaPlan{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	return normalizeInfiniteCodeQuotaConfig(config), nil
}

func (s *Store) UpdateInfiniteCodeQuotaConfig(ctx context.Context, config map[string]InfiniteCodeQuotaPlan) error {
	normalized := normalizeInfiniteCodeQuotaConfig(config)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, infiniteCodeQuotaConfigKey, marshalJSON(normalized))
	return err
}

func (s *Store) SeedInfiniteCodeQuotaConfig(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO NOTHING
	`, infiniteCodeQuotaConfigKey, marshalJSON(defaultInfiniteCodeQuotaConfig()))
	return err
}

func (s *Store) GetInfiniteCodeModelLimits(ctx context.Context) (map[string]map[string]int, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, infiniteCodeModelLimitsKey).Scan(&raw)
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

func (s *Store) UpdateInfiniteCodeModelLimits(ctx context.Context, limits map[string]map[string]int) error {
	normalized := normalizeModelMembershipLimits(limits)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, infiniteCodeModelLimitsKey, marshalJSON(normalized))
	return err
}

func (s *Store) CountInfiniteCodeUsageSince(ctx context.Context, userID string, since time.Time) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE
			WHEN event_type = 'infinite_code_usage' THEN 1
			WHEN event_type = 'infinite_code_refund' THEN -1
			ELSE 0
		END), 0)::int
		FROM quota_ledgers
		WHERE user_id = $1
		  AND event_type IN ('infinite_code_usage', 'infinite_code_refund')
		  AND created_at >= $2
	`, userID, since).Scan(&count)
	if count < 0 {
		count = 0
	}
	return count, err
}

func (s *Store) ReserveInfiniteCodeUsage(ctx context.Context, userID string, since time.Time, limit int, source string, referenceID string, metadata map[string]any) (int, bool, error) {
	var usedAfter int
	allowed := false
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var lockedUserID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID); err != nil {
			return err
		}
		var used int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(CASE
				WHEN event_type = 'infinite_code_usage' THEN 1
				WHEN event_type = 'infinite_code_refund' THEN -1
				ELSE 0
			END), 0)::int
			FROM quota_ledgers
			WHERE user_id = $1
			  AND event_type IN ('infinite_code_usage', 'infinite_code_refund')
			  AND created_at >= $2
		`, userID, since).Scan(&used); err != nil {
			return err
		}
		if used < 0 {
			used = 0
		}
		if limit <= 0 || used >= limit {
			usedAfter = used
			return nil
		}
		var current float64
		if err := tx.QueryRow(ctx, `
			SELECT balance_after
			FROM quota_ledgers
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, userID).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		next := current - 1
		if _, err := tx.Exec(ctx, `
			INSERT INTO quota_ledgers (user_id, event_type, amount, balance_after, source, reference_id, metadata)
			VALUES ($1, 'infinite_code_usage', -1, $2, $3, $4, $5::jsonb)
		`, userID, next, source, referenceID, marshalJSON(metadata)); err != nil {
			return err
		}
		usedAfter = used + 1
		allowed = true
		return nil
	})
	return usedAfter, allowed, err
}

func (s *Store) CountInfiniteCodeModelUsageSince(ctx context.Context, userID string, model string, since time.Time) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE
			WHEN event_type = 'infinite_code_usage' THEN 1
			WHEN event_type = 'infinite_code_refund' THEN -1
			ELSE 0
		END), 0)::int
		FROM quota_ledgers
		WHERE user_id = $1
		  AND metadata->>'model' = $2
		  AND event_type IN ('infinite_code_usage', 'infinite_code_refund')
		  AND created_at >= $3
	`, userID, model, since).Scan(&count)
	if count < 0 {
		count = 0
	}
	return count, err
}

func (s *Store) RefundInfiniteCodeUsage(ctx context.Context, userID string, referenceID string, metadata map[string]any) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var lockedUserID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID); err != nil {
			return err
		}
		var existingRefunds int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM quota_ledgers
			WHERE user_id = $1
			  AND event_type = 'infinite_code_refund'
			  AND reference_id = $2
		`, userID, referenceID).Scan(&existingRefunds); err != nil {
			return err
		}
		if existingRefunds > 0 {
			return nil
		}
		var existingCharges int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM quota_ledgers
			WHERE user_id = $1
			  AND event_type = 'infinite_code_usage'
			  AND reference_id = $2
		`, userID, referenceID).Scan(&existingCharges); err != nil {
			return err
		}
		if existingCharges == 0 {
			return nil
		}
		var current float64
		if err := tx.QueryRow(ctx, `
			SELECT balance_after
			FROM quota_ledgers
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`, userID).Scan(&current); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		next := current + 1
		_, err := tx.Exec(ctx, `
			INSERT INTO quota_ledgers (user_id, event_type, amount, balance_after, source, reference_id, metadata)
			VALUES ($1, 'infinite_code_refund', 1, $2, 'infinite_code', $3, $4::jsonb)
		`, userID, next, referenceID, marshalJSON(metadata))
		return err
	})
}
