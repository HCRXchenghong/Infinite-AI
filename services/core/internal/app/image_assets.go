package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) persistGeneratedImageAssets(ctx context.Context, userID string, conversationID string, payload map[string]any) ([]store.MessageAsset, map[string]any) {
	data, ok := payload["data"].([]any)
	if !ok || len(data) == 0 {
		return extractGeneratedImageAssets(payload), payload
	}
	user, _ := s.Store.GetUserByID(ctx, userID)
	now := time.Now().UTC()
	displayName := "user"
	if user != nil && strings.TrimSpace(user.DisplayName) != "" {
		displayName = sanitizeFileName(strings.ToLower(user.DisplayName))
	}
	fileNamePrefix := fmt.Sprintf("infinite-ai-%s-%02d-%02d-", displayName, int(now.Month()), now.Day())
	baseCount, _ := s.Store.CountUserImageAttachmentsForDay(ctx, userID, int(now.Month()), now.Day(), fileNamePrefix)
	nextData := make([]any, 0, len(data))
	assets := make([]store.MessageAsset, 0, len(data))
	for index, item := range data {
		entry, ok := item.(map[string]any)
		if !ok {
			nextData = append(nextData, item)
			continue
		}
		nextEntry := map[string]any{}
		for key, value := range entry {
			if key != "b64_json" {
				nextEntry[key] = value
			}
		}
		if rawURL, ok := entry["url"].(string); ok && strings.TrimSpace(rawURL) != "" && !strings.HasPrefix(rawURL, "data:") {
			fileName := fmt.Sprintf("%s%03d.png", fileNamePrefix, baseCount+index+1)
			asset := store.MessageAsset{
				ID:       fmt.Sprintf("generated-image-%d", index+1),
				FileName: fileName,
				MimeType: "image/png",
				URL:      strings.TrimSpace(rawURL),
			}
			assets = append(assets, asset)
			nextEntry["url"] = asset.URL
			nextData = append(nextData, nextEntry)
			continue
		}
		b64, _ := entry["b64_json"].(string)
		if b64 == "" {
			if rawURL, ok := entry["url"].(string); ok && strings.HasPrefix(rawURL, "data:") {
				if comma := strings.Index(rawURL, ","); comma >= 0 {
					b64 = rawURL[comma+1:]
				}
			}
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil || len(decoded) == 0 {
			nextData = append(nextData, nextEntry)
			continue
		}
		fileName := fmt.Sprintf("%s%03d.png", fileNamePrefix, baseCount+index+1)
		objectKey := fmt.Sprintf("users/%s/generated-images/%d-%d.png", userID, time.Now().UnixNano(), index+1)
		if _, err := s.MinIO.PutObject(ctx, s.Config.MinIOBucket, objectKey, bytes.NewReader(decoded), int64(len(decoded)), minio.PutObjectOptions{ContentType: "image/png"}); err != nil {
			nextData = append(nextData, nextEntry)
			continue
		}
		asset, err := s.Store.CreateReadyAttachment(ctx, userID, conversationID, objectKey, s.Config.MinIOBucket, fileName, "image/png", int64(len(decoded)))
		if err != nil {
			nextData = append(nextData, nextEntry)
			continue
		}
		assets = append(assets, *asset)
		nextEntry["url"] = asset.URL
		nextData = append(nextData, nextEntry)
	}
	nextPayload := map[string]any{}
	for key, value := range payload {
		nextPayload[key] = value
	}
	nextPayload["data"] = nextData
	return assets, nextPayload
}
