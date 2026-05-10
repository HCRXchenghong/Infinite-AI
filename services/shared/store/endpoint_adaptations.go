package store

import (
	"context"
	"strings"
	"time"
)

type EndpointAdaptation struct {
	Operation     string    `json:"operation"`
	Protocol      string    `json:"protocol"`
	RouteSlug     string    `json:"routeSlug"`
	UpstreamModel string    `json:"upstreamModel"`
	BaseURL       string    `json:"baseUrl"`
	Kind          string    `json:"kind"`
	URL           string    `json:"url"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (s *Store) ListEndpointAdaptations(ctx context.Context) ([]EndpointAdaptation, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT operation, protocol, route_slug, upstream_model, base_url, kind, url, updated_at
		FROM endpoint_adaptations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EndpointAdaptation, 0)
	for rows.Next() {
		var item EndpointAdaptation
		if err := rows.Scan(&item.Operation, &item.Protocol, &item.RouteSlug, &item.UpstreamModel, &item.BaseURL, &item.Kind, &item.URL, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertEndpointAdaptation(ctx context.Context, item EndpointAdaptation) error {
	item.Operation = strings.TrimSpace(item.Operation)
	item.Protocol = strings.TrimSpace(item.Protocol)
	item.RouteSlug = strings.TrimSpace(item.RouteSlug)
	item.UpstreamModel = strings.TrimSpace(item.UpstreamModel)
	item.BaseURL = strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
	item.Kind = strings.TrimSpace(item.Kind)
	item.URL = strings.TrimSpace(item.URL)
	if item.Protocol == "" {
		item.Protocol = "openai"
	}
	if item.Operation == "" || item.BaseURL == "" || item.Kind == "" || item.URL == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `
		INSERT INTO endpoint_adaptations (operation, protocol, route_slug, upstream_model, base_url, kind, url)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (operation, protocol, route_slug, upstream_model, base_url) DO UPDATE SET
			kind = EXCLUDED.kind,
			url = EXCLUDED.url,
			updated_at = NOW()
	`, item.Operation, item.Protocol, item.RouteSlug, item.UpstreamModel, item.BaseURL, item.Kind, item.URL)
	return err
}
