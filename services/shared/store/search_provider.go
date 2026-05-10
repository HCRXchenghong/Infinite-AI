package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const searchProviderSettingKey = "search_provider"

func defaultSearchProviderConfig() *SearchProviderConfig {
	return &SearchProviderConfig{
		Enabled:        true,
		Provider:       "openai_then_searxng",
		BaseURL:        "http://searxng:8080",
		ResultCount:    5,
		TimeoutSeconds: 8,
	}
}

func normalizeSearchProviderConfig(config *SearchProviderConfig) *SearchProviderConfig {
	out := defaultSearchProviderConfig()
	if config == nil {
		return out
	}
	out.Enabled = config.Enabled
	if provider := strings.TrimSpace(config.Provider); provider != "" && provider != "searxng" {
		out.Provider = provider
	}
	if baseURL := strings.TrimSpace(config.BaseURL); baseURL != "" {
		out.BaseURL = strings.TrimRight(baseURL, "/")
	}
	if config.ResultCount > 0 && config.ResultCount <= 20 {
		out.ResultCount = config.ResultCount
	}
	if config.TimeoutSeconds > 0 && config.TimeoutSeconds <= 30 {
		out.TimeoutSeconds = config.TimeoutSeconds
	}
	return out
}

func (s *Store) GetSearchProviderConfig(ctx context.Context) (*SearchProviderConfig, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, searchProviderSettingKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultSearchProviderConfig(), nil
		}
		return nil, err
	}
	var config SearchProviderConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	return normalizeSearchProviderConfig(&config), nil
}

func (s *Store) UpdateSearchProviderConfig(ctx context.Context, config SearchProviderConfig) error {
	normalized := normalizeSearchProviderConfig(&config)
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, searchProviderSettingKey, marshalJSON(normalized))
	return err
}
