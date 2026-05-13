package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrAdminSetupUnavailable = errors.New("admin setup is unavailable")

type CreateUserInput struct {
	Email           string
	PasswordHash    string
	DisplayName     string
	Phone           string
	PhoneVerifiedAt *time.Time
}

func (s *Store) CreateUser(ctx context.Context, input CreateUserInput) (*User, error) {
	var user User
	err := s.DB.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name, phone, phone_verified_at, status)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, 'active')
		RETURNING id::text, email, COALESCE(phone, ''), display_name, status, created_at
	`, input.Email, input.PasswordHash, input.DisplayName, input.Phone, input.PhoneVerifiedAt).Scan(&user.ID, &user.Email, &user.Phone, &user.DisplayName, &user.Status, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO user_settings (user_id, theme, language, deep_search_default, chat_history_enabled, memory_enabled)
		VALUES ($1, 'system', 'auto', FALSE, TRUE, TRUE)
		ON CONFLICT (user_id) DO NOTHING
	`, user.ID)
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO subscriptions (user_id, plan_code, status, source, source_reference)
		VALUES ($1, 'free', 'active', 'system', 'seed')
		ON CONFLICT DO NOTHING
	`, user.ID)
	return &user, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, string, error) {
	var user User
	var passwordHash string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, email, COALESCE(phone, ''), display_name, status, password_hash, created_at
		FROM users
		WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.Phone, &user.DisplayName, &user.Status, &passwordHash, &user.CreatedAt)
	if err != nil {
		return nil, "", scanNotFound(err)
	}
	return &user, passwordHash, nil
}

func (s *Store) GetUserByPhone(ctx context.Context, phone string) (*User, string, error) {
	var user User
	var passwordHash string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, email, COALESCE(phone, ''), display_name, status, password_hash, created_at
		FROM users
		WHERE phone = $1
	`, phone).Scan(&user.ID, &user.Email, &user.Phone, &user.DisplayName, &user.Status, &passwordHash, &user.CreatedAt)
	if err != nil {
		return nil, "", scanNotFound(err)
	}
	return &user, passwordHash, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, email, COALESCE(phone, ''), display_name, status, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Email, &user.Phone, &user.DisplayName, &user.Status, &user.CreatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &user, nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID string, passwordHash string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET password_hash = $2, updated_at = NOW()
		WHERE id = $1
	`, userID, passwordHash)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID)
	return err
}

func (s *Store) CreateUserSession(ctx context.Context, userID string, csrfToken string, userAgent string, ip string, expiresAt time.Time) (*SessionView, error) {
	var session SessionView
	err := s.DB.QueryRow(ctx, `
		INSERT INTO user_sessions (user_id, csrf_token, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, user_id::text, csrf_token, expires_at
	`, userID, csrfToken, userAgent, ip, expiresAt).Scan(&session.SessionID, &session.UserID, &session.CSRFToken, &session.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) GetUserSession(ctx context.Context, sessionID string) (*SessionView, error) {
	var session SessionView
	err := s.DB.QueryRow(ctx, `
		SELECT us.id::text, u.id::text, u.email, u.display_name, us.csrf_token, us.expires_at
		FROM user_sessions us
		JOIN users u ON u.id = us.user_id
		WHERE us.id = $1 AND us.expires_at > NOW() AND u.status = 'active'
	`, sessionID).Scan(&session.SessionID, &session.UserID, &session.Email, &session.Name, &session.CSRFToken, &session.ExpiresAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	_, _ = s.DB.Exec(ctx, `UPDATE user_sessions SET last_seen_at = NOW() WHERE id = $1`, sessionID)
	return &session, nil
}

func (s *Store) DeleteUserSession(ctx context.Context, sessionID string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE id = $1`, sessionID)
	return err
}

func (s *Store) GetAdminByEmail(ctx context.Context, email string) (*AdminUser, string, string, error) {
	var admin AdminUser
	var passwordHash string
	var totpSecretEnc string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, email, display_name, role_slug, status, password_hash, totp_secret_enc, created_at
		FROM admin_users
		WHERE email = $1
	`, email).Scan(&admin.ID, &admin.Email, &admin.DisplayName, &admin.Role, &admin.Status, &passwordHash, &totpSecretEnc, &admin.CreatedAt)
	if err != nil {
		return nil, "", "", scanNotFound(err)
	}
	return &admin, passwordHash, totpSecretEnc, nil
}

func (s *Store) AdminSetupRequired(ctx context.Context) (bool, error) {
	var count int
	if err := s.DB.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *Store) CreateFirstAdmin(ctx context.Context, email string, passwordHash string, displayName string, roleSlug string, totpSecretEnc string) (*AdminUser, error) {
	var admin AdminUser
	err := s.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE admin_users IN ACCESS EXCLUSIVE MODE`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrAdminSetupUnavailable
		}
		return tx.QueryRow(ctx, `
			INSERT INTO admin_users (email, password_hash, display_name, role_slug, status, totp_secret_enc)
			VALUES ($1, $2, $3, $4, 'active', $5)
			RETURNING id::text, email, display_name, role_slug, status, created_at
		`, email, passwordHash, displayName, roleSlug, totpSecretEnc).Scan(
			&admin.ID,
			&admin.Email,
			&admin.DisplayName,
			&admin.Role,
			&admin.Status,
			&admin.CreatedAt,
		)
	})
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

func (s *Store) GetAdminByID(ctx context.Context, id string) (*AdminUser, error) {
	var admin AdminUser
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, email, display_name, role_slug, status, created_at
		FROM admin_users
		WHERE id = $1
	`, id).Scan(&admin.ID, &admin.Email, &admin.DisplayName, &admin.Role, &admin.Status, &admin.CreatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &admin, nil
}

func (s *Store) CreateAdminSession(ctx context.Context, adminID string, csrfToken string, userAgent string, ip string, expiresAt time.Time) (*SessionView, error) {
	var session SessionView
	err := s.DB.QueryRow(ctx, `
		INSERT INTO admin_sessions (admin_user_id, csrf_token, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, admin_user_id::text, csrf_token, expires_at
	`, adminID, csrfToken, userAgent, ip, expiresAt).Scan(&session.SessionID, &session.AdminID, &session.CSRFToken, &session.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) GetAdminSession(ctx context.Context, sessionID string) (*SessionView, error) {
	var session SessionView
	err := s.DB.QueryRow(ctx, `
		SELECT s.id::text, a.id::text, a.email, a.display_name, a.role_slug, s.csrf_token, s.expires_at
		FROM admin_sessions s
		JOIN admin_users a ON a.id = s.admin_user_id
		WHERE s.id = $1 AND s.expires_at > NOW() AND a.status = 'active'
	`, sessionID).Scan(&session.SessionID, &session.AdminID, &session.Email, &session.Name, &session.Role, &session.CSRFToken, &session.ExpiresAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	_, _ = s.DB.Exec(ctx, `UPDATE admin_sessions SET last_seen_at = NOW() WHERE id = $1`, sessionID)
	return &session, nil
}

func (s *Store) DeleteAdminSession(ctx context.Context, sessionID string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM admin_sessions WHERE id = $1`, sessionID)
	return err
}

func (s *Store) GetOAuthProviders(ctx context.Context, includeSecrets bool) ([]OAuthProvider, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id::text, slug, name, provider_kind, enabled, logo_url, auth_url, token_url, userinfo_url, scopes, client_id_enc, client_secret_enc, user_id_field, user_email_field, user_name_field, auth_params, token_params
		FROM oauth_provider_configs
		ORDER BY slug ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OAuthProvider
	for rows.Next() {
		var provider OAuthProvider
		var clientIDEnc string
		var clientSecretEnc string
		var authParamsRaw []byte
		var tokenParamsRaw []byte
		if err := rows.Scan(&provider.ID, &provider.Slug, &provider.Name, &provider.ProviderKind, &provider.Enabled, &provider.LogoURL, &provider.AuthURL, &provider.TokenURL, &provider.UserInfoURL, &provider.Scopes, &clientIDEnc, &clientSecretEnc, &provider.UserIDField, &provider.UserEmailField, &provider.UserNameField, &authParamsRaw, &tokenParamsRaw); err != nil {
			return nil, err
		}
		if includeSecrets {
			clientID, _ := s.Decrypt(clientIDEnc)
			clientSecret, _ := s.Decrypt(clientSecretEnc)
			provider.ClientID = clientID
			provider.ClientSecret = clientSecret
		} else {
			clientID, _ := s.Decrypt(clientIDEnc)
			clientSecret, _ := s.Decrypt(clientSecretEnc)
			provider.ClientID = secretPreview(clientID)
			provider.ClientSecret = secretPreview(clientSecret)
		}
		provider.AuthParams = unmarshalStringMap(authParamsRaw)
		provider.TokenParams = unmarshalStringMap(tokenParamsRaw)
		if provider.UserIDField == "" {
			provider.UserIDField = "id"
		}
		if provider.UserEmailField == "" {
			provider.UserEmailField = "email"
		}
		if provider.UserNameField == "" {
			provider.UserNameField = "name"
		}
		out = append(out, provider)
	}
	return out, rows.Err()
}

func (s *Store) GetOAuthProviderBySlug(ctx context.Context, slug string) (*OAuthProvider, error) {
	var provider OAuthProvider
	var clientIDEnc string
	var clientSecretEnc string
	var authParamsRaw []byte
	var tokenParamsRaw []byte
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, slug, name, provider_kind, enabled, logo_url, auth_url, token_url, userinfo_url, scopes, client_id_enc, client_secret_enc, user_id_field, user_email_field, user_name_field, auth_params, token_params
		FROM oauth_provider_configs
		WHERE slug = $1
	`, slug).Scan(&provider.ID, &provider.Slug, &provider.Name, &provider.ProviderKind, &provider.Enabled, &provider.LogoURL, &provider.AuthURL, &provider.TokenURL, &provider.UserInfoURL, &provider.Scopes, &clientIDEnc, &clientSecretEnc, &provider.UserIDField, &provider.UserEmailField, &provider.UserNameField, &authParamsRaw, &tokenParamsRaw)
	if err != nil {
		return nil, scanNotFound(err)
	}
	clientID, _ := s.Decrypt(clientIDEnc)
	clientSecret, _ := s.Decrypt(clientSecretEnc)
	provider.ClientID = clientID
	provider.ClientSecret = clientSecret
	provider.AuthParams = unmarshalStringMap(authParamsRaw)
	provider.TokenParams = unmarshalStringMap(tokenParamsRaw)
	if provider.UserIDField == "" {
		provider.UserIDField = "id"
	}
	if provider.UserEmailField == "" {
		provider.UserEmailField = "email"
	}
	if provider.UserNameField == "" {
		provider.UserNameField = "name"
	}
	return &provider, nil
}

func (s *Store) UpsertOAuthProvider(ctx context.Context, provider OAuthProvider) error {
	clientIDEnc, err := s.Encrypt(provider.ClientID)
	if err != nil {
		return err
	}
	clientSecretEnc, err := s.Encrypt(provider.ClientSecret)
	if err != nil {
		return err
	}
	if provider.ProviderKind == "" {
		provider.ProviderKind = "oauth2"
	}
	if provider.UserIDField == "" {
		provider.UserIDField = "id"
	}
	if provider.UserEmailField == "" {
		provider.UserEmailField = "email"
	}
	if provider.UserNameField == "" {
		provider.UserNameField = "name"
	}
	_, err = s.DB.Exec(ctx, `
		INSERT INTO oauth_provider_configs (
			slug, name, provider_kind, enabled, logo_url, auth_url, token_url, userinfo_url, scopes, client_id_enc, client_secret_enc, user_id_field, user_email_field, user_name_field, auth_params, token_params
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb)
		ON CONFLICT (slug) DO UPDATE SET
			name = EXCLUDED.name,
			provider_kind = EXCLUDED.provider_kind,
			enabled = EXCLUDED.enabled,
			logo_url = EXCLUDED.logo_url,
			auth_url = EXCLUDED.auth_url,
			token_url = EXCLUDED.token_url,
			userinfo_url = EXCLUDED.userinfo_url,
			scopes = EXCLUDED.scopes,
			client_id_enc = EXCLUDED.client_id_enc,
			client_secret_enc = EXCLUDED.client_secret_enc,
			user_id_field = EXCLUDED.user_id_field,
			user_email_field = EXCLUDED.user_email_field,
			user_name_field = EXCLUDED.user_name_field,
			auth_params = EXCLUDED.auth_params,
			token_params = EXCLUDED.token_params,
			updated_at = NOW()
	`, provider.Slug, provider.Name, provider.ProviderKind, provider.Enabled, provider.LogoURL, provider.AuthURL, provider.TokenURL, provider.UserInfoURL, provider.Scopes, clientIDEnc, clientSecretEnc, provider.UserIDField, provider.UserEmailField, provider.UserNameField, marshalJSON(provider.AuthParams), marshalJSON(provider.TokenParams))
	return err
}

func (s *Store) LinkOAuthIdentity(ctx context.Context, userID string, providerSlug string, providerUserID string, email string, profile map[string]any) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO oauth_identities (user_id, provider_slug, provider_user_id, email, profile)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (provider_slug, provider_user_id) DO UPDATE
		SET user_id = EXCLUDED.user_id,
			email = EXCLUDED.email,
			profile = EXCLUDED.profile
	`, userID, providerSlug, providerUserID, email, marshalJSON(profile))
	return err
}

func (s *Store) FindUserByOAuth(ctx context.Context, providerSlug string, providerUserID string) (*User, error) {
	var user User
	err := s.DB.QueryRow(ctx, `
		SELECT u.id::text, u.email, COALESCE(u.phone, ''), u.display_name, u.status, u.created_at
		FROM oauth_identities oi
		JOIN users u ON u.id = oi.user_id
		WHERE oi.provider_slug = $1 AND oi.provider_user_id = $2
	`, providerSlug, providerUserID).Scan(&user.ID, &user.Email, &user.Phone, &user.DisplayName, &user.Status, &user.CreatedAt)
	if err != nil {
		return nil, scanNotFound(err)
	}
	return &user, nil
}

func unmarshalStringMap(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var out map[string]string
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return map[string]string{}
	}
	return out
}

func (s *Store) CreateAffiliateInvite(ctx context.Context, createdByAdminID string, sourceUserID string, code string, metadata map[string]any) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO affiliate_invites (code, created_by_admin_id, source_user_id, metadata)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4::jsonb)
	`, code, nullString(createdByAdminID), nullString(sourceUserID), marshalJSON(metadata))
	return err
}

func (s *Store) ConsumeAffiliateInvite(ctx context.Context, code string, userID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE affiliate_invites
		SET consumed_by_user_id = $2, consumed_at = NOW()
		WHERE code = $1 AND consumed_by_user_id IS NULL
	`, code, userID)
	return err
}

func nullString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Store) EnsureUserSettings(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO user_settings (user_id, theme, language, deep_search_default, selected_model_slug, chat_history_enabled, memory_enabled)
		VALUES ($1, 'system', 'auto', FALSE, '', TRUE, TRUE)
		ON CONFLICT (user_id) DO NOTHING
	`, userID)
	return err
}

func uuidString() string {
	return uuid.NewString()
}
