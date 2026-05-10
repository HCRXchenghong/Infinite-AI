package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

var ErrShareCollaboratorLimitReached = errors.New("share collaborator limit reached")

func (s *Store) GetConversationByID(ctx context.Context, conversationID string) (*Conversation, error) {
	var item Conversation
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, title, model_slug, deep_search, created_at, updated_at
		FROM conversations
		WHERE id = $1
	`, conversationID).Scan(&item.ID, &item.Title, &item.ModelSlug, &item.DeepSearch, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &item, nil
}

func (s *Store) ListMessagesByConversationID(ctx context.Context, conversationID string) ([]Message, error) {
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

func (s *Store) GetConversationShareForOwner(ctx context.Context, userID string, conversationID string) (*ConversationShare, error) {
	if _, err := s.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	var item ConversationShare
	var accessCodeEnc string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, user_id::text, is_active, require_access_code, access_code_enc, collaboration_enabled, created_at, updated_at
		FROM conversation_shares
		WHERE conversation_id = $1 AND user_id = $2
	`, conversationID, userID).Scan(
		&item.ID,
		&item.ConversationID,
		&item.UserID,
		&item.IsActive,
		&item.RequireAccessCode,
		&accessCodeEnc,
		&item.CollaborationEnabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, scanNotFound(err)
	}
	if strings.TrimSpace(accessCodeEnc) != "" {
		item.AccessCode, _ = s.Decrypt(accessCodeEnc)
	}
	return &item, nil
}

func (s *Store) UpsertConversationShare(ctx context.Context, userID string, conversationID string, enabled bool, collaborationCode string, collaborationEnabled bool) (*ConversationShare, error) {
	if _, err := s.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	existing, err := s.GetConversationShareForOwner(ctx, userID, conversationID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	requireAccessCode := false
	accessCode := strings.TrimSpace(collaborationCode)
	accessCodeEnc := ""
	switch {
	case collaborationEnabled && accessCode != "":
		accessCodeEnc, err = s.Encrypt(accessCode)
		if err != nil {
			return nil, err
		}
	case collaborationEnabled && existing != nil && strings.TrimSpace(existing.AccessCode) != "":
		accessCodeEnc, err = s.Encrypt(existing.AccessCode)
		if err != nil {
			return nil, err
		}
		accessCode = existing.AccessCode
	case !collaborationEnabled:
		accessCode = ""
	}
	var item ConversationShare
	var accessCodeEncOut string
	err = s.DB.QueryRow(ctx, `
		INSERT INTO conversation_shares (
			conversation_id, user_id, is_active, require_access_code, access_code_enc, collaboration_enabled, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (conversation_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			is_active = EXCLUDED.is_active,
			require_access_code = EXCLUDED.require_access_code,
			access_code_enc = EXCLUDED.access_code_enc,
			collaboration_enabled = EXCLUDED.collaboration_enabled,
			updated_at = NOW()
		RETURNING id::text, conversation_id::text, user_id::text, is_active, require_access_code, access_code_enc, collaboration_enabled, created_at, updated_at
	`, conversationID, userID, enabled, requireAccessCode, accessCodeEnc, collaborationEnabled).Scan(
		&item.ID,
		&item.ConversationID,
		&item.UserID,
		&item.IsActive,
		&item.RequireAccessCode,
		&accessCodeEncOut,
		&item.CollaborationEnabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(accessCodeEncOut) != "" {
		item.AccessCode, _ = s.Decrypt(accessCodeEncOut)
	}
	return &item, nil
}

func (s *Store) GetActiveConversationShareByID(ctx context.Context, shareID string) (*ConversationShare, error) {
	var item ConversationShare
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, user_id::text, is_active, require_access_code, collaboration_enabled, created_at, updated_at
		FROM conversation_shares
		WHERE id = $1 AND is_active = TRUE
	`, shareID).Scan(
		&item.ID,
		&item.ConversationID,
		&item.UserID,
		&item.IsActive,
		&item.RequireAccessCode,
		&item.CollaborationEnabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &item, nil
}

func (s *Store) ConversationShareAccessAllowed(ctx context.Context, shareID string, accessCode string) (bool, error) {
	var accessCodeEnc string
	err := s.DB.QueryRow(ctx, `
		SELECT access_code_enc
		FROM conversation_shares
		WHERE id = $1 AND is_active = TRUE
	`, shareID).Scan(&accessCodeEnc)
	if err != nil {
		return false, scanNotFound(err)
	}
	if strings.TrimSpace(accessCodeEnc) == "" {
		return false, nil
	}
	expected, err := s.Decrypt(accessCodeEnc)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(expected) != "" && strings.TrimSpace(expected) == strings.TrimSpace(accessCode), nil
}

func (s *Store) CountConversationShareCollaborators(ctx context.Context, shareID string) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM conversation_share_collaborators
		WHERE share_id = $1
	`, shareID).Scan(&count)
	return count, err
}

func (s *Store) IsConversationShareCollaborator(ctx context.Context, shareID string, userID string) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM conversation_share_collaborators
			WHERE share_id = $1 AND user_id = $2
		)
	`, shareID, userID).Scan(&exists)
	return exists, err
}

func (s *Store) EnsureConversationShareCollaborator(ctx context.Context, shareID string, userID string, limit int) error {
	return s.Tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM conversation_share_collaborators
				WHERE share_id = $1 AND user_id = $2
			)
		`, shareID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM conversation_share_collaborators
			WHERE share_id = $1
		`, shareID).Scan(&count); err != nil {
			return err
		}
		if limit >= 0 && count >= limit {
			return ErrShareCollaboratorLimitReached
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO conversation_share_collaborators (share_id, user_id, created_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (share_id, user_id) DO NOTHING
		`, shareID, userID)
		return err
	})
}

func (s *Store) GetSharedConversationAttachment(ctx context.Context, shareID string, attachmentID string) (*AttachmentRecord, error) {
	var item AttachmentRecord
	var conversationID *string
	err := s.DB.QueryRow(ctx, `
		SELECT a.id::text, a.user_id::text, a.conversation_id::text, a.object_key, a.bucket, a.file_name, a.mime_type, a.size_bytes, a.extracted_text, a.status, a.created_at
		FROM attachments a
		JOIN conversation_shares cs ON cs.conversation_id = a.conversation_id
		WHERE cs.id = $1
		  AND cs.is_active = TRUE
		  AND a.id = $2
	`, shareID, attachmentID).Scan(
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

func (s *Store) CountUserImageAttachmentsForDay(ctx context.Context, userID string, month int, day int, fileNamePrefix string) (int, error) {
	var count int
	err := s.DB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM attachments
		WHERE user_id = $1
		  AND mime_type LIKE 'image/%'
		  AND status = 'ready'
		  AND file_name LIKE $2
		  AND EXTRACT(MONTH FROM created_at AT TIME ZONE 'UTC') = $3
		  AND EXTRACT(DAY FROM created_at AT TIME ZONE 'UTC') = $4
	`, userID, fileNamePrefix+"%", month, day).Scan(&count)
	return count, err
}
