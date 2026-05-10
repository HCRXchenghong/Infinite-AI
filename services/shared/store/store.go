package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
	"github.com/seron-cheng/infinite-ai/services/shared/crypto"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
)

type Store struct {
	DB     *pgxpool.Pool
	Config config.Config
}

func New(pool *pgxpool.Pool, cfg config.Config) *Store {
	return &Store{
		DB:     pool,
		Config: cfg,
	}
}

func (s *Store) Encrypt(value string) (string, error) {
	return crypto.Encrypt(s.Config.MasterKey, value)
}

func (s *Store) Decrypt(value string) (string, error) {
	return crypto.Decrypt(s.Config.MasterKey, value)
}

func marshalJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func unmarshalStrings(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []string{}
	}
	return out
}

func unmarshalAssets(raw []byte) []MessageAsset {
	if len(raw) == 0 {
		return []MessageAsset{}
	}
	var out []MessageAsset
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []MessageAsset{}
	}
	return out
}

func unmarshalSearchSources(raw []byte) []SearchSource {
	if len(raw) == 0 {
		return []SearchSource{}
	}
	var out []SearchSource
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []SearchSource{}
	}
	return out
}

func unmarshalMessageArtifacts(raw []byte) []MessageArtifact {
	if len(raw) == 0 {
		return []MessageArtifact{}
	}
	var out []MessageArtifact
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []MessageArtifact{}
	}
	return out
}

func unmarshalArtifactFiles(raw []byte) []ArtifactFile {
	if len(raw) == 0 {
		return []ArtifactFile{}
	}
	var out []ArtifactFile
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []ArtifactFile{}
	}
	return out
}

func scanNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ErrNotFound
	}
	return err
}

func (s *Store) now() time.Time {
	return time.Now().UTC()
}

func (s *Store) Health(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Store) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func secretPreview(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 6 {
		return value
	}
	return fmt.Sprintf("%s••••", value[:6])
}
