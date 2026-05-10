package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) EnsureSeedData(ctx context.Context) error {
	if err := s.seedRoles(ctx); err != nil {
		return err
	}
	if err := s.seedPlans(ctx); err != nil {
		return err
	}
	if err := s.seedSystemSettings(ctx); err != nil {
		return err
	}
	if err := s.seedOAuthProviders(ctx); err != nil {
		return err
	}
	if err := s.seedAuthSecuritySettings(ctx); err != nil {
		return err
	}
	if err := s.SeedInfiniteCodeQuotaConfig(ctx); err != nil {
		return err
	}
	if err := s.SeedShareCollaborationConfig(ctx); err != nil {
		return err
	}
	return s.seedDownloads(ctx)
}

func (s *Store) seedRoles(ctx context.Context) error {
	roles := map[string]string{
		"super_admin":   "Full access",
		"ops_admin":     "Ops and model administration",
		"finance_admin": "Finance, membership, and IF-Pay operations",
		"support_admin": "User support and moderation",
	}
	for slug, description := range roles {
		_, err := s.DB.Exec(ctx, `
			INSERT INTO admin_roles (slug, description)
			VALUES ($1, $2)
			ON CONFLICT (slug) DO UPDATE SET description = EXCLUDED.description
		`, slug, description)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedPlans(ctx context.Context) error {
	plans := []Plan{
		{Code: "free", Name: "免费版", Tier: "free", PriceCents: 0, Interval: "month", Description: "用于日常的基础任务", Features: []string{"访问标准模型", "基础响应速度"}},
		{Code: "go", Name: "Go 版", Tier: "go", PriceCents: 10000, Interval: "month", Description: "更高频次的日常访问", Features: []string{"高峰期优先访问", "基础文件与图片分析"}},
		{Code: "plus", Name: "Plus 版", Tier: "plus", PriceCents: 17000, Interval: "month", Description: "强大的研究与工作助手", Features: []string{"访问 Pro 模型", "深度搜索", "高级图片生成"}},
		{Code: "pro_basic", Name: "Pro 版 (基础档)", Tier: "pro", PriceCents: 24000, Interval: "month", Description: "高额度算力", Features: []string{"顶级推理模型", "高额度对话限额"}},
		{Code: "pro_max", Name: "Pro 版 (满血档)", Tier: "pro", PriceCents: 143000, Interval: "month", Description: "为高级用户提供顶配算力", Features: []string{"顶级推理模型", "超高对话限额"}},
	}
	for _, plan := range plans {
		_, err := s.DB.Exec(ctx, `
			INSERT INTO plans (code, name, tier, price_cents, interval, description, features, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, TRUE)
			ON CONFLICT (code) DO UPDATE SET
				name = EXCLUDED.name,
				tier = EXCLUDED.tier,
				price_cents = EXCLUDED.price_cents,
				description = EXCLUDED.description,
				features = EXCLUDED.features,
				active = TRUE
		`, plan.Code, plan.Name, plan.Tier, plan.PriceCents, plan.Interval, plan.Description, marshalJSON(plan.Features))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedSystemSettings(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('register_enabled', $1::jsonb, NOW())
		ON CONFLICT (key) DO NOTHING
	`, marshalJSON(true))
	if err != nil {
		return err
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM system_settings WHERE key = 'ifpay_config')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.putEncryptedJSONSetting(ctx, "ifpay_config", map[string]any{
		"merchantId":     "",
		"secretKey":      "",
		"webhookURL":     fmt.Sprintf("%s/webhooks/ifpay", s.Config.APIBaseURL),
		"enabledMethods": []string{"wechat", "alipay", "usdt"},
	})
}

func (s *Store) seedAuthSecuritySettings(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ('auth_security_config', $1::jsonb, NOW())
		ON CONFLICT (key) DO NOTHING
	`, marshalJSON(map[string]any{
		"captchaRequiredOnRegister":           true,
		"phoneVerificationRequiredOnRegister": true,
		"phoneLoginEnabled":                   true,
		"smsCodeTTLSeconds":                   300,
		"verificationTestMode":                false,
	}))
	if err != nil {
		return err
	}
	if err := s.putEncryptedJSONSettingIfMissing(ctx, "email_gateway_config", map[string]any{
		"enabled":         false,
		"providerName":    "",
		"endpointUrl":     "",
		"authScheme":      "bearer",
		"headerName":      "Authorization",
		"authToken":       "",
		"fromAddress":     "",
		"fromName":        "Infinite-AI",
		"subjectTemplate": "【Infinite-AI】您的验证码是 {{code}}",
		"contentTemplate": "您的验证码是 {{code}}，{{minutes}} 分钟内有效。",
	}); err != nil {
		return err
	}
	return s.putEncryptedJSONSettingIfMissing(ctx, "sms_gateway_config", map[string]any{
		"enabled":         false,
		"providerName":    "",
		"endpointUrl":     "",
		"authScheme":      "bearer",
		"headerName":      "Authorization",
		"authToken":       "",
		"senderId":        "",
		"messageTemplate": "【Infinite-AI】您的验证码是 {{code}}，{{minutes}} 分钟内有效。",
	})
}

func (s *Store) seedOAuthProviders(ctx context.Context) error {
	type preset struct {
		Slug     string
		Name     string
		AuthURL  string
		TokenURL string
		UserInfo string
		Scopes   string
	}
	presets := []preset{
		{
			Slug:     "github",
			Name:     "GitHub OAuth",
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
			UserInfo: "https://api.github.com/user",
			Scopes:   "read:user user:email",
		},
		{
			Slug:     "google",
			Name:     "Google OAuth",
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			UserInfo: "https://openidconnect.googleapis.com/v1/userinfo",
			Scopes:   "openid email profile",
		},
	}
	for _, provider := range presets {
		_, err := s.DB.Exec(ctx, `
			INSERT INTO oauth_provider_configs (
				slug, name, provider_kind, enabled, logo_url, auth_url, token_url, userinfo_url, scopes, client_id_enc, client_secret_enc, user_id_field, user_email_field, user_name_field, auth_params, token_params
			)
			VALUES ($1, $2, 'oauth2', FALSE, '', $3, $4, $5, $6, '', '', 'id', 'email', 'name', '{}'::jsonb, '{}'::jsonb)
			ON CONFLICT (slug) DO UPDATE SET
				name = EXCLUDED.name,
				auth_url = EXCLUDED.auth_url,
				token_url = EXCLUDED.token_url,
				userinfo_url = EXCLUDED.userinfo_url,
				scopes = EXCLUDED.scopes,
				updated_at = NOW()
		`, provider.Slug, provider.Name, provider.AuthURL, provider.TokenURL, provider.UserInfo, provider.Scopes)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedDownloads(ctx context.Context) error {
	releases := []DownloadRelease{
		{
			Platform:    "desktop",
			Channel:     "stable",
			Version:     "unreleased",
			Title:       "桌面端 (Mac / Windows)",
			Description: "支持快捷键唤醒与全局截图对话",
			DownloadURL: "",
			Status:      "unavailable",
		},
		{
			Platform:    "mobile",
			Channel:     "stable",
			Version:     "unreleased",
			Title:       "移动端 (iOS / Android)",
			Description: "支持语音实时通话与摄像头分析",
			DownloadURL: "",
			Status:      "unavailable",
		},
	}
	for _, release := range releases {
		var existingID string
		err := s.DB.QueryRow(ctx, `
			SELECT id::text
			FROM download_releases
			WHERE platform = $1 AND channel = $2
			ORDER BY created_at ASC
			LIMIT 1
		`, release.Platform, release.Channel).Scan(&existingID)
		if err != nil {
			if err == pgx.ErrNoRows {
				_, err = s.DB.Exec(ctx, `
					INSERT INTO download_releases (platform, channel, version, title, description, download_url, status)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
				`, release.Platform, release.Channel, release.Version, release.Title, release.Description, release.DownloadURL, release.Status)
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		if _, err := s.DB.Exec(ctx, `
			UPDATE download_releases
			SET version = $2,
				title = $3,
				description = $4,
				download_url = $5,
				status = $6
			WHERE id = $1::uuid
		`, existingID, release.Version, release.Title, release.Description, release.DownloadURL, release.Status); err != nil {
			return err
		}
		if _, err := s.DB.Exec(ctx, `
			DELETE FROM download_releases
			WHERE platform = $1 AND channel = $2 AND id <> $3::uuid
		`, release.Platform, release.Channel, existingID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RegisterEnabled(ctx context.Context) (bool, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = 'register_enabled'`).Scan(&raw)
	if err != nil {
		return true, scanNotFound(err)
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return true, err
	}
	return enabled, nil
}
