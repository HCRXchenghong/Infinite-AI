package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const infiniteCodeQuotaConfigKey = "infinite_code_quota_config"

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
