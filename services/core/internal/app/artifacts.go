package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type codeFenceBlock struct {
	Language string
	Code     string
}

func (s *Server) createArtifactsForAssistantMessage(ctx context.Context, userID string, conversationID string, messageID string, content string) []store.MessageArtifact {
	blocks := extractCodeFenceBlocks(content)
	if len(blocks) == 0 {
		return nil
	}
	out := make([]store.MessageArtifact, 0, len(blocks))
	for index, block := range blocks {
		kind := artifactKindForCode(block.Language, block.Code)
		if kind == "" {
			continue
		}
		files := artifactFilesForCode(kind, block.Language, block.Code)
		if len(files) == 0 {
			continue
		}
		title := artifactTitle(kind, index+1)
		artifact, err := s.Store.CreateChatArtifact(ctx, userID, conversationID, messageID, title, kind, files[0].Path, files)
		if err != nil {
			continue
		}
		out = append(out, store.MessageArtifact{
			ID:        artifact.ID,
			Title:     artifact.Title,
			Kind:      artifact.Kind,
			EntryFile: artifact.EntryFile,
			Language:  strings.TrimSpace(block.Language),
		})
	}
	return out
}

func extractCodeFenceBlocks(content string) []codeFenceBlock {
	matches := regexp.MustCompile("(?s)```([^\\n`]*)\\n(.*?)```").FindAllStringSubmatch(content, -1)
	blocks := make([]codeFenceBlock, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		language := ""
		if fields := strings.Fields(match[1]); len(fields) > 0 {
			language = strings.TrimSpace(fields[0])
		}
		code := strings.TrimSuffix(match[2], "\n")
		if strings.TrimSpace(code) == "" {
			continue
		}
		blocks = append(blocks, codeFenceBlock{Language: language, Code: code})
	}
	return blocks
}

func artifactKindForCode(language string, code string) string {
	lang := strings.ToLower(strings.TrimSpace(language))
	trimmed := strings.TrimSpace(code)
	switch lang {
	case "html", "htm":
		return "html"
	case "svg":
		return "svg"
	case "jsx", "tsx", "react":
		return "react"
	case "vue":
		return "vue"
	}
	if regexp.MustCompile(`(?is)^\s*(<!doctype html|<html[\s>])`).MatchString(trimmed) {
		return "html"
	}
	if regexp.MustCompile(`(?is)^\s*<svg[\s>]`).MatchString(trimmed) {
		return "svg"
	}
	if strings.Contains(trimmed, "ReactDOM") || strings.Contains(trimmed, "createRoot(") {
		return "react"
	}
	if strings.Contains(trimmed, "createApp(") || strings.Contains(trimmed, "<template>") {
		return "vue"
	}
	return ""
}

func artifactFilesForCode(kind string, language string, code string) []store.ArtifactFile {
	switch kind {
	case "html":
		return []store.ArtifactFile{{Path: "index.html", Language: "html", Content: code}}
	case "svg":
		return []store.ArtifactFile{{Path: "image.svg", Language: "svg", Content: code}}
	case "react":
		return []store.ArtifactFile{
			{Path: "index.html", Language: "html", Content: reactArtifactHTML()},
			{Path: "src/App.jsx", Language: languageOrDefault(language, "jsx"), Content: code},
		}
	case "vue":
		return []store.ArtifactFile{
			{Path: "index.html", Language: "html", Content: vueArtifactHTML()},
			{Path: "src/App.vue", Language: "vue", Content: code},
		}
	default:
		return nil
	}
}

func artifactTitle(kind string, index int) string {
	switch kind {
	case "svg":
		return fmt.Sprintf("SVG 预览 %d", index)
	case "react":
		return fmt.Sprintf("React 预览 %d", index)
	case "vue":
		return fmt.Sprintf("Vue 预览 %d", index)
	default:
		return fmt.Sprintf("HTML 预览 %d", index)
	}
}

func languageOrDefault(language string, fallback string) string {
	if strings.TrimSpace(language) == "" {
		return fallback
	}
	return strings.TrimSpace(language)
}

func reactArtifactHTML() string {
	return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Infinite-AI React 预览</title>
    <script type="importmap">
      {"imports":{"react":"https://esm.sh/react@19.2.5","react-dom/client":"https://esm.sh/react-dom@19.2.5/client"}}
    </script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="./src/App.jsx"></script>
  </body>
</html>`
}

func vueArtifactHTML() string {
	return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Infinite-AI Vue 预览</title>
    <script type="importmap">
      {"imports":{"vue":"https://esm.sh/vue@3"}}
    </script>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="./src/App.vue"></script>
  </body>
</html>`
}
