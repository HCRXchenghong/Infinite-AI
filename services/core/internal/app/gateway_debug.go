package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func (s *Server) debugEmailGateway(ctx context.Context, actor principal, email string) (map[string]any, error) {
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("请输入有效的邮箱地址")
	}
	authSecurity, err := s.Store.GetAuthSecuritySettings(ctx)
	if err != nil {
		return nil, err
	}
	code := "654321"
	if authSecurity.VerificationTestMode {
		if err := s.Store.CreateSystemLog(ctx, store.CreateSystemLogInput{
			Service:   "core",
			Level:     "info",
			Category:  "auth",
			EventType: "email_gateway_debug",
			Method:    http.MethodPost,
			Path:      "/admin/settings/email-gateway/test",
			AdminID:   actor.subjectID,
			Account:   firstNonEmptyGateway(actor.email, email),
			Message:   "邮箱网关调试已写入系统日志",
			Payload: map[string]any{
				"target":       email,
				"code":         code,
				"deliveryMode": "test",
			},
		}); err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":           true,
			"deliveryMode": "test",
			"previewCode":  code,
			"message":      "测试模式已开启，未向真实邮箱发送，已写入系统日志。",
		}, nil
	}
	gateway, err := s.Store.GetEmailGatewayConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !gateway.Enabled || strings.TrimSpace(gateway.EndpointURL) == "" {
		return nil, fmt.Errorf("邮箱网关未配置完成")
	}
	if err := sendEmailViaGateway(ctx, gateway, email, code, "debug", 10); err != nil {
		return nil, err
	}
	if err := s.Store.CreateSystemLog(ctx, store.CreateSystemLogInput{
		Service:   "core",
		Level:     "info",
		Category:  "auth",
		EventType: "email_gateway_debug",
		Method:    http.MethodPost,
		Path:      "/admin/settings/email-gateway/test",
		AdminID:   actor.subjectID,
		Account:   firstNonEmptyGateway(actor.email, email),
		Message:   "邮箱网关调试已发送",
		Payload: map[string]any{
			"target":       email,
			"code":         code,
			"deliveryMode": "email",
			"providerName": gateway.ProviderName,
		},
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":           true,
		"deliveryMode": "email",
		"previewCode":  code,
		"message":      "调试邮件已发送，请检查收件箱。",
	}, nil
}

func (s *Server) debugSMSGateway(ctx context.Context, actor principal, phone string) (map[string]any, error) {
	if strings.TrimSpace(phone) == "" {
		return nil, fmt.Errorf("请输入有效的手机号")
	}
	authSecurity, err := s.Store.GetAuthSecuritySettings(ctx)
	if err != nil {
		return nil, err
	}
	code := "654321"
	if authSecurity.VerificationTestMode {
		if err := s.Store.CreateSystemLog(ctx, store.CreateSystemLogInput{
			Service:   "core",
			Level:     "info",
			Category:  "auth",
			EventType: "sms_gateway_debug",
			Method:    http.MethodPost,
			Path:      "/admin/settings/sms-gateway/test",
			AdminID:   actor.subjectID,
			Account:   firstNonEmptyGateway(actor.email, phone),
			Message:   "短信网关调试已写入系统日志",
			Payload: map[string]any{
				"target":       phone,
				"code":         code,
				"deliveryMode": "test",
			},
		}); err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":           true,
			"deliveryMode": "test",
			"previewCode":  code,
			"message":      "测试模式已开启，未向真实手机发送，已写入系统日志。",
		}, nil
	}
	gateway, err := s.Store.GetSMSGatewayConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !gateway.Enabled || strings.TrimSpace(gateway.EndpointURL) == "" {
		return nil, fmt.Errorf("短信网关未配置完成")
	}
	if err := sendSMSViaGateway(ctx, gateway, phone, code, "debug", 10); err != nil {
		return nil, err
	}
	if err := s.Store.CreateSystemLog(ctx, store.CreateSystemLogInput{
		Service:   "core",
		Level:     "info",
		Category:  "auth",
		EventType: "sms_gateway_debug",
		Method:    http.MethodPost,
		Path:      "/admin/settings/sms-gateway/test",
		AdminID:   actor.subjectID,
		Account:   firstNonEmptyGateway(actor.email, phone),
		Message:   "短信网关调试已发送",
		Payload: map[string]any{
			"target":       phone,
			"code":         code,
			"deliveryMode": "sms",
			"providerName": gateway.ProviderName,
		},
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":           true,
		"deliveryMode": "sms",
		"previewCode":  code,
		"message":      "调试短信已发送，请检查手机。",
	}, nil
}

func sendSMSViaGateway(ctx context.Context, gateway *store.SMSGatewayConfig, phone string, code string, purpose string, ttlMinutes int) error {
	if ttlMinutes <= 0 {
		ttlMinutes = 5
	}
	message := gateway.MessageTemplate
	if strings.TrimSpace(message) == "" {
		message = "【Infinite-AI】您的验证码是 {{code}}，{{minutes}} 分钟内有效。"
	}
	message = strings.ReplaceAll(message, "{{code}}", code)
	message = strings.ReplaceAll(message, "{{minutes}}", fmt.Sprintf("%d", ttlMinutes))
	payload := map[string]any{
		"to":       phone,
		"code":     code,
		"message":  message,
		"purpose":  purpose,
		"provider": gateway.ProviderName,
		"senderId": gateway.SenderID,
	}
	return postGatewayPayload(ctx, gateway.EndpointURL, gateway.AuthScheme, gateway.HeaderName, gateway.AuthToken, payload)
}

func sendEmailViaGateway(ctx context.Context, gateway *store.EmailGatewayConfig, email string, code string, purpose string, ttlMinutes int) error {
	if ttlMinutes <= 0 {
		ttlMinutes = 5
	}
	subject := gateway.SubjectTemplate
	if strings.TrimSpace(subject) == "" {
		subject = "【Infinite-AI】您的验证码是 {{code}}"
	}
	content := gateway.ContentTemplate
	if strings.TrimSpace(content) == "" {
		content = "您的验证码是 {{code}}，{{minutes}} 分钟内有效。"
	}
	subject = strings.ReplaceAll(subject, "{{code}}", code)
	subject = strings.ReplaceAll(subject, "{{minutes}}", fmt.Sprintf("%d", ttlMinutes))
	content = strings.ReplaceAll(content, "{{code}}", code)
	content = strings.ReplaceAll(content, "{{minutes}}", fmt.Sprintf("%d", ttlMinutes))
	payload := map[string]any{
		"to":          email,
		"code":        code,
		"purpose":     purpose,
		"provider":    gateway.ProviderName,
		"fromAddress": gateway.FromAddress,
		"fromName":    gateway.FromName,
		"subject":     subject,
		"text":        content,
		"html":        strings.ReplaceAll(content, "\n", "<br>"),
	}
	return postGatewayPayload(ctx, gateway.EndpointURL, gateway.AuthScheme, gateway.HeaderName, gateway.AuthToken, payload)
}

func postGatewayPayload(ctx context.Context, endpointURL string, authScheme string, headerName string, authToken string, payload map[string]any) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	switch strings.ToLower(strings.TrimSpace(authScheme)) {
	case "header":
		if strings.TrimSpace(headerName) == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, authToken)
	case "bearer":
		if strings.TrimSpace(authToken) != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
	default:
		if strings.TrimSpace(authToken) != "" {
			req.Header.Set("Authorization", authToken)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("gateway returned %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func firstNonEmptyGateway(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
