package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const systemLogRetention = 7 * 24 * time.Hour

type SystemLogEntry struct {
	ID          string         `json:"id"`
	Service     string         `json:"service"`
	Level       string         `json:"level"`
	Category    string         `json:"category"`
	EventType   string         `json:"eventType"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	StatusCode  int            `json:"statusCode"`
	UserID      string         `json:"userId,omitempty"`
	AdminID     string         `json:"adminId,omitempty"`
	Account     string         `json:"account,omitempty"`
	IP          string         `json:"ip,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Message     string         `json:"message"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type CreateSystemLogInput struct {
	Service     string
	Level       string
	Category    string
	EventType   string
	Method      string
	Path        string
	StatusCode  int
	UserID      string
	AdminID     string
	Account     string
	IP          string
	Fingerprint string
	Message     string
	Payload     map[string]any
}

func (s *Store) CreateSystemLog(ctx context.Context, input CreateSystemLogInput) error {
	if strings.TrimSpace(input.Level) == "" {
		input.Level = "info"
	}
	if strings.TrimSpace(input.Category) == "" {
		input.Category = "request"
	}
	if strings.TrimSpace(input.Message) == "" {
		input.Message = "request completed"
	}
	if _, err := s.DB.Exec(ctx, `
		INSERT INTO system_logs (
			service, level, category, event_type, method, path, status_code,
			user_id, admin_id, account, ip, fingerprint, message, payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
	`, strings.TrimSpace(input.Service), strings.TrimSpace(input.Level), strings.TrimSpace(input.Category), strings.TrimSpace(input.EventType),
		strings.TrimSpace(input.Method), strings.TrimSpace(input.Path), input.StatusCode, strings.TrimSpace(input.UserID),
		strings.TrimSpace(input.AdminID), strings.TrimSpace(input.Account), strings.TrimSpace(input.IP), strings.TrimSpace(input.Fingerprint),
		strings.TrimSpace(input.Message), marshalJSON(input.Payload)); err != nil {
		return err
	}
	return s.PruneSystemLogs(ctx)
}

func (s *Store) PruneSystemLogs(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		DELETE FROM system_logs
		WHERE created_at < NOW() - INTERVAL '7 days'
	`)
	return err
}

func (s *Store) ListSystemLogs(ctx context.Context, limit int) ([]SystemLogEntry, error) {
	query := `
		SELECT id::text, service, level, category, event_type, method, path, status_code,
			user_id, admin_id, account, ip, fingerprint, message, payload, created_at
		FROM system_logs
		WHERE created_at >= NOW() - INTERVAL '7 days'
		ORDER BY created_at DESC
	`
	var rows pgx.Rows
	var err error
	if limit > 0 {
		query += "\nLIMIT $1"
		rows, err = s.DB.Query(ctx, query, limit)
	} else {
		rows, err = s.DB.Query(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	capHint := limit
	if capHint <= 0 {
		capHint = 128
	}
	out := make([]SystemLogEntry, 0, capHint)
	for rows.Next() {
		var item SystemLogEntry
		var payloadRaw []byte
		if err := rows.Scan(
			&item.ID, &item.Service, &item.Level, &item.Category, &item.EventType, &item.Method, &item.Path, &item.StatusCode,
			&item.UserID, &item.AdminID, &item.Account, &item.IP, &item.Fingerprint, &item.Message, &payloadRaw, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(payloadRaw) > 0 {
			_ = json.Unmarshal(payloadRaw, &item.Payload)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
