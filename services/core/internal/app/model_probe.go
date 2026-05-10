package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type modelProbeSummary struct {
	Status              string `json:"status"`
	SuccessRate         int    `json:"successRate"`
	SuccessfulEndpoints int    `json:"successfulEndpoints"`
	ActiveEndpoints     int    `json:"activeEndpoints"`
	TotalEndpoints      int    `json:"totalEndpoints"`
	AvgLatencyMs        int64  `json:"avgLatencyMs"`
	Message             string `json:"message"`
}

type modelProbeEndpointResult struct {
	Index     int    `json:"index"`
	BaseURL   string `json:"baseUrl"`
	Active    bool   `json:"active"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

type modelProbeResult struct {
	ModelSlug   string                     `json:"modelSlug"`
	ModelName   string                     `json:"modelName"`
	ModelType   string                     `json:"modelType"`
	Protocol    string                     `json:"protocol"`
	Strategy    string                     `json:"strategy"`
	Upstream    string                     `json:"upstreamModel"`
	Summary     modelProbeSummary          `json:"summary"`
	Endpoints   []modelProbeEndpointResult `json:"endpoints"`
	CheckedAt   string                     `json:"checkedAt"`
	CheckedUnix int64                      `json:"checkedUnix"`
}

func (s *Server) probeModelRoute(ctx context.Context, route store.ModelRoute) *modelProbeResult {
	if strings.TrimSpace(route.Protocol) == "" {
		route.Protocol = "openai"
	}
	if strings.TrimSpace(route.ModelType) == "" {
		route.ModelType = "chat"
	}
	results := make([]modelProbeEndpointResult, 0, len(route.Endpoints))
	successCount := 0
	activeCount := 0
	var totalLatency int64
	for index, endpoint := range route.Endpoints {
		item := modelProbeEndpointResult{
			Index:   index + 1,
			BaseURL: endpoint.BaseURL,
			Active:  endpoint.Active,
			Status:  "skipped",
			Message: "已跳过",
		}
		if !endpoint.Active {
			item.Message = "端点未启用"
			results = append(results, item)
			continue
		}
		activeCount++
		if strings.TrimSpace(endpoint.BaseURL) == "" || strings.TrimSpace(endpoint.Secret) == "" {
			item.Status = "failed"
			item.Message = "缺少 Base URL 或密钥"
			results = append(results, item)
			continue
		}
		startedAt := time.Now()
		message, err := s.probeSingleEndpoint(ctx, route, endpoint)
		item.LatencyMs = time.Since(startedAt).Milliseconds()
		totalLatency += item.LatencyMs
		if err != nil {
			item.Status = "failed"
			item.Message = httpx.UserFacingMessage(http.StatusBadGateway, "model_probe_failed", err.Error())
		} else {
			item.Status = "success"
			item.Message = message
			successCount++
		}
		results = append(results, item)
	}

	summary := modelProbeSummary{
		Status:              "missing",
		SuccessRate:         0,
		SuccessfulEndpoints: successCount,
		ActiveEndpoints:     activeCount,
		TotalEndpoints:      len(route.Endpoints),
		AvgLatencyMs:        0,
		Message:             "还没有可检测的有效端点",
	}
	if activeCount > 0 {
		summary.SuccessRate = int(float64(successCount) / float64(activeCount) * 100)
		summary.AvgLatencyMs = totalLatency / int64(activeCount)
		switch {
		case successCount == activeCount:
			summary.Status = "healthy"
			summary.Message = fmt.Sprintf("%d / %d 个启用端点全部连通", successCount, activeCount)
		case successCount > 0:
			summary.Status = "degraded"
			summary.Message = fmt.Sprintf("%d / %d 个启用端点连通，建议检查失败节点", successCount, activeCount)
		default:
			summary.Status = "failed"
			summary.Message = "所有启用端点都未通过检测"
		}
	}

	checkedAt := time.Now().Local()
	return &modelProbeResult{
		ModelSlug:   route.Slug,
		ModelName:   route.Name,
		ModelType:   route.ModelType,
		Protocol:    route.Protocol,
		Strategy:    route.Strategy,
		Upstream:    route.UpstreamModel,
		Summary:     summary,
		Endpoints:   results,
		CheckedAt:   checkedAt.Format("2006-01-02 15:04:05"),
		CheckedUnix: checkedAt.Unix(),
	}
}

func (s *Server) probeSingleEndpoint(ctx context.Context, route store.ModelRoute, endpoint store.ModelEndpoint) (string, error) {
	switch route.ModelType {
	case "image":
		return s.probeImageEndpoint(ctx, route, endpoint)
	default:
		reply, err := s.callProvider(ctx, &route, endpoint, []aiMessage{
			{Role: "system", Content: "You are a health-check assistant."},
			{Role: "user", Content: "Reply with OK only."},
		})
		if err != nil {
			return "", err
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			return "返回成功，但内容为空", nil
		}
		return fmt.Sprintf("返回成功：%s", clipProbeText(reply)), nil
	}
}

func (s *Server) probeImageEndpoint(ctx context.Context, route store.ModelRoute, endpoint store.ModelEndpoint) (string, error) {
	if route.Protocol != "openai" {
		return "", fmt.Errorf("图片模型测试暂时只支持 OpenAI 兼容协议")
	}
	if message, err := s.probeOpenAIModelCatalog(ctx, route, endpoint); err == nil {
		return message, nil
	}
	_, err := s.callOpenAIImageGeneration(ctx, &route, endpoint, imageGenerationRequest{
		Model:  route.Slug,
		Prompt: "health-check image",
		N:      1,
	})
	if err != nil {
		return "", err
	}
	return "图片生成接口连通成功", nil
}

func (s *Server) probeOpenAIModelCatalog(ctx context.Context, route store.ModelRoute, endpoint store.ModelEndpoint) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var lastErr error
	candidates := openAIEndpointCandidates(endpoint.BaseURL, "/models")
	if cached, ok := s.getOpenAIEndpointAdaptation(&route, endpoint, openAIAdapterOperationModels); ok {
		candidates = openAIEndpointCandidatesWithPreferred(endpoint.BaseURL, "/models", cached.URL)
	}
	for _, target := range candidates {
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		if res.StatusCode >= 300 {
			bodyText := readUpstreamErrorBody(res.Body)
			_ = res.Body.Close()
			lastErr = newUpstreamHTTPError("模型目录检测", res.StatusCode, bodyText)
			if shouldTryNextOpenAIEndpointCandidate(res.StatusCode, bodyText) {
				continue
			}
			return "", lastErr
		}
		var payload struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			_ = res.Body.Close()
			return "", err
		}
		_ = res.Body.Close()
		if strings.TrimSpace(route.UpstreamModel) == "" {
			s.rememberOpenAIEndpointAdaptation(&route, endpoint, openAIAdapterOperationModels, openAIEndpointAdaptation{
				Kind: "models",
				URL:  target,
			})
			return "模型目录接口连通成功", nil
		}
		for _, item := range payload.Data {
			if item.ID == route.UpstreamModel {
				s.rememberOpenAIEndpointAdaptation(&route, endpoint, openAIAdapterOperationModels, openAIEndpointAdaptation{
					Kind: "models",
					URL:  target,
				})
				return fmt.Sprintf("模型目录连通成功，已识别 %s", route.UpstreamModel), nil
			}
		}
		if len(payload.Data) == 0 {
			s.rememberOpenAIEndpointAdaptation(&route, endpoint, openAIAdapterOperationModels, openAIEndpointAdaptation{
				Kind: "models",
				URL:  target,
			})
			return "模型目录接口连通成功，但未返回模型列表", nil
		}
		return "", fmt.Errorf("模型目录已连通，但未找到 %s", route.UpstreamModel)
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("模型目录检测失败: OpenAI 兼容端点未配置")
}

func clipProbeText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 80 {
		return value
	}
	return value[:80] + "..."
}
