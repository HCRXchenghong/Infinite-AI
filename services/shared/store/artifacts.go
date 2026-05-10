package store

import (
	"context"
	"strings"
)

func (s *Store) CreateChatArtifact(ctx context.Context, userID string, conversationID string, messageID string, title string, kind string, entryFile string, files []ArtifactFile) (*ChatArtifact, error) {
	if strings.TrimSpace(title) == "" {
		title = "代码预览"
	}
	if strings.TrimSpace(kind) == "" {
		kind = "html"
	}
	if strings.TrimSpace(entryFile) == "" {
		entryFile = "index.html"
	}
	var item ChatArtifact
	var filesRaw []byte
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_artifacts (user_id, conversation_id, message_id, title, kind, entry_file, files, version)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7::jsonb, 1)
		RETURNING id::text, user_id::text, COALESCE(conversation_id::text, ''), COALESCE(message_id::text, ''),
			title, kind, entry_file, files, version, created_at, updated_at
	`, userID, conversationID, messageID, title, kind, entryFile, marshalJSON(files)).Scan(
		&item.ID,
		&item.UserID,
		&item.ConversationID,
		&item.MessageID,
		&item.Title,
		&item.Kind,
		&item.EntryFile,
		&filesRaw,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.Files = unmarshalArtifactFiles(filesRaw)
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO chat_artifact_versions (artifact_id, version, files)
		VALUES ($1, 1, $2::jsonb)
		ON CONFLICT (artifact_id, version) DO NOTHING
	`, item.ID, marshalJSON(files))
	return &item, nil
}

func (s *Store) AttachArtifactsToMessage(ctx context.Context, messageID string, artifacts []MessageArtifact) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE messages
		SET artifacts = $2::jsonb
		WHERE id = $1
	`, messageID, marshalJSON(artifacts))
	return err
}

func (s *Store) GetChatArtifact(ctx context.Context, userID string, artifactID string) (*ChatArtifact, error) {
	var item ChatArtifact
	var filesRaw []byte
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, user_id::text, COALESCE(conversation_id::text, ''), COALESCE(message_id::text, ''),
			title, kind, entry_file, files, version, created_at, updated_at
		FROM chat_artifacts
		WHERE id = $1 AND user_id = $2
	`, artifactID, userID).Scan(
		&item.ID,
		&item.UserID,
		&item.ConversationID,
		&item.MessageID,
		&item.Title,
		&item.Kind,
		&item.EntryFile,
		&filesRaw,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, scanNotFound(err)
	}
	item.Files = unmarshalArtifactFiles(filesRaw)
	return &item, nil
}

func (s *Store) CreateChatArtifactVersion(ctx context.Context, userID string, artifactID string, files []ArtifactFile) (*ChatArtifactVersion, error) {
	if _, err := s.GetChatArtifact(ctx, userID, artifactID); err != nil {
		return nil, err
	}
	var nextVersion int
	if err := s.DB.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM chat_artifact_versions WHERE artifact_id = $1`, artifactID).Scan(&nextVersion); err != nil {
		return nil, err
	}
	var item ChatArtifactVersion
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_artifact_versions (artifact_id, version, files)
		VALUES ($1, $2, $3::jsonb)
		RETURNING id::text, version, created_at
	`, artifactID, nextVersion, marshalJSON(files)).Scan(&item.ID, &item.Version, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	item.Files = files
	_, _ = s.DB.Exec(ctx, `
		UPDATE chat_artifacts
		SET files = $2::jsonb, version = $3, updated_at = NOW()
		WHERE id = $1
	`, artifactID, marshalJSON(files), nextVersion)
	return &item, nil
}

func (s *Store) ListChatArtifactVersions(ctx context.Context, userID string, artifactID string) ([]ChatArtifactVersion, error) {
	if _, err := s.GetChatArtifact(ctx, userID, artifactID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, version, created_at
		FROM chat_artifact_versions
		WHERE artifact_id = $1
		ORDER BY version DESC
	`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatArtifactVersion
	for rows.Next() {
		var item ChatArtifactVersion
		if err := rows.Scan(&item.ID, &item.Version, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
