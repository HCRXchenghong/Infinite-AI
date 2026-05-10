package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

type UserSettings struct {
	Theme              string `json:"theme"`
	Language           string `json:"language"`
	DeepSearchDefault  bool   `json:"deepSearchDefault"`
	SelectedModelSlug  string `json:"selectedModelSlug"`
	ChatHistoryEnabled bool   `json:"chatHistoryEnabled"`
	MemoryEnabled      bool   `json:"memoryEnabled"`
}

func (s *Store) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, title, model_slug, deep_search, created_at, updated_at
		FROM conversations
		WHERE user_id = $1
		ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var item Conversation
		if err := rows.Scan(&item.ID, &item.Title, &item.ModelSlug, &item.DeepSearch, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateConversation(ctx context.Context, userID string, title string, modelSlug string, deepSearch bool) (*Conversation, error) {
	if title == "" {
		title = "新聊天"
	}
	var item Conversation
	err := s.DB.QueryRow(ctx, `
		INSERT INTO conversations (user_id, title, model_slug, deep_search)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, title, model_slug, deep_search, created_at, updated_at
	`, userID, title, modelSlug, deepSearch).Scan(&item.ID, &item.Title, &item.ModelSlug, &item.DeepSearch, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) GetConversation(ctx context.Context, userID string, conversationID string) (*Conversation, error) {
	var item Conversation
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, title, model_slug, deep_search, created_at, updated_at
		FROM conversations
		WHERE id = $1 AND user_id = $2
	`, conversationID, userID).Scan(&item.ID, &item.Title, &item.ModelSlug, &item.DeepSearch, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &item, nil
}

func (s *Store) ListMessages(ctx context.Context, userID string, conversationID string) ([]Message, error) {
	if _, err := s.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, role, content, reasoning_content, attachments, sources, artifacts, model_slug, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var item Message
		var attachmentsRaw []byte
		var sourcesRaw []byte
		var artifactsRaw []byte
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &item.ReasoningContent, &attachmentsRaw, &sourcesRaw, &artifactsRaw, &item.ModelSlug, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Attachments = unmarshalAssets(attachmentsRaw)
		item.Sources = unmarshalSearchSources(sourcesRaw)
		item.Artifacts = unmarshalMessageArtifacts(artifactsRaw)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListRecentUserMemoryMessages(ctx context.Context, userID string, currentConversationID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 24
	}
	rows, err := s.DB.Query(ctx, `
		SELECT m.id::text, m.role, m.content, m.reasoning_content, m.attachments, m.sources, m.artifacts, m.model_slug, m.created_at
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE c.user_id = $1
		  AND m.role = 'user'
		  AND (NULLIF($2, '') IS NULL OR m.conversation_id <> NULLIF($2, '')::uuid)
		  AND btrim(m.content) <> ''
		ORDER BY m.created_at DESC
		LIMIT $3
	`, userID, currentConversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reversed []Message
	for rows.Next() {
		var item Message
		var attachmentsRaw []byte
		var sourcesRaw []byte
		var artifactsRaw []byte
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &item.ReasoningContent, &attachmentsRaw, &sourcesRaw, &artifactsRaw, &item.ModelSlug, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Content = strings.TrimSpace(item.Content)
		if item.Content == "" {
			continue
		}
		item.Attachments = unmarshalAssets(attachmentsRaw)
		item.Sources = unmarshalSearchSources(sourcesRaw)
		item.Artifacts = unmarshalMessageArtifacts(artifactsRaw)
		reversed = append(reversed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		out = append(out, reversed[index])
	}
	return out, nil
}

func (s *Store) CreateMessage(ctx context.Context, conversationID string, role string, content string, reasoningContent string, attachments []MessageAsset, modelSlug string) (*Message, error) {
	return s.CreateMessageWithExtras(ctx, conversationID, role, content, reasoningContent, attachments, nil, nil, modelSlug)
}

func (s *Store) CreateMessageWithExtras(ctx context.Context, conversationID string, role string, content string, reasoningContent string, attachments []MessageAsset, sources []SearchSource, artifacts []MessageArtifact, modelSlug string) (*Message, error) {
	var item Message
	var attachmentsRaw []byte
	var sourcesRaw []byte
	var artifactsRaw []byte
	err := s.DB.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, role, content, reasoning_content, attachments, sources, artifacts, model_slug, token_usage)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, '{}'::jsonb)
		RETURNING id::text, role, content, reasoning_content, attachments, sources, artifacts, model_slug, created_at
	`, conversationID, role, content, reasoningContent, marshalJSON(attachments), marshalJSON(sources), marshalJSON(artifacts), modelSlug).Scan(
		&item.ID,
		&item.Role,
		&item.Content,
		&item.ReasoningContent,
		&attachmentsRaw,
		&sourcesRaw,
		&artifactsRaw,
		&item.ModelSlug,
		&item.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.Attachments = unmarshalAssets(attachmentsRaw)
	item.Sources = unmarshalSearchSources(sourcesRaw)
	item.Artifacts = unmarshalMessageArtifacts(artifactsRaw)
	_, _ = s.DB.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, conversationID)
	return &item, nil
}

func (s *Store) RewriteConversationFromMessage(ctx context.Context, userID string, conversationID string, messageID string, content string, attachments []MessageAsset, modelSlug string) (*Message, error) {
	if _, err := s.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}

	var item Message
	var attachmentsRaw []byte
	var sourcesRaw []byte
	var artifactsRaw []byte
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `
			SELECT role
			FROM messages
			WHERE id = $1 AND conversation_id = $2
		`, messageID, conversationID).Scan(&role); err != nil {
			return scanNotFound(err)
		}
		if role != "user" {
			return fmt.Errorf("只有用户消息才可以重新编辑")
		}

		if _, err := tx.Exec(ctx, `
			WITH ordered AS (
				SELECT id, row_number() OVER (ORDER BY created_at ASC, id ASC) AS pos
				FROM messages
				WHERE conversation_id = $1
			),
			target AS (
				SELECT pos
				FROM ordered
				WHERE id = $2::uuid
			)
			DELETE FROM messages m
			USING ordered o, target t
			WHERE m.id = o.id
			  AND m.conversation_id = $1
			  AND o.pos > t.pos
		`, conversationID, messageID); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `
			UPDATE messages
			SET content = $3,
			    reasoning_content = '',
			    attachments = $4::jsonb,
			    sources = '[]'::jsonb,
			    artifacts = '[]'::jsonb,
			    model_slug = $5
			WHERE id = $2
			  AND conversation_id = $1
			  AND role = 'user'
			RETURNING id::text, role, content, reasoning_content, attachments, sources, artifacts, model_slug, created_at
		`, conversationID, messageID, content, marshalJSON(attachments), modelSlug).Scan(
			&item.ID,
			&item.Role,
			&item.Content,
			&item.ReasoningContent,
			&attachmentsRaw,
			&sourcesRaw,
			&artifactsRaw,
			&item.ModelSlug,
			&item.CreatedAt,
		); err != nil {
			return scanNotFound(err)
		}

		if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at = NOW() WHERE id = $1`, conversationID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	item.Attachments = unmarshalAssets(attachmentsRaw)
	item.Sources = unmarshalSearchSources(sourcesRaw)
	item.Artifacts = unmarshalMessageArtifacts(artifactsRaw)
	return &item, nil
}

func (s *Store) RenameConversation(ctx context.Context, conversationID string, title string) error {
	_, err := s.DB.Exec(ctx, `UPDATE conversations SET title = $2, updated_at = NOW() WHERE id = $1`, conversationID, title)
	return err
}

func (s *Store) DeleteConversation(ctx context.Context, userID string, conversationID string) error {
	commandTag, err := s.DB.Exec(ctx, `
		DELETE FROM conversations
		WHERE id = $1 AND user_id = $2
	`, conversationID, userID)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (s *Store) CreateAttachment(ctx context.Context, userID string, conversationID string, objectKey string, bucket string, fileName string, mimeType string, sizeBytes int64) (*MessageAsset, error) {
	var id string
	err := s.DB.QueryRow(ctx, `
		INSERT INTO attachments (user_id, conversation_id, object_key, bucket, file_name, mime_type, size_bytes, status)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, 'pending')
		RETURNING id::text
	`, userID, nullString(conversationID), objectKey, bucket, fileName, mimeType, sizeBytes).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &MessageAsset{
		ID:       id,
		FileName: fileName,
		MimeType: mimeType,
	}, nil
}

func (s *Store) GetAttachmentByID(ctx context.Context, userID string, attachmentID string) (*AttachmentRecord, error) {
	var item AttachmentRecord
	var conversationID *string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, user_id::text, conversation_id::text, object_key, bucket, file_name, mime_type, size_bytes, extracted_text, status, created_at
		FROM attachments
		WHERE id = $1 AND user_id = $2
	`, attachmentID, userID).Scan(
		&item.ID,
		&item.UserID,
		&conversationID,
		&item.ObjectKey,
		&item.Bucket,
		&item.FileName,
		&item.MimeType,
		&item.SizeBytes,
		&item.ExtractedText,
		&item.Status,
		&item.CreatedAt,
	)
	if err != nil {
		return nil, scanNotFound(err)
	}
	item.ConversationID = conversationID
	return &item, nil
}

func (s *Store) AssignAttachmentsToConversation(ctx context.Context, userID string, conversationID string, ids []string) error {
	if len(ids) == 0 || conversationID == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `
		UPDATE attachments
		SET conversation_id = $3
		WHERE user_id = $1 AND id = ANY($2)
	`, userID, ids, conversationID)
	return err
}

func (s *Store) CompleteAttachment(ctx context.Context, userID string, attachmentID string, extractedText string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE attachments
		SET status = 'ready', extracted_text = $3
		WHERE id = $1 AND user_id = $2
	`, attachmentID, userID, extractedText)
	return err
}

func (s *Store) CreateReadyAttachment(ctx context.Context, userID string, conversationID string, objectKey string, bucket string, fileName string, mimeType string, sizeBytes int64) (*MessageAsset, error) {
	var id string
	err := s.DB.QueryRow(ctx, `
		INSERT INTO attachments (user_id, conversation_id, object_key, bucket, file_name, mime_type, size_bytes, status)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, 'ready')
		RETURNING id::text
	`, userID, nullString(conversationID), objectKey, bucket, fileName, mimeType, sizeBytes).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &MessageAsset{
		ID:       id,
		FileName: fileName,
		MimeType: mimeType,
		URL:      "/chat/assets/" + id,
	}, nil
}

func (s *Store) GetAttachmentAssets(ctx context.Context, userID string, ids []string) ([]MessageAsset, error) {
	if len(ids) == 0 {
		return []MessageAsset{}, nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, file_name, mime_type
		FROM attachments
		WHERE user_id = $1 AND id = ANY($2) AND status = 'ready'
	`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageAsset
	for rows.Next() {
		var item MessageAsset
		if err := rows.Scan(&item.ID, &item.FileName, &item.MimeType); err != nil {
			return nil, err
		}
		item.URL = "/chat/assets/" + item.ID
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, name, prefix, scopes, status, rate_limit_per_minute, last_used_at, created_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var item APIKey
		var scopesRaw []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Prefix, &scopesRaw, &item.Status, &item.RateLimitPerMinute, &item.LastUsedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Scopes = unmarshalStrings(scopesRaw)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateAPIKey(ctx context.Context, userID string, name string, scopes []string, rateLimitPerMinute int, prefix string, hash string) (*APIKey, error) {
	var item APIKey
	var scopesRaw []byte
	err := s.DB.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, name, prefix, hash, scopes, status, rate_limit_per_minute)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'active', $6)
		RETURNING id::text, name, prefix, scopes, status, rate_limit_per_minute, last_used_at, created_at
	`, userID, name, prefix, hash, marshalJSON(scopes), rateLimitPerMinute).Scan(&item.ID, &item.Name, &item.Prefix, &scopesRaw, &item.Status, &item.RateLimitPerMinute, &item.LastUsedAt, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	item.Scopes = unmarshalStrings(scopesRaw)
	return &item, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, userID string, apiKeyID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE api_keys
		SET status = 'revoked', revoked_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, apiKeyID, userID)
	return err
}

func (s *Store) FindAPIKeyByHash(ctx context.Context, hash string) (*APIKey, string, error) {
	var item APIKey
	var scopesRaw []byte
	var userID string
	err := s.DB.QueryRow(ctx, `
		SELECT k.id::text, k.user_id::text, k.name, k.prefix, k.scopes, k.status, k.rate_limit_per_minute, k.last_used_at, k.created_at
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.hash = $1 AND k.status = 'active' AND u.status = 'active'
	`, hash).Scan(&item.ID, &userID, &item.Name, &item.Prefix, &scopesRaw, &item.Status, &item.RateLimitPerMinute, &item.LastUsedAt, &item.CreatedAt)
	if err != nil {
		return nil, "", scanNotFound(err)
	}
	item.Scopes = unmarshalStrings(scopesRaw)
	return &item, userID, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, apiKeyID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, apiKeyID)
	return err
}

func (s *Store) ListDownloadReleases(ctx context.Context) ([]DownloadRelease, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, platform, channel, version, title, description, download_url, status, created_at
		FROM download_releases
		ORDER BY platform ASC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownloadRelease
	for rows.Next() {
		var item DownloadRelease
		if err := rows.Scan(&item.ID, &item.Platform, &item.Channel, &item.Version, &item.Title, &item.Description, &item.DownloadURL, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetUserSettings(ctx context.Context, userID string) (*UserSettings, error) {
	if err := s.EnsureUserSettings(ctx, userID); err != nil {
		return nil, err
	}
	var settings UserSettings
	err := s.DB.QueryRow(ctx, `
		SELECT theme, language, deep_search_default, selected_model_slug, chat_history_enabled, memory_enabled
		FROM user_settings
		WHERE user_id = $1
	`, userID).Scan(&settings.Theme, &settings.Language, &settings.DeepSearchDefault, &settings.SelectedModelSlug, &settings.ChatHistoryEnabled, &settings.MemoryEnabled)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &settings, nil
}

func (s *Store) UpdateUserSettings(ctx context.Context, userID string, settings UserSettings) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE user_settings
		SET theme = $2, language = $3, deep_search_default = $4, selected_model_slug = $5, chat_history_enabled = $6, memory_enabled = $7, updated_at = NOW()
		WHERE user_id = $1
	`, userID, settings.Theme, settings.Language, settings.DeepSearchDefault, settings.SelectedModelSlug, settings.ChatHistoryEnabled, settings.MemoryEnabled)
	return err
}

func (s *Store) ClearUserChats(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM conversations WHERE user_id = $1`, userID)
	return err
}

func (s *Store) ExportUserData(ctx context.Context, userID string) (map[string]any, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	settings, _ := s.GetUserSettings(ctx, userID)
	conversations, _ := s.ListConversations(ctx, userID)
	keys, _ := s.ListAPIKeys(ctx, userID)
	conversationExports := make([]map[string]any, 0, len(conversations))
	for _, conversation := range conversations {
		messages, _ := s.ListMessages(ctx, userID, conversation.ID)
		conversationExports = append(conversationExports, map[string]any{
			"conversation": conversation,
			"messages":     messages,
		})
	}
	export := map[string]any{
		"user":          user,
		"settings":      settings,
		"conversations": conversationExports,
		"apiKeys":       keys,
		"exportedAt":    time.Now().UTC(),
	}
	return export, nil
}

func (s *Store) DeleteUserAccount(ctx context.Context, userID string) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE users
			SET status = 'deleted', email = CONCAT('deleted+', id::text, '@infinite.local'), updated_at = NOW()
			WHERE id = $1
		`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE api_keys
			SET status = 'revoked', revoked_at = NOW()
			WHERE user_id = $1 AND status <> 'revoked'
		`, userID); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) GetQuotaSummary(ctx context.Context, userID string) (map[string]any, error) {
	var total float64
	err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(MAX(balance_after), 0)
		FROM quota_ledgers
		WHERE user_id = $1
	`, userID).Scan(&total)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"balance": total,
		"hint":    fmt.Sprintf("%.2f units remaining", total),
	}, nil
}

func (s *Store) AppendQuotaLedger(ctx context.Context, userID string, eventType string, amount float64, source string, referenceID string, metadata map[string]any) error {
	var current float64
	_ = s.DB.QueryRow(ctx, `
		SELECT COALESCE(MAX(balance_after), 0)
		FROM quota_ledgers
		WHERE user_id = $1
	`, userID).Scan(&current)
	next := current + amount
	_, err := s.DB.Exec(ctx, `
		INSERT INTO quota_ledgers (user_id, event_type, amount, balance_after, source, reference_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`, userID, eventType, amount, next, source, referenceID, marshalJSON(metadata))
	return err
}
