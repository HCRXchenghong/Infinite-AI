package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const imagePromptPlannerSystemPrompt = `你是 Infinite-AI 的图片生成提示词规划器。你的任务是先理解用户的文字需求和参考图用途，再把它改写成给图片生成模型使用的高质量提示词。

只输出最终图片提示词，不要解释，不要寒暄，不要 Markdown，不要说“我将生成”。保留用户明确要求的主体、构图、镜头、风格、光线、背景、清晰度、数量和禁忌。

若有参考图，默认把参考图当作源图编辑，而不是只做风格参考：尽量保持参考图中的人物身份、脸部特征、主体轮廓、视角、姿态、服装和关键物体一致；只增删或修改用户明确要求变化的部分；不要无故换脸、换主体、换构图。只有当用户明确说“仅参考风格/氛围/配色”时，才把参考图作为风格参考。`

func (s *Server) planImageGenerationRequest(ctx context.Context, request imageGenerationRequest) imageGenerationRequest {
	if len(request.ReferenceImages) > 0 && strings.TrimSpace(request.InputFidelity) == "" {
		request.InputFidelity = "high"
	}
	plannedPrompt := s.planImagePrompt(ctx, request.Prompt, request.ReferenceImages)
	if strings.TrimSpace(plannedPrompt) == "" {
		return request
	}
	request.Prompt = plannedPrompt
	return request
}

func (s *Server) planImagePrompt(ctx context.Context, prompt string, references []imageReferenceInput) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	plannerCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var builder strings.Builder
	builder.WriteString("用户原始图片需求：\n")
	builder.WriteString(prompt)
	if len(references) > 0 {
		builder.WriteString("\n\n参考图：\n")
		for index, reference := range references {
			fileName := sanitizeFileName(reference.FileName)
			if fileName == "" {
				fileName = fmt.Sprintf("reference-%d", index+1)
			}
			mimeType := strings.TrimSpace(reference.MimeType)
			if mimeType == "" {
				mimeType = "image"
			}
			builder.WriteString(fmt.Sprintf("%d. %s (%s)\n", index+1, fileName, mimeType))
		}
	}
	builder.WriteString("\n\n请先分析需求，再只输出一段可直接传给图片生成/编辑接口的最终提示词。")
	planned, err := s.generateAIResponse(plannerCtx, s.Config.DefaultChatRoute, []aiMessage{
		{Role: "system", Content: imagePromptPlannerSystemPrompt},
		{Role: "user", Content: builder.String()},
	})
	if err != nil {
		return prompt
	}
	planned = sanitizeImagePlannedPrompt(planned)
	if planned == "" {
		return prompt
	}
	return planned
}

func sanitizeImagePlannedPrompt(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "`")
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"最终提示词：", "图片提示词：", "提示词：", "Prompt:", "prompt:"} {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	if strings.HasPrefix(value, "```") {
		lines := strings.Split(value, "\n")
		if len(lines) >= 3 {
			lines = lines[1:]
			if strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1]
			}
			value = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return clipRunes(value, 2400)
}
