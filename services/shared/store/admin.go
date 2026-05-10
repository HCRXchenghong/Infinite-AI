package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Store) DashboardSummary(ctx context.Context) (map[string]any, error) {
	summary := map[string]any{}
	var totalUsers int
	var apiKeys int
	var activeSubs int
	var avgLatency int
	_ = s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE status <> 'deleted'`).Scan(&totalUsers)
	_ = s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE status = 'active'`).Scan(&apiKeys)
	_ = s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE status = 'active'`).Scan(&activeSubs)
	avgLatency = 124
	summary["stats"] = []map[string]any{
		{"label": "总注册用户", "value": totalUsers, "trend": "+0.0%", "isUp": true},
		{"label": "有效订阅", "value": activeSubs, "trend": "+0.0%", "isUp": true},
		{"label": "API Key 数量", "value": apiKeys, "trend": "+0.0%", "isUp": true},
		{"label": "系统平均延迟", "value": fmt.Sprintf("%dms", avgLatency), "trend": "-0ms", "isUp": true},
	}
	return summary, nil
}

func (s *Store) ListUsersForAdmin(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT u.id::text, u.display_name, u.email, u.status, u.created_at,
			COALESCE(sub.plan_code, 'free') AS plan_code,
			COALESCE((SELECT MAX(balance_after) FROM quota_ledgers q WHERE q.user_id = u.id), 0) AS balance
		FROM users u
		LEFT JOIN LATERAL (
			SELECT plan_code
			FROM subscriptions
			WHERE user_id = u.id
			ORDER BY created_at DESC
			LIMIT 1
		) sub ON TRUE
		WHERE u.status <> 'deleted'
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, email, status, planCode string
		var createdAt time.Time
		var balance float64
		if err := rows.Scan(&id, &name, &email, &status, &createdAt, &planCode, &balance); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id":         id,
			"name":       name,
			"email":      email,
			"status":     statusLabel(status),
			"statusCode": status,
			"plan":       planBadge(planCode),
			"tokens":     fmt.Sprintf("%.1f", balance),
			"joined":     createdAt.Format("2006-01-02"),
			"rawPlan":    planCode,
		})
	}
	return out, rows.Err()
}

func (s *Store) ListAffiliateInvitesForAdmin(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.DB.Query(ctx, `
		SELECT
			i.code,
			i.created_at,
			i.consumed_at,
			COALESCE(a.display_name, a.email, '') AS created_by,
			COALESCE(u.display_name, u.email, '') AS consumed_by
		FROM affiliate_invites i
		LEFT JOIN admin_users a ON a.id = i.created_by_admin_id
		LEFT JOIN users u ON u.id = i.consumed_by_user_id
		ORDER BY i.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		var code string
		var createdAt time.Time
		var consumedAt *time.Time
		var createdBy string
		var consumedBy string
		if err := rows.Scan(&code, &createdAt, &consumedAt, &createdBy, &consumedBy); err != nil {
			return nil, err
		}
		status := "pending"
		consumedAtText := ""
		if consumedAt != nil {
			status = "consumed"
			consumedAtText = consumedAt.Local().Format("2006-01-02 15:04")
		}
		out = append(out, map[string]any{
			"code":       code,
			"status":     status,
			"createdAt":  createdAt.Local().Format("2006-01-02 15:04"),
			"consumedAt": consumedAtText,
			"createdBy":  createdBy,
			"consumedBy": consumedBy,
		})
	}
	return out, rows.Err()
}

func (s *Store) RevokeAffiliateInviteForAdmin(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `DELETE FROM affiliate_invites WHERE code = $1`, code)
	return err
}

func (s *Store) UpdateUserAdminConfig(ctx context.Context, userID string, planCode string, expiry string, status string) error {
	if planCode != "" {
		var endsAt *time.Time
		if expiry != "" {
			if parsed, err := time.Parse("2006-01-02", expiry); err == nil {
				endsAt = &parsed
			}
		} else if currentSub, err := s.GetSubscriptionByUser(ctx, userID); err == nil {
			endsAt = currentSub.EndsAt
		}
		if err := s.UpsertSubscription(ctx, userID, planCode, endsAt, "admin", "manual"); err != nil {
			return err
		}
	}
	if status != "" {
		_, err := s.DB.Exec(ctx, `UPDATE users SET status = $2, updated_at = NOW() WHERE id = $1`, userID, status)
		if err != nil {
			return err
		}
		if status != "active" {
			if _, err := s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) DeleteUserAdmin(ctx context.Context, userID string) error {
	return s.DeleteUserAccount(ctx, userID)
}

func (s *Store) MemberLogs(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT u.display_name, u.email, sub.plan_code, sub.status, sub.created_at
		FROM subscriptions sub
		JOIN users u ON u.id = sub.user_id
		ORDER BY sub.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var name, email, planCode, status string
		var createdAt time.Time
		if err := rows.Scan(&name, &email, &planCode, &status, &createdAt); err != nil {
			return nil, err
		}
		action := "升级到 " + planBadge(planCode)
		if status == "expired" {
			action = "取消了 " + planBadge(planCode)
		}
		out = append(out, map[string]any{
			"user":   name,
			"email":  email,
			"action": action,
			"amount": "-",
			"date":   createdAt.Format("2006-01-02 15:04"),
		})
	}
	return out, rows.Err()
}

func (s *Store) FinanceSnapshot(ctx context.Context) (map[string]any, error) {
	var today float64
	var month float64
	var pending float64
	_ = s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) / 100.0, 0)
		FROM payment_orders
		WHERE status = 'succeeded' AND created_at::date = CURRENT_DATE
	`).Scan(&today)
	_ = s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) / 100.0, 0)
		FROM payment_orders
		WHERE status = 'succeeded' AND date_trunc('month', created_at) = date_trunc('month', NOW())
	`).Scan(&month)
	_ = s.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) / 100.0, 0)
		FROM payment_orders
		WHERE status IN ('pending', 'processing')
	`).Scan(&pending)
	ifpay, _ := s.GetIFPayConfig(ctx)
	rows, err := s.DB.Query(ctx, `
		SELECT o.id::text, COALESCE(u.display_name, ''), COALESCE(u.email, ''), o.order_type, o.amount_cents, o.status, o.created_at
		FROM payment_orders o
		LEFT JOIN users u ON u.id = o.user_id
		ORDER BY o.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	transactions := make([]map[string]any, 0, 50)
	for rows.Next() {
		var id, userName, email, orderType, status string
		var amountCents int
		var createdAt time.Time
		if err := rows.Scan(&id, &userName, &email, &orderType, &amountCents, &status, &createdAt); err != nil {
			return nil, err
		}
		transactions = append(transactions, map[string]any{
			"id":        id,
			"orderId":   id,
			"userName":  userName,
			"account":   strings.TrimSpace(userName),
			"email":     email,
			"type":      orderType,
			"amount":    float64(amountCents) / 100.0,
			"status":    status,
			"createdAt": createdAt.Format("2006-01-02 15:04:05"),
		})
	}
	return map[string]any{
		"todayRevenue":   today,
		"monthRevenue":   month,
		"pendingAmount":  pending,
		"refundAmount":   0,
		"ifpayConfig":    ifpay,
		"transactions":   transactions,
		"webhookHintURL": fmt.Sprintf("%s/webhooks/ifpay", s.Config.APIBaseURL),
	}, nil
}

func (s *Store) UpdateIFPayConfig(ctx context.Context, payload map[string]any) error {
	return s.putEncryptedJSONSetting(ctx, "ifpay_config", payload)
}

func (s *Store) GetIFPayConfig(ctx context.Context) (map[string]any, error) {
	return s.getEncryptedJSONSetting(ctx, "ifpay_config")
}

func (s *Store) GlobalSettings(ctx context.Context) (map[string]any, error) {
	registerEnabled, err := s.RegisterEnabled(ctx)
	if err != nil {
		return nil, err
	}
	oauth, err := s.GetOAuthProviders(ctx, true)
	if err != nil {
		return nil, err
	}
	authSecurity, err := s.GetAuthSecuritySettings(ctx)
	if err != nil {
		return nil, err
	}
	emailGateway, err := s.GetEmailGatewayConfig(ctx)
	if err != nil {
		return nil, err
	}
	smsGateway, err := s.GetSMSGatewayConfig(ctx)
	if err != nil {
		return nil, err
	}
	modelMembershipLimits, err := s.GetModelMembershipLimits(ctx)
	if err != nil {
		return nil, err
	}
	modelContextLimits, err := s.GetModelContextLimits(ctx)
	if err != nil {
		return nil, err
	}
	infiniteCodeQuotaConfig, err := s.GetInfiniteCodeQuotaConfig(ctx)
	if err != nil {
		return nil, err
	}
	shareCollaborationConfig, err := s.GetShareCollaborationConfig(ctx)
	if err != nil {
		return nil, err
	}
	searchProvider, err := s.GetSearchProviderConfig(ctx)
	if err != nil {
		return nil, err
	}
	authSecurity.SMSGatewayConfigured = smsGateway.Enabled && smsGateway.EndpointURL != ""
	authSecurity.EmailGatewayConfigured = emailGateway.Enabled && emailGateway.EndpointURL != ""
	return map[string]any{
		"registerEnabled":         registerEnabled,
		"oauthProviders":          oauth,
		"authSecurity":            authSecurity,
		"emailGateway":            emailGateway,
		"smsGateway":              smsGateway,
		"modelMembershipLimits":   modelMembershipLimits,
		"modelContextLimits":      modelContextLimits,
		"infiniteCodeQuotaConfig": infiniteCodeQuotaConfig,
		"shareCollaborationConfig": shareCollaborationConfig,
		"searchProvider":          searchProvider,
	}, nil
}

func (s *Store) UpdateRegisterEnabled(ctx context.Context, enabled bool) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('register_enabled', $1::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, marshalJSON(enabled))
	return err
}

func statusLabel(status string) string {
	switch status {
	case "deleted":
		return "已删除"
	case "banned":
		return "已封禁"
	case "suspended":
		return "已停用"
	default:
		return "正常"
	}
}

func planBadge(code string) string {
	switch code {
	case "go":
		return "Go 版"
	case "plus":
		return "Plus 版"
	case "pro_basic":
		return "Pro 基础版"
	case "pro_max":
		return "Pro 满血版"
	default:
		return "免费版"
	}
}

func (s *Store) GetAuthSecuritySettings(ctx context.Context) (*AuthSecuritySettings, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = 'auth_security_config'`).Scan(&raw)
	if err != nil {
		return nil, scanNotFound(err)
	}
	settings := &AuthSecuritySettings{
		CaptchaRequiredOnRegister:           true,
		PhoneVerificationRequiredOnRegister: true,
		PhoneLoginEnabled:                   true,
		SMSCodeTTLSeconds:                   300,
		VerificationTestMode:                false,
	}
	_ = json.Unmarshal(raw, settings)
	if settings.SMSCodeTTLSeconds <= 0 {
		settings.SMSCodeTTLSeconds = 300
	}
	return settings, nil
}

func (s *Store) UpdateAuthSecuritySettings(ctx context.Context, settings AuthSecuritySettings) error {
	if settings.SMSCodeTTLSeconds <= 0 {
		settings.SMSCodeTTLSeconds = 300
	}
	settings.SMSGatewayConfigured = false
	settings.EmailGatewayConfigured = false
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('auth_security_config', $1::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, marshalJSON(settings))
	return err
}

func (s *Store) UpdateEmailGatewayConfig(ctx context.Context, payload EmailGatewayConfig) error {
	return s.putEncryptedJSONSetting(ctx, "email_gateway_config", map[string]any{
		"enabled":         payload.Enabled,
		"providerName":    payload.ProviderName,
		"endpointUrl":     payload.EndpointURL,
		"authScheme":      payload.AuthScheme,
		"headerName":      payload.HeaderName,
		"authToken":       payload.AuthToken,
		"fromAddress":     payload.FromAddress,
		"fromName":        payload.FromName,
		"subjectTemplate": payload.SubjectTemplate,
		"contentTemplate": payload.ContentTemplate,
	})
}

func (s *Store) GetEmailGatewayConfig(ctx context.Context) (*EmailGatewayConfig, error) {
	payload, err := s.getEncryptedJSONSetting(ctx, "email_gateway_config")
	if err != nil {
		return nil, err
	}
	config := &EmailGatewayConfig{
		AuthScheme:      "bearer",
		HeaderName:      "Authorization",
		FromName:        "Infinite-AI",
		SubjectTemplate: "【Infinite-AI】您的验证码是 {{code}}",
		ContentTemplate: "您的验证码是 {{code}}，{{minutes}} 分钟内有效。",
	}
	if value, ok := payload["enabled"].(bool); ok {
		config.Enabled = value
	}
	if value, ok := payload["providerName"].(string); ok {
		config.ProviderName = value
	}
	if value, ok := payload["endpointUrl"].(string); ok {
		config.EndpointURL = value
	}
	if value, ok := payload["authScheme"].(string); ok && value != "" {
		config.AuthScheme = value
	}
	if value, ok := payload["headerName"].(string); ok && value != "" {
		config.HeaderName = value
	}
	if value, ok := payload["authToken"].(string); ok {
		config.AuthToken = value
	}
	if value, ok := payload["fromAddress"].(string); ok {
		config.FromAddress = value
	}
	if value, ok := payload["fromName"].(string); ok && value != "" {
		config.FromName = value
	}
	if value, ok := payload["subjectTemplate"].(string); ok && value != "" {
		config.SubjectTemplate = value
	}
	if value, ok := payload["contentTemplate"].(string); ok && value != "" {
		config.ContentTemplate = value
	}
	return config, nil
}

func (s *Store) UpdateSMSGatewayConfig(ctx context.Context, payload SMSGatewayConfig) error {
	return s.putEncryptedJSONSetting(ctx, "sms_gateway_config", map[string]any{
		"enabled":         payload.Enabled,
		"providerName":    payload.ProviderName,
		"endpointUrl":     payload.EndpointURL,
		"authScheme":      payload.AuthScheme,
		"headerName":      payload.HeaderName,
		"authToken":       payload.AuthToken,
		"senderId":        payload.SenderID,
		"messageTemplate": payload.MessageTemplate,
	})
}

func (s *Store) GetSMSGatewayConfig(ctx context.Context) (*SMSGatewayConfig, error) {
	payload, err := s.getEncryptedJSONSetting(ctx, "sms_gateway_config")
	if err != nil {
		return nil, err
	}
	config := &SMSGatewayConfig{
		AuthScheme:      "bearer",
		HeaderName:      "Authorization",
		MessageTemplate: "【Infinite-AI】您的验证码是 {{code}}，{{minutes}} 分钟内有效。",
	}
	if value, ok := payload["enabled"].(bool); ok {
		config.Enabled = value
	}
	if value, ok := payload["providerName"].(string); ok {
		config.ProviderName = value
	}
	if value, ok := payload["endpointUrl"].(string); ok {
		config.EndpointURL = value
	}
	if value, ok := payload["authScheme"].(string); ok && value != "" {
		config.AuthScheme = value
	}
	if value, ok := payload["headerName"].(string); ok && value != "" {
		config.HeaderName = value
	}
	if value, ok := payload["authToken"].(string); ok {
		config.AuthToken = value
	}
	if value, ok := payload["senderId"].(string); ok {
		config.SenderID = value
	}
	if value, ok := payload["messageTemplate"].(string); ok && value != "" {
		config.MessageTemplate = value
	}
	return config, nil
}

func (s *Store) putEncryptedJSONSetting(ctx context.Context, key string, payload map[string]any) error {
	plain, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ciphertext, err := s.Encrypt(string(plain))
	if err != nil {
		return err
	}
	wrapper := map[string]any{
		"encrypted":  true,
		"ciphertext": ciphertext,
	}
	_, err = s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, marshalJSON(wrapper))
	return err
}

func (s *Store) putEncryptedJSONSettingIfMissing(ctx context.Context, key string, payload map[string]any) error {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM system_settings WHERE key = $1)`, key).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.putEncryptedJSONSetting(ctx, key, payload)
}

func (s *Store) getEncryptedJSONSetting(ctx context.Context, key string) (map[string]any, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return nil, scanNotFound(err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	if encrypted, _ := wrapper["encrypted"].(bool); encrypted {
		ciphertext, _ := wrapper["ciphertext"].(string)
		plain, err := s.Decrypt(ciphertext)
		if err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(plain), &payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	return wrapper, nil
}
