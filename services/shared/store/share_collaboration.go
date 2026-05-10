package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const shareCollaborationConfigKey = "share_collaboration_config"

func defaultShareCollaborationConfig() map[string]ShareCollaborationPlan {
	return map[string]ShareCollaborationPlan{
		"free":      {MaxCollaborators: 0},
		"go":        {MaxCollaborators: 0},
		"plus":      {MaxCollaborators: 2},
		"pro_basic": {MaxCollaborators: 5},
		"pro_max":   {MaxCollaborators: 10},
	}
}

func normalizeShareCollaborationConfig(config map[string]ShareCollaborationPlan) map[string]ShareCollaborationPlan {
	defaults := defaultShareCollaborationConfig()
	out := make(map[string]ShareCollaborationPlan, len(defaults))
	for planCode, plan := range defaults {
		out[planCode] = plan
	}
	for planCode, plan := range config {
		planCode = strings.TrimSpace(planCode)
		if planCode == "" {
			continue
		}
		if plan.MaxCollaborators < 0 {
			plan.MaxCollaborators = 0
		}
		out[planCode] = plan
	}
	return out
}

func (s *Store) GetShareCollaborationConfig(ctx context.Context) (map[string]ShareCollaborationPlan, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, shareCollaborationConfigKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultShareCollaborationConfig(), nil
		}
		return nil, err
	}
	config := map[string]ShareCollaborationPlan{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	return normalizeShareCollaborationConfig(config), nil
}

func (s *Store) UpdateShareCollaborationConfig(ctx context.Context, config map[string]ShareCollaborationPlan) error {
	normalized := normalizeShareCollaborationConfig(config)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, shareCollaborationConfigKey, marshalJSON(normalized))
	return err
}

func (s *Store) SeedShareCollaborationConfig(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO NOTHING
	`, shareCollaborationConfigKey, marshalJSON(defaultShareCollaborationConfig()))
	return err
}

func (s *Store) ResolveShareCollaborationLimit(ctx context.Context, userID string) (string, int, error) {
	planCode, err := s.ResolveUserPlanCode(ctx, userID)
	if err != nil || strings.TrimSpace(planCode) == "" {
		planCode = "free"
	}
	config, err := s.GetShareCollaborationConfig(ctx)
	if err != nil {
		return planCode, 0, err
	}
	limit := config[planCode].MaxCollaborators
	if limit < 0 {
		limit = 0
	}
	return planCode, limit, nil
}
