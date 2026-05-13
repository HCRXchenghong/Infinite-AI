package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListModelRoutes(ctx context.Context, includeSecrets bool) ([]ModelRoute, error) {
	return s.listModelRoutes(ctx, includeSecrets, false)
}

func (s *Store) ListActiveModelRoutes(ctx context.Context, includeSecrets bool) ([]ModelRoute, error) {
	return s.listModelRoutes(ctx, includeSecrets, true)
}

func (s *Store) listModelRoutes(ctx context.Context, includeSecrets bool, activeOnly bool) ([]ModelRoute, error) {
	query := `
		SELECT id::text, slug, name, protocol, strategy, model_type, upstream_model, description, sort_order, prompt_enabled, prompt_text, active
		FROM model_routes
	`
	if activeOnly {
		query += ` WHERE active = TRUE`
	}
	query += ` ORDER BY sort_order ASC, created_at ASC`
	rows, err := s.DB.Query(ctx, `
	`+query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ModelRoute, 0)
	for rows.Next() {
		var route ModelRoute
		if err := rows.Scan(&route.ID, &route.Slug, &route.Name, &route.Protocol, &route.Strategy, &route.ModelType, &route.UpstreamModel, &route.Description, &route.SortOrder, &route.PromptEnabled, &route.PromptText, &route.Active); err != nil {
			return nil, err
		}
		if !includeSecrets {
			route.PromptEnabled = false
			route.PromptText = ""
		}
		endpoints, err := s.ListModelEndpoints(ctx, route.ID, includeSecrets)
		if err != nil {
			return nil, err
		}
		route.Endpoints = endpoints
		out = append(out, route)
	}
	return out, rows.Err()
}

func (s *Store) ListModelEndpoints(ctx context.Context, routeID string, includeSecrets bool) ([]ModelEndpoint, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, sort_order, base_url, secret_enc, active
		FROM model_endpoints
		WHERE route_id = $1
		ORDER BY sort_order ASC, created_at ASC
	`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ModelEndpoint, 0)
	for rows.Next() {
		var endpoint ModelEndpoint
		var secretEnc string
		if err := rows.Scan(&endpoint.ID, &endpoint.SortOrder, &endpoint.BaseURL, &secretEnc, &endpoint.Active); err != nil {
			return nil, err
		}
		if includeSecrets {
			endpoint.Secret, _ = s.Decrypt(secretEnc)
		}
		out = append(out, endpoint)
	}
	return out, rows.Err()
}

func (s *Store) GetModelRouteBySlug(ctx context.Context, slug string, includeSecrets bool) (*ModelRoute, error) {
	var route ModelRoute
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, slug, name, protocol, strategy, model_type, upstream_model, description, sort_order, prompt_enabled, prompt_text, active
		FROM model_routes
		WHERE slug = $1
	`, slug).Scan(&route.ID, &route.Slug, &route.Name, &route.Protocol, &route.Strategy, &route.ModelType, &route.UpstreamModel, &route.Description, &route.SortOrder, &route.PromptEnabled, &route.PromptText, &route.Active)
	if err != nil {
		return nil, scanNotFound(err)
	}
	if !includeSecrets {
		route.PromptEnabled = false
		route.PromptText = ""
	}
	endpoints, err := s.ListModelEndpoints(ctx, route.ID, includeSecrets)
	if err != nil {
		return nil, err
	}
	route.Endpoints = endpoints
	return &route, nil
}

func (s *Store) FindActiveModelRoute(ctx context.Context, selector string, includeSecrets bool) (*ModelRoute, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, scanNotFound(pgx.ErrNoRows)
	}
	var route ModelRoute
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, slug, name, protocol, strategy, model_type, upstream_model, description, sort_order, prompt_enabled, prompt_text, active
		FROM model_routes
		WHERE active = TRUE AND (slug = $1 OR upstream_model = $1)
		ORDER BY CASE WHEN slug = $1 THEN 0 ELSE 1 END, updated_at DESC, created_at DESC
		LIMIT 1
	`, selector).Scan(&route.ID, &route.Slug, &route.Name, &route.Protocol, &route.Strategy, &route.ModelType, &route.UpstreamModel, &route.Description, &route.SortOrder, &route.PromptEnabled, &route.PromptText, &route.Active)
	if err != nil {
		return nil, scanNotFound(err)
	}
	if !includeSecrets {
		route.PromptEnabled = false
		route.PromptText = ""
	}
	endpoints, err := s.ListModelEndpoints(ctx, route.ID, includeSecrets)
	if err != nil {
		return nil, err
	}
	route.Endpoints = endpoints
	return &route, nil
}

func (s *Store) UpsertModelRoute(ctx context.Context, route ModelRoute) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var routeID string
		err := tx.QueryRow(ctx, `
			INSERT INTO model_routes (slug, name, protocol, strategy, model_type, upstream_model, description, sort_order, prompt_enabled, prompt_text, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (slug) DO UPDATE SET
				name = EXCLUDED.name,
				protocol = EXCLUDED.protocol,
				strategy = EXCLUDED.strategy,
				model_type = EXCLUDED.model_type,
				upstream_model = EXCLUDED.upstream_model,
				description = EXCLUDED.description,
				sort_order = EXCLUDED.sort_order,
				prompt_enabled = EXCLUDED.prompt_enabled,
				prompt_text = EXCLUDED.prompt_text,
				active = EXCLUDED.active,
				updated_at = NOW()
			RETURNING id::text
		`, route.Slug, route.Name, route.Protocol, route.Strategy, route.ModelType, route.UpstreamModel, route.Description, route.SortOrder, route.PromptEnabled, route.PromptText, route.Active).Scan(&routeID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM model_endpoints WHERE route_id = $1`, routeID); err != nil {
			return err
		}
		for index, endpoint := range route.Endpoints {
			secretEnc, err := s.Encrypt(endpoint.Secret)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO model_endpoints (route_id, sort_order, base_url, secret_enc, extra_headers, active)
				VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
			`, routeID, index, endpoint.BaseURL, secretEnc, endpoint.Active); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DeleteModelRoute(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `DELETE FROM model_routes WHERE slug = $1`, slug)
	return err
}
