package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

func (s *Store) CreateChatRun(ctx context.Context, userID string, conversationID string, modelSlug string, deepSearch bool, userMessageID string) (*ChatRun, error) {
	var item ChatRun
	var assistantMessageID *string
	var completedAt *string
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_runs (user_id, conversation_id, status, model_slug, deep_search, user_message_id)
		VALUES ($1, $2, 'running', $3, $4, NULLIF($5, '')::uuid)
		RETURNING id::text, user_id::text, conversation_id::text, status, model_slug, deep_search,
			COALESCE(user_message_id::text, ''), assistant_message_id::text, error_message, cancel_requested,
			created_at, updated_at, completed_at::text
	`, userID, conversationID, modelSlug, deepSearch, userMessageID).Scan(
		&item.ID,
		&item.UserID,
		&item.ConversationID,
		&item.Status,
		&item.ModelSlug,
		&item.DeepSearch,
		&item.UserMessageID,
		&assistantMessageID,
		&item.ErrorMessage,
		&item.CancelRequested,
		&item.CreatedAt,
		&item.UpdatedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}
	if assistantMessageID != nil {
		item.AssistantMessageID = *assistantMessageID
	}
	return &item, nil
}

func (s *Store) GetChatRun(ctx context.Context, userID string, runID string) (*ChatRun, error) {
	var item ChatRun
	var userMessageID *string
	var assistantMessageID *string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, user_id::text, conversation_id::text, status, model_slug, deep_search,
			user_message_id::text, assistant_message_id::text, error_message, cancel_requested,
			created_at, updated_at, completed_at
		FROM chat_runs
		WHERE id = $1 AND user_id = $2
	`, runID, userID).Scan(
		&item.ID,
		&item.UserID,
		&item.ConversationID,
		&item.Status,
		&item.ModelSlug,
		&item.DeepSearch,
		&userMessageID,
		&assistantMessageID,
		&item.ErrorMessage,
		&item.CancelRequested,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.CompletedAt,
	)
	if err != nil {
		return nil, scanNotFound(err)
	}
	if userMessageID != nil {
		item.UserMessageID = *userMessageID
	}
	if assistantMessageID != nil {
		item.AssistantMessageID = *assistantMessageID
	}
	return &item, nil
}

func (s *Store) ListActiveChatRuns(ctx context.Context, userID string, conversationID string) ([]ChatRun, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, user_id::text, conversation_id::text, status, model_slug, deep_search,
			COALESCE(user_message_id::text, ''), COALESCE(assistant_message_id::text, ''),
			error_message, cancel_requested, created_at, updated_at, completed_at
		FROM chat_runs
		WHERE user_id = $1
		  AND conversation_id = $2
		  AND status IN ('queued', 'running', 'canceling')
		ORDER BY created_at ASC
	`, userID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatRun
	for rows.Next() {
		var item ChatRun
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.ConversationID,
			&item.Status,
			&item.ModelSlug,
			&item.DeepSearch,
			&item.UserMessageID,
			&item.AssistantMessageID,
			&item.ErrorMessage,
			&item.CancelRequested,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.CompletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AppendChatRunEvent(ctx context.Context, runID string, eventType string, payload map[string]any) (*ChatRunEvent, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = "message"
	}
	var item ChatRunEvent
	var raw []byte
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_run_events (run_id, event_type, payload)
		VALUES ($1, $2, $3::jsonb)
		RETURNING seq, run_id::text, event_type, payload, created_at
	`, runID, eventType, marshalJSON(payload)).Scan(&item.Seq, &item.RunID, &item.EventType, &raw, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	item.Payload = map[string]any{}
	_ = json.Unmarshal(raw, &item.Payload)
	return &item, nil
}

func (s *Store) ListChatRunEventsAfter(ctx context.Context, userID string, runID string, afterSeq int64, limit int) ([]ChatRunEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `
		SELECT e.seq, e.run_id::text, e.event_type, e.payload, e.created_at
		FROM chat_run_events e
		JOIN chat_runs r ON r.id = e.run_id
		WHERE e.run_id = $1
		  AND r.user_id = $2
		  AND e.seq > $3
		ORDER BY e.seq ASC
		LIMIT $4
	`, runID, userID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChatRunEvent, 0, limit)
	for rows.Next() {
		var item ChatRunEvent
		var raw []byte
		if err := rows.Scan(&item.Seq, &item.RunID, &item.EventType, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Payload = map[string]any{}
		_ = json.Unmarshal(raw, &item.Payload)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) MarkChatRunCancelRequested(ctx context.Context, userID string, runID string) error {
	tag, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET cancel_requested = TRUE,
		    status = CASE WHEN status IN ('completed', 'failed', 'canceled') THEN status ELSE 'canceling' END,
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, runID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (s *Store) ChatRunCancelRequested(ctx context.Context, runID string) bool {
	var requested bool
	err := s.DB.QueryRow(ctx, `SELECT cancel_requested FROM chat_runs WHERE id = $1`, runID).Scan(&requested)
	return err == nil && requested
}

func (s *Store) CompleteChatRun(ctx context.Context, runID string, assistantMessageID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET status = 'completed',
		    assistant_message_id = NULLIF($2, '')::uuid,
		    error_message = '',
		    updated_at = NOW(),
		    completed_at = NOW()
		WHERE id = $1
		  AND status NOT IN ('failed', 'canceled')
	`, runID, assistantMessageID)
	return err
}

func (s *Store) FailChatRun(ctx context.Context, runID string, message string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET status = 'failed',
		    error_message = $2,
		    updated_at = NOW(),
		    completed_at = NOW()
		WHERE id = $1
		  AND status NOT IN ('completed', 'canceled')
	`, runID, strings.TrimSpace(message))
	return err
}

func (s *Store) CancelChatRun(ctx context.Context, runID string, assistantMessageID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET status = 'canceled',
		    assistant_message_id = NULLIF($2, '')::uuid,
		    cancel_requested = TRUE,
		    updated_at = NOW(),
		    completed_at = NOW()
		WHERE id = $1
	`, runID, assistantMessageID)
	return err
}

func (s *Store) TryCancelChatRun(ctx context.Context, runID string) (bool, error) {
	tag, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET status = 'canceled',
		    cancel_requested = TRUE,
		    updated_at = NOW(),
		    completed_at = COALESCE(completed_at, NOW())
		WHERE id = $1
		  AND status NOT IN ('completed', 'failed', 'canceled')
	`, runID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) AttachChatRunAssistantMessage(ctx context.Context, runID string, assistantMessageID string) error {
	if strings.TrimSpace(assistantMessageID) == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `
		UPDATE chat_runs
		SET assistant_message_id = NULLIF($2, '')::uuid,
		    updated_at = NOW()
		WHERE id = $1
	`, runID, assistantMessageID)
	return err
}

func (s *Store) EnsureChatRunOwned(ctx context.Context, userID string, runID string) error {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_runs WHERE id = $1 AND user_id = $2)`, runID, userID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return db.ErrNotFound
	}
	return nil
}

func IsChatRunTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func IsNotFound(err error) bool {
	return errors.Is(err, db.ErrNotFound) || errors.Is(err, pgx.ErrNoRows)
}
