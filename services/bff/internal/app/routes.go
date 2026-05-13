package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type contextKey string

const (
	contextKeyPrincipal contextKey = "principal"
)

type principal struct {
	kind      string
	subjectID string
	email     string
	name      string
	role      string
	csrfToken string
}

type registerRequest struct {
	Identifier       string `json:"identifier"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	PhoneCode        string `json:"phoneCode"`
	VerificationCode string `json:"verificationCode"`
	CaptchaID        string `json:"captchaId"`
	CaptchaAnswer    string `json:"captchaAnswer"`
	Password         string `json:"password"`
	DisplayName      string `json:"displayName"`
	AffiliateCode    string `json:"affiliateCode"`
}

type loginRequest struct {
	Identifier    string `json:"identifier"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type passwordResetRequest struct {
	Identifier       string `json:"identifier"`
	VerificationCode string `json:"verificationCode"`
	CaptchaID        string `json:"captchaId"`
	CaptchaAnswer    string `json:"captchaAnswer"`
	Password         string `json:"password"`
}

type adminLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
}

type adminBootstrapStartRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type adminBootstrapCompleteRequest struct {
	SetupToken string `json:"setupToken"`
	TOTPCode   string `json:"totpCode"`
}

type pendingAdminBootstrap struct {
	Email        string    `json:"email"`
	PasswordHash string    `json:"passwordHash"`
	DisplayName  string    `json:"displayName"`
	RoleSlug     string    `json:"roleSlug"`
	TOTPSecret   string    `json:"totpSecret"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	requestLog := s.systemLogMiddleware("bff")
	r.With(requestLog).Get("/auth/session", s.handleSession)
	r.With(requestLog).Get("/auth/oauth/providers", s.handleOAuthProviders)
	r.With(requestLog).Get("/auth/captcha", s.handleCaptcha)
	r.With(requestLog).Get("/auth/device", s.handleInfiniteCodeAuthorize)
	r.With(requestLog, s.riskGuard("auth:device-code", riskRule{Window: 15 * time.Minute, LimitPerIP: 30, LimitPerFingerprint: 20, BlockDuration: 30 * time.Minute, Message: "授权请求过于频繁，请稍后再试。"})).Post("/auth/device/code", s.handleInfiniteCodeDeviceCode)
	r.With(requestLog, s.riskGuard("auth:device-authorize", riskRule{Window: 15 * time.Minute, LimitPerIP: 30, LimitPerFingerprint: 20, BlockDuration: 30 * time.Minute, Message: "授权请求过于频繁，请稍后再试。"})).Post("/auth/device", s.handleInfiniteCodeAuthorize)
	r.With(requestLog, s.riskGuard("auth:device-token", riskRule{Window: 15 * time.Minute, LimitPerIP: 240, LimitPerFingerprint: 180, BlockDuration: 15 * time.Minute, Message: "登录检查过于频繁，请稍后再试。"})).Post("/auth/device/token", s.handleInfiniteCodeDeviceToken)
	r.With(requestLog, s.riskGuard("auth:contact-code", riskRule{Window: 10 * time.Minute, LimitPerIP: 10, LimitPerFingerprint: 6, BlockDuration: 30 * time.Minute, Message: "验证码请求过于频繁，请稍后再试。"})).Post("/auth/contact/send-code", s.handleSendContactCode)
	r.With(requestLog, s.riskGuard("auth:phone-code", riskRule{Window: 10 * time.Minute, LimitPerIP: 10, LimitPerFingerprint: 6, BlockDuration: 30 * time.Minute, Message: "验证码请求过于频繁，请稍后再试。"})).Post("/auth/phone/send-code", s.handleSendPhoneCode)
	r.With(requestLog, s.riskGuard("auth:register", riskRule{Window: 30 * time.Minute, LimitPerIP: 12, LimitPerFingerprint: 8, BlockDuration: time.Hour, Message: "注册请求过于频繁，请稍后再试。"})).Post("/auth/register", s.handleRegister)
	r.With(requestLog, s.riskGuard("auth:login", riskRule{Window: 15 * time.Minute, LimitPerIP: 20, LimitPerFingerprint: 12, BlockDuration: 45 * time.Minute, Message: "登录尝试过于频繁，请稍后再试。"})).Post("/auth/login", s.handleLogin)
	r.With(requestLog, s.riskGuard("auth:password-forgot", riskRule{Window: 15 * time.Minute, LimitPerIP: 8, LimitPerFingerprint: 5, BlockDuration: time.Hour, Message: "重置密码请求过于频繁，请稍后再试。"})).Post("/auth/password/forgot", s.handlePasswordForgot)
	r.With(requestLog, s.riskGuard("auth:password-reset", riskRule{Window: 15 * time.Minute, LimitPerIP: 8, LimitPerFingerprint: 5, BlockDuration: time.Hour, Message: "重置密码请求过于频繁，请稍后再试。"})).Post("/auth/password/reset", s.handlePasswordReset)
	r.With(requestLog).Post("/auth/logout", s.handleLogout)
	r.With(requestLog).Post("/auth/admin/bootstrap/start", s.handleAdminBootstrapStart)
	r.With(requestLog).Post("/auth/admin/bootstrap/complete", s.handleAdminBootstrapComplete)
	r.With(requestLog, s.riskGuard("auth:admin-login", riskRule{Window: 20 * time.Minute, LimitPerIP: 15, LimitPerFingerprint: 10, BlockDuration: time.Hour, Message: "管理员登录尝试过于频繁，请稍后再试。"})).Post("/auth/admin/login", s.handleAdminLogin)
	r.With(requestLog).Post("/auth/admin/logout", s.handleAdminLogout)
	r.With(requestLog).Get("/auth/oauth/start/{slug}", s.handleOAuthStart)
	r.With(requestLog).Get("/auth/oauth/callback/{slug}", s.handleOAuthCallback)

	r.With(requestLog).Get("/api/user", s.handleInfiniteCodeAPIUser)
	r.With(requestLog).Get("/api/orgs", s.handleInfiniteCodeAPIOrgs)
	r.With(requestLog).Get("/api/config", s.handleInfiniteCodeAPIConfig)
	r.With(requestLog).Get("/api/desktop/billing", s.handleInfiniteCodeDesktopBilling)
	r.With(requestLog, s.riskGuard("desktop:error-report", riskRule{Window: time.Minute, LimitPerIP: 120, LimitPerFingerprint: 120, BlockDuration: 5 * time.Minute, Message: "错误上报过于频繁，请稍后再试。"})).Post("/api/desktop/errors", s.handleInfiniteCodeDesktopError)

	r.With(requestLog).Handle("/billing/plans", s.proxyPublic())
	r.With(requestLog).Handle("/downloads/releases", s.proxyPublic())
	r.With(requestLog).Handle("/chat/models", s.proxyPublic())
	r.With(requestLog).Handle("/redeem/codes/*", s.proxyPublic())
	r.With(requestLog).Handle("/webhooks/ifpay", s.proxyPublic())
	r.With(requestLog, s.riskGuard("developer:public-api", riskRule{Window: time.Minute, LimitPerIP: 600, BlockDuration: 5 * time.Minute, Message: "API 请求过于频繁，请稍后再试。"})).Handle("/v1/*", s.proxyPublic())

	r.Group(func(r chi.Router) {
		r.Use(s.optionalUserSession)
		r.Use(requestLog)
		r.Handle("/chat/shares/*", s.proxyAuthenticatedOrPublic())
		r.Handle("/chat/shares", s.proxyAuthenticatedOrPublic())
	})

	r.Group(func(r chi.Router) {
		r.Use(s.requireUserSession)
		r.Use(requestLog)
		r.Handle("/chat/*", s.proxyAuthenticated())
		r.Handle("/chat", s.proxyAuthenticated())
		r.Handle("/developer/*", s.proxyAuthenticated())
		r.Handle("/developer", s.proxyAuthenticated())
		r.Handle("/billing/subscription", s.proxyAuthenticated())
		r.Handle("/billing/orders/*", s.proxyAuthenticated())
		r.Handle("/billing/orders", s.proxyAuthenticated())
		r.Handle("/user/*", s.proxyAuthenticated())
		r.Handle("/user", s.proxyAuthenticated())
	})

	r.Group(func(r chi.Router) {
		r.Use(s.optionalUserSession)
		r.Use(requestLog)
		r.Handle("/redeem/claim", s.proxyAuthenticatedOrPublic())
	})

	r.Group(func(r chi.Router) {
		r.Use(s.requireAdminSession)
		r.Use(requestLog)
		r.Handle("/admin/*", s.proxyAuthenticated())
		r.Handle("/admin", s.proxyAuthenticated())
	})

	return r
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{
		"user":               nil,
		"admin":              nil,
		"csrfToken":          "",
		"userCsrfToken":      "",
		"adminCsrfToken":     "",
		"registerEnabled":    true,
		"adminSetupRequired": false,
		"oauthProviders":     []store.OAuthProvider{},
		"authSecurity": store.AuthSecuritySettings{
			CaptchaRequiredOnRegister:           true,
			PhoneVerificationRequiredOnRegister: true,
			PhoneLoginEnabled:                   true,
			SMSCodeTTLSeconds:                   300,
			SMSGatewayConfigured:                false,
		},
	}

	registerEnabled, _ := s.Store.RegisterEnabled(r.Context())
	response["registerEnabled"] = registerEnabled
	if adminSetupRequired, err := s.Store.AdminSetupRequired(r.Context()); err == nil {
		response["adminSetupRequired"] = adminSetupRequired
	}
	if authSecurity, err := s.Store.GetAuthSecuritySettings(r.Context()); err == nil {
		if smsGateway, gatewayErr := s.Store.GetSMSGatewayConfig(r.Context()); gatewayErr == nil {
			authSecurity.SMSGatewayConfigured = smsGateway.Enabled && smsGateway.EndpointURL != ""
		}
		if emailGateway, gatewayErr := s.Store.GetEmailGatewayConfig(r.Context()); gatewayErr == nil {
			authSecurity.EmailGatewayConfigured = emailGateway.Enabled && emailGateway.EndpointURL != ""
		}
		response["authSecurity"] = authSecurity
	}

	providers, _ := s.Store.GetOAuthProviders(r.Context(), false)
	enabled := make([]store.OAuthProvider, 0, len(providers))
	for _, provider := range providers {
		if provider.Enabled && provider.ClientID != "" {
			enabled = append(enabled, provider)
		}
	}
	response["oauthProviders"] = enabled

	if sessionCookie, err := r.Cookie(s.Config.SessionCookieName); err == nil {
		if session, err := s.Store.GetUserSession(r.Context(), sessionCookie.Value); err == nil {
			response["user"] = map[string]any{
				"id":          session.UserID,
				"email":       session.Email,
				"displayName": session.Name,
			}
			response["userCsrfToken"] = session.CSRFToken
			response["csrfToken"] = session.CSRFToken
		}
	}
	if adminCookie, err := r.Cookie(s.Config.AdminSessionCookieName); err == nil {
		if session, err := s.Store.GetAdminSession(r.Context(), adminCookie.Value); err == nil {
			response["admin"] = map[string]any{
				"id":          session.AdminID,
				"email":       session.Email,
				"displayName": session.Name,
				"role":        session.Role,
			}
			response["adminCsrfToken"] = session.CSRFToken
			if response["csrfToken"] == "" {
				response["csrfToken"] = session.CSRFToken
			}
		}
	}

	httpx.JSON(w, http.StatusOK, response)
}

func (s *Server) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.Store.GetOAuthProviders(r.Context(), false)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "provider_load_failed", err.Error())
		return
	}
	enabled := make([]store.OAuthProvider, 0, len(providers))
	for _, provider := range providers {
		if provider.Enabled && provider.ClientID != "" {
			enabled = append(enabled, provider)
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"providers": enabled})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	enabled, err := s.Store.RegisterEnabled(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", "系统配置加载失败，请稍后重试")
		return
	}
	if !enabled {
		httpx.Error(w, http.StatusForbidden, "register_disabled", "当前暂未开放注册")
		return
	}
	authSecurity, err := s.Store.GetAuthSecuritySettings(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", "系统配置加载失败，请稍后重试")
		return
	}
	if authSecurity.CaptchaRequiredOnRegister {
		if err := s.validateCaptcha(r.Context(), body.CaptchaID, body.CaptchaAnswer); err != nil {
			httpx.Error(w, http.StatusBadRequest, "captcha_invalid", err.Error())
			return
		}
	}
	identifier := strings.TrimSpace(body.Identifier)
	if identifier == "" {
		if strings.TrimSpace(body.Email) != "" {
			identifier = strings.TrimSpace(body.Email)
		} else {
			identifier = strings.TrimSpace(body.Phone)
		}
	}
	isPhoneIdentifier := looksLikePhone(identifier)
	phone := normalizePhone(body.Phone)
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if isPhoneIdentifier {
		phone = normalizePhone(identifier)
		if phone == "" {
			httpx.Error(w, http.StatusBadRequest, "phone_invalid", "请输入正确的手机号")
			return
		}
		if email == "" {
			email = buildPhonePlaceholderEmail(phone)
		}
	} else {
		email = strings.TrimSpace(strings.ToLower(identifier))
	}
	verificationCode := strings.TrimSpace(body.VerificationCode)
	if verificationCode == "" {
		verificationCode = strings.TrimSpace(body.PhoneCode)
	}
	var phoneVerifiedAt *time.Time
	if authSecurity.PhoneVerificationRequiredOnRegister {
		verificationTarget := email
		if isPhoneIdentifier {
			verificationTarget = phone
		}
		if err := s.verifyContactCode(r.Context(), verificationTarget, verificationCode, "register"); err != nil {
			httpx.Error(w, http.StatusBadRequest, "verification_code_invalid", err.Error())
			return
		}
		if isPhoneIdentifier {
			now := time.Now().UTC()
			phoneVerifiedAt = &now
		}
	}
	passwordHash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "password_hash_failed", "密码处理失败，请稍后重试")
		return
	}
	if email == "" || !strings.Contains(email, "@") {
		httpx.Error(w, http.StatusBadRequest, "email_invalid", "请输入正确的邮箱地址")
		return
	}
	displayName := strings.TrimSpace(body.DisplayName)
	if displayName == "" {
		if isPhoneIdentifier {
			displayName = "用户" + phone[max(0, len(phone)-4):]
		} else {
			displayName = strings.Split(email, "@")[0]
		}
	}
	user, err := s.Store.CreateUser(r.Context(), store.CreateUserInput{
		Email:           email,
		PasswordHash:    passwordHash,
		DisplayName:     displayName,
		Phone:           phone,
		PhoneVerifiedAt: phoneVerifiedAt,
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "register_failed", translateUserCreateError(err, isPhoneIdentifier))
		return
	}
	if body.AffiliateCode != "" {
		_ = s.Store.ConsumeAffiliateInvite(r.Context(), body.AffiliateCode, user.ID)
	}
	if err := s.issueUserSession(w, r, user.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session_create_failed", "登录状态创建失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"user": user,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	identifier := strings.TrimSpace(body.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(body.Email)
	}
	if identifier == "" {
		httpx.Error(w, http.StatusBadRequest, "identifier_required", "请输入邮箱或手机号")
		return
	}
	if err := s.validateCaptcha(r.Context(), body.CaptchaID, body.CaptchaAnswer); err != nil {
		httpx.Error(w, http.StatusBadRequest, "captcha_invalid", err.Error())
		return
	}
	var user *store.User
	var passwordHash string
	var err error
	if looksLikePhone(identifier) {
		authSecurity, settingsErr := s.Store.GetAuthSecuritySettings(r.Context())
		if settingsErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", "系统配置加载失败，请稍后重试")
			return
		}
		if !authSecurity.PhoneLoginEnabled {
			httpx.Error(w, http.StatusForbidden, "phone_login_disabled", "当前未开启手机号登录")
			return
		}
		user, passwordHash, err = s.Store.GetUserByPhone(r.Context(), normalizePhone(identifier))
	} else {
		user, passwordHash, err = s.Store.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(identifier)))
	}
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "邮箱或手机号 / 密码不正确")
		return
	}
	if err := auth.CheckPassword(passwordHash, body.Password); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "邮箱或手机号 / 密码不正确")
		return
	}
	if user.Status != "active" {
		httpx.Error(w, http.StatusForbidden, "user_inactive", "账号当前不可用，请联系管理员")
		return
	}
	if err := s.issueUserSession(w, r, user.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session_create_failed", "登录状态创建失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

func (s *Server) handlePasswordForgot(w http.ResponseWriter, r *http.Request) {
	var body sendContactCodeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	body.Purpose = "password_reset"
	s.sendContactCodeResponse(w, r, body.Identifier, body.Purpose, body.CaptchaID, body.CaptchaAnswer)
}

func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	var body passwordResetRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	identifier := strings.TrimSpace(body.Identifier)
	if identifier == "" {
		httpx.Error(w, http.StatusBadRequest, "identifier_required", "请输入邮箱或手机号")
		return
	}
	if len(body.Password) < 8 {
		httpx.Error(w, http.StatusBadRequest, "password_too_short", "密码至少需要 8 位")
		return
	}
	if err := s.validateCaptcha(r.Context(), body.CaptchaID, body.CaptchaAnswer); err != nil {
		httpx.Error(w, http.StatusBadRequest, "captcha_invalid", err.Error())
		return
	}
	if err := s.verifyContactCode(r.Context(), identifier, strings.TrimSpace(body.VerificationCode), "password_reset"); err != nil {
		httpx.Error(w, http.StatusBadRequest, "verification_code_invalid", err.Error())
		return
	}
	var user *store.User
	var err error
	if looksLikePhone(identifier) {
		user, _, err = s.Store.GetUserByPhone(r.Context(), normalizePhone(identifier))
	} else {
		user, _, err = s.Store.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(identifier)))
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "account_not_found", "账号不存在，请核对后重试")
		return
	}
	passwordHash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "password_hash_failed", "密码处理失败，请稍后重试")
		return
	}
	if err := s.Store.UpdateUserPassword(r.Context(), user.ID, passwordHash); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "password_reset_failed", "密码重置失败，请稍后重试")
		return
	}
	s.logSystemEvent(r, "info", "auth", "password_reset", "用户密码已重置", map[string]any{
		"identifier": identifier,
		"userId":     user.ID,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.Config.SessionCookieName); err == nil {
		_ = s.Store.DeleteUserSession(r.Context(), cookie.Value)
		s.clearCookie(w, s.Config.SessionCookieName)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body adminLoginRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	setupRequired, err := s.Store.AdminSetupRequired(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_status_failed", "管理员初始化状态读取失败，请稍后重试")
		return
	}
	if setupRequired {
		httpx.Error(w, http.StatusForbidden, "admin_setup_required", "请先初始化首个管理员账号")
		return
	}
	admin, passwordHash, totpEnc, err := s.Store.GetAdminByEmail(r.Context(), strings.TrimSpace(strings.ToLower(body.Email)))
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "邮箱、密码或 2FA 验证码不正确")
		return
	}
	if err := auth.CheckPassword(passwordHash, body.Password); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "邮箱、密码或 2FA 验证码不正确")
		return
	}
	totpSecret, _ := s.Store.Decrypt(totpEnc)
	if !auth.ValidateTOTP(totpSecret, body.TOTPCode) {
		httpx.Error(w, http.StatusUnauthorized, "invalid_totp", "两步验证码不正确")
		return
	}
	if err := s.issueAdminSession(w, r, admin.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session_create_failed", "登录状态创建失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"admin": admin,
	})
}

func (s *Server) handleAdminBootstrapStart(w http.ResponseWriter, r *http.Request) {
	setupRequired, err := s.Store.AdminSetupRequired(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_status_failed", "管理员初始化状态读取失败，请稍后重试")
		return
	}
	if !setupRequired {
		httpx.Error(w, http.StatusConflict, "admin_setup_unavailable", "首个管理员账号已经初始化")
		return
	}
	var body adminBootstrapStartRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	password := body.Password
	displayName := strings.TrimSpace(body.DisplayName)
	if email == "" || !strings.Contains(email, "@") {
		httpx.Error(w, http.StatusBadRequest, "admin_email_invalid", "请输入正确的管理员邮箱")
		return
	}
	if len(password) < 10 {
		httpx.Error(w, http.StatusBadRequest, "admin_password_too_short", "管理员密码至少需要 10 位")
		return
	}
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "password_hash_failed", "密码处理失败，请稍后重试")
		return
	}
	totpSecret, provisioningURL, qrCodeDataURL, err := auth.GenerateTOTPSetup(email, "Infinite-AI 管理后台")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_totp_setup_failed", "管理员两步验证初始化失败，请稍后重试")
		return
	}
	setupToken, err := randomURLToken(32)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_token_failed", "管理员初始化令牌生成失败，请稍后重试")
		return
	}
	state := pendingAdminBootstrap{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		RoleSlug:     "super_admin",
		TOTPSecret:   totpSecret,
		CreatedAt:    time.Now().UTC(),
	}
	rawState, _ := json.Marshal(state)
	if err := s.Redis.Set(r.Context(), adminBootstrapStateKey(setupToken), rawState, 15*time.Minute).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_store_failed", "管理员初始化信息保存失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"setupToken":       setupToken,
		"email":            email,
		"manualEntryKey":   totpSecret,
		"provisioningUrl":  provisioningURL,
		"qrCodeDataUrl":    qrCodeDataURL,
		"issuer":           "Infinite-AI 管理后台",
		"totpAppHint":      "Microsoft Authenticator",
		"expiresInSeconds": 900,
	})
}

func (s *Server) handleAdminBootstrapComplete(w http.ResponseWriter, r *http.Request) {
	setupRequired, err := s.Store.AdminSetupRequired(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_status_failed", "管理员初始化状态读取失败，请稍后重试")
		return
	}
	if !setupRequired {
		httpx.Error(w, http.StatusConflict, "admin_setup_unavailable", "首个管理员账号已经初始化")
		return
	}
	var body adminBootstrapCompleteRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	if body.SetupToken == "" || strings.TrimSpace(body.TOTPCode) == "" {
		httpx.Error(w, http.StatusBadRequest, "admin_setup_incomplete", "请输入设置令牌和两步验证码")
		return
	}
	rawState, err := s.Redis.Get(r.Context(), adminBootstrapStateKey(body.SetupToken)).Result()
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "admin_setup_expired", "管理员初始化会话已过期，请重新开始")
		return
	}
	var state pendingAdminBootstrap
	if err := json.Unmarshal([]byte(rawState), &state); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_setup_state_invalid", "管理员初始化状态异常，请重新开始")
		return
	}
	if !auth.ValidateTOTP(state.TOTPSecret, strings.TrimSpace(body.TOTPCode)) {
		httpx.Error(w, http.StatusUnauthorized, "invalid_totp", "两步验证码不正确")
		return
	}
	totpSecretEnc, err := s.Store.Encrypt(state.TOTPSecret)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "admin_totp_encrypt_failed", "管理员两步验证信息保存失败，请稍后重试")
		return
	}
	admin, err := s.Store.CreateFirstAdmin(r.Context(), state.Email, state.PasswordHash, state.DisplayName, state.RoleSlug, totpSecretEnc)
	if err != nil {
		if err == store.ErrAdminSetupUnavailable {
			httpx.Error(w, http.StatusConflict, "admin_setup_unavailable", "首个管理员账号已经初始化")
			return
		}
		httpx.Error(w, http.StatusBadRequest, "admin_create_failed", translateAdminCreateError(err))
		return
	}
	_, _ = s.Redis.Del(r.Context(), adminBootstrapStateKey(body.SetupToken)).Result()
	if err := s.issueAdminSession(w, r, admin.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session_create_failed", "登录状态创建失败，请稍后重试")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"admin": admin,
	})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.Config.AdminSessionCookieName); err == nil {
		_ = s.Store.DeleteAdminSession(r.Context(), cookie.Value)
		s.clearCookie(w, s.Config.AdminSessionCookieName)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	provider, err := s.Store.GetOAuthProviderBySlug(r.Context(), slug)
	if err != nil || !provider.Enabled || provider.ClientID == "" {
		httpx.Error(w, http.StatusNotFound, "provider_unavailable", "当前第三方登录方式暂不可用")
		return
	}
	state, err := randomURLToken(24)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "state_failed", "登录状态生成失败，请稍后重试")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "ia_oauth_state",
		Value:    slug + ":" + state,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.IsProd(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	redirectURI := fmt.Sprintf("%s/auth/oauth/callback/%s", requestBaseURL(r, s.Config.UserBaseURL), slug)
	values := url.Values{}
	values.Set("client_id", provider.ClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("scope", provider.Scopes)
	values.Set("state", state)
	for key, value := range provider.AuthParams {
		if strings.TrimSpace(key) != "" && value != "" {
			values.Set(key, value)
		}
	}
	if slug == "google" {
		if values.Get("access_type") == "" {
			values.Set("access_type", "offline")
		}
		if values.Get("prompt") == "" {
			values.Set("prompt", "consent")
		}
	}
	target := provider.AuthURL
	if strings.Contains(target, "?") {
		target += "&" + values.Encode()
	} else {
		target += "?" + values.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	provider, err := s.Store.GetOAuthProviderBySlug(r.Context(), slug)
	if err != nil || !provider.Enabled || provider.ClientID == "" || provider.ClientSecret == "" {
		httpx.Error(w, http.StatusNotFound, "provider_unavailable", "当前第三方登录方式暂不可用")
		return
	}
	stateCookie, err := r.Cookie("ia_oauth_state")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_state", "第三方登录状态已失效，请重新发起登录")
		return
	}
	expected := slug + ":" + r.URL.Query().Get("state")
	if stateCookie.Value != expected {
		httpx.Error(w, http.StatusBadRequest, "invalid_state", "第三方登录状态校验失败，请重新发起登录")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		httpx.Error(w, http.StatusBadRequest, "oauth_code_missing", "第三方登录授权码缺失，请重新尝试")
		return
	}
	profile, err := s.exchangeOAuthProfile(r.Context(), provider, code, requestBaseURL(r, s.Config.UserBaseURL))
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "oauth_exchange_failed", "第三方登录暂时不可用，请稍后重试")
		return
	}
	email, _ := profile["email"].(string)
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		httpx.Error(w, http.StatusBadGateway, "oauth_profile_incomplete", "第三方登录未返回邮箱信息")
		return
	}
	providerUserID := fmt.Sprint(profile["id"])
	user, err := s.Store.FindUserByOAuth(r.Context(), slug, providerUserID)
	if err != nil {
		user, _, err = s.Store.GetUserByEmail(r.Context(), email)
		if err != nil {
			enabled, settingsErr := s.Store.RegisterEnabled(r.Context())
			if settingsErr != nil {
				httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", "系统配置加载失败，请稍后重试")
				return
			}
			if !enabled {
				httpx.Error(w, http.StatusForbidden, "register_disabled", "当前暂未开放注册")
				return
			}
			passwordHash, _ := auth.HashPassword("oauth-user-temporary-password")
			displayName := email
			if name, ok := profile["name"].(string); ok && name != "" {
				displayName = name
			}
			user, err = s.Store.CreateUser(r.Context(), store.CreateUserInput{
				Email:        email,
				PasswordHash: passwordHash,
				DisplayName:  displayName,
			})
			if err != nil {
				httpx.Error(w, http.StatusBadGateway, "oauth_user_create_failed", translateUserCreateError(err, false))
				return
			}
		}
	}
	if err := s.Store.LinkOAuthIdentity(r.Context(), user.ID, slug, providerUserID, email, profile); err != nil {
		httpx.Error(w, http.StatusBadGateway, "oauth_link_failed", "第三方账号绑定失败，请稍后重试")
		return
	}
	if err := s.issueUserSession(w, r, user.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session_create_failed", "登录状态创建失败，请稍后重试")
		return
	}
	s.clearCookie(w, "ia_oauth_state")
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) exchangeOAuthProfile(ctx context.Context, provider *store.OAuthProvider, code string, redirectBaseURL string) (map[string]any, error) {
	redirectURI := fmt.Sprintf("%s/auth/oauth/callback/%s", redirectBaseURL, provider.Slug)
	form := url.Values{}
	form.Set("client_id", provider.ClientID)
	form.Set("client_secret", provider.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	for key, value := range provider.TokenParams {
		if strings.TrimSpace(key) != "" && value != "" {
			form.Set(key, value)
		}
	}
	if provider.Slug == "google" && form.Get("grant_type") == "" {
		form.Set("grant_type", "authorization_code")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("oauth token exchange failed: %s", string(body))
	}
	var tokenPayload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&tokenPayload); err != nil {
		return nil, err
	}
	accessToken, _ := tokenPayload["access_token"].(string)
	var profile map[string]any
	if provider.UserInfoURL == "" {
		profile = tokenPayload
	} else {
		profileReq, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoURL, nil)
		if err != nil {
			return nil, err
		}
		profileReq.Header.Set("Authorization", auth.BearerToken(accessToken))
		profileReq.Header.Set("Accept", "application/json")
		profileRes, err := http.DefaultClient.Do(profileReq)
		if err != nil {
			return nil, err
		}
		defer profileRes.Body.Close()
		if profileRes.StatusCode >= 300 {
			body, _ := io.ReadAll(profileRes.Body)
			return nil, fmt.Errorf("oauth userinfo failed: %s", string(body))
		}
		if err := json.NewDecoder(profileRes.Body).Decode(&profile); err != nil {
			return nil, err
		}
	}
	if provider.Slug == "github" && (profile["email"] == nil || profile["email"] == "") {
		emailReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
		if err == nil {
			emailReq.Header.Set("Authorization", auth.BearerToken(accessToken))
			emailReq.Header.Set("Accept", "application/json")
			emailRes, err := http.DefaultClient.Do(emailReq)
			if err == nil {
				defer emailRes.Body.Close()
				var emails []map[string]any
				if json.NewDecoder(emailRes.Body).Decode(&emails) == nil {
					for _, item := range emails {
						if primary, _ := item["primary"].(bool); primary {
							if email, _ := item["email"].(string); email != "" {
								profile["email"] = email
								break
							}
						}
					}
				}
			}
		}
	}
	if _, ok := profile["name"]; !ok {
		profile["name"] = profile["login"]
	}
	if idValue := profileField(profile, provider.UserIDField); idValue != nil {
		profile["id"] = idValue
	}
	if emailValue := stringField(profileField(profile, provider.UserEmailField)); emailValue != "" {
		profile["email"] = emailValue
	}
	if nameValue := stringField(profileField(profile, provider.UserNameField)); nameValue != "" {
		profile["name"] = nameValue
	}
	return profile, nil
}

func (s *Server) requireUserSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie(s.Config.SessionCookieName)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "user_session_missing", "登录状态已失效，请重新登录")
			return
		}
		session, err := s.Store.GetUserSession(r.Context(), sessionCookie.Value)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "user_session_invalid", "登录状态已失效，请重新登录")
			return
		}
		if requiresCSRF(r.Method) {
			if r.Header.Get("X-CSRF-Token") != session.CSRFToken {
				httpx.Error(w, http.StatusForbidden, "csrf_invalid", "请求已失效，请刷新后重试")
				return
			}
		}
		ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{
			kind:      "user",
			subjectID: session.UserID,
			email:     session.Email,
			name:      session.Name,
			csrfToken: session.CSRFToken,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) optionalUserSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie(s.Config.SessionCookieName)
		if err != nil {
			ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{kind: "public"})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		session, err := s.Store.GetUserSession(r.Context(), sessionCookie.Value)
		if err != nil {
			s.clearCookie(w, s.Config.SessionCookieName)
			ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{kind: "public"})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if requiresCSRF(r.Method) && r.Header.Get("X-CSRF-Token") != session.CSRFToken {
			httpx.Error(w, http.StatusForbidden, "csrf_invalid", "请求已失效，请刷新后重试")
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{
			kind:      "user",
			subjectID: session.UserID,
			email:     session.Email,
			name:      session.Name,
			csrfToken: session.CSRFToken,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie(s.Config.AdminSessionCookieName)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "admin_session_missing", "登录状态已失效，请重新登录")
			return
		}
		session, err := s.Store.GetAdminSession(r.Context(), sessionCookie.Value)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "admin_session_invalid", "登录状态已失效，请重新登录")
			return
		}
		if requiresCSRF(r.Method) {
			if r.Header.Get("X-CSRF-Token") != session.CSRFToken {
				httpx.Error(w, http.StatusForbidden, "csrf_invalid", "请求已失效，请刷新后重试")
				return
			}
		}
		ctx := context.WithValue(r.Context(), contextKeyPrincipal, principal{
			kind:      "admin",
			subjectID: session.AdminID,
			email:     session.Email,
			name:      session.Name,
			role:      session.Role,
			csrfToken: session.CSRFToken,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) proxyPublic() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.proxyRequest(w, r, principal{kind: "public"})
	})
}

func (s *Server) proxyAuthenticated() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Context().Value(contextKeyPrincipal)
		current, ok := value.(principal)
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "principal_missing", "登录状态已失效，请重新登录")
			return
		}
		s.proxyRequest(w, r, current)
	})
}

func (s *Server) proxyAuthenticatedOrPublic() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Context().Value(contextKeyPrincipal)
		current, ok := value.(principal)
		if !ok {
			current = principal{kind: "public"}
		}
		s.proxyRequest(w, r, current)
	})
}

func (s *Server) proxyRequest(w http.ResponseWriter, r *http.Request, current principal) {
	token, err := auth.IssueInternalJWT(s.Config.InternalJWTSecret, current.subjectID, current.kind, current.role, current.email, 5*time.Minute)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal_jwt_failed", "内部访问凭证生成失败，请稍后重试")
		return
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(s.CoreURL)
			req.Out.URL.Path = r.URL.Path
			req.Out.URL.RawQuery = r.URL.RawQuery
			if originalAuthorization := r.Header.Get("Authorization"); originalAuthorization != "" {
				req.Out.Header.Set("X-External-Authorization", originalAuthorization)
			}
			if originalAPIKey := strings.TrimSpace(r.Header.Get("x-api-key")); originalAPIKey != "" {
				req.Out.Header.Set("X-External-API-Key", originalAPIKey)
			}
			req.Out.Header.Set("Authorization", auth.BearerToken(token))
			req.Out.Header.Set("X-Principal-Kind", current.kind)
			req.Out.Header.Set("X-Principal-Name", current.name)
			req.Out.Header.Set("X-Original-Host", r.Host)
			req.Out.Header.Set("X-Forwarded-For", clientIPFromRequest(r))
			if fingerprint := strings.TrimSpace(r.Header.Get("X-Device-Fingerprint")); fingerprint != "" {
				req.Out.Header.Set("X-Device-Fingerprint", fingerprint)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.Error(w, http.StatusBadGateway, "core_unavailable", proxyUserFacingErrorMessage(r.URL.Path, current.kind))
		},
	}
	proxy.ServeHTTP(w, r)
}

func proxyUserFacingErrorMessage(path string, kind string) string {
	if kind == "admin" || strings.HasPrefix(path, "/admin/") {
		return "后台服务暂时不可用，请稍后重试"
	}
	if strings.HasPrefix(path, "/chat/") || strings.HasPrefix(path, "/developer/") {
		return "与服务器断联，请重试"
	}
	return "服务暂时不可用，请稍后重试"
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func (s *Server) issueUserSession(w http.ResponseWriter, r *http.Request, userID string) error {
	csrfToken, err := auth.GenerateCSRFToken()
	if err != nil {
		return err
	}
	session, err := s.Store.CreateUserSession(r.Context(), userID, csrfToken, r.UserAgent(), clientIPFromRequest(r), time.Now().Add(s.Config.SessionTTL))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.Config.SessionCookieName,
		Value:    session.SessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.IsProd(),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	return nil
}

func (s *Server) issueAdminSession(w http.ResponseWriter, r *http.Request, adminID string) error {
	csrfToken, err := auth.GenerateCSRFToken()
	if err != nil {
		return err
	}
	session, err := s.Store.CreateAdminSession(r.Context(), adminID, csrfToken, r.UserAgent(), clientIPFromRequest(r), time.Now().Add(s.Config.AdminSessionTTL))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.Config.AdminSessionCookieName,
		Value:    session.SessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.IsProd(),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
	return nil
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.IsProd(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func requestBaseURL(r *http.Request, fallback string) string {
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	if idx := strings.Index(host, ","); idx >= 0 {
		host = strings.TrimSpace(host[:idx])
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if host == "" {
		return strings.TrimRight(fallback, "/")
	}
	return scheme + "://" + normalizeHost(host)
}

func normalizeHost(host string) string {
	if name, port, err := net.SplitHostPort(host); err == nil {
		return net.JoinHostPort(name, port)
	}
	return host
}

func translateUserCreateError(err error, isPhoneIdentifier bool) string {
	if err == nil {
		return "注册失败，请稍后重试"
	}
	detail := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(detail, "users_phone_unique_idx"):
		return "该手机号已被注册"
	case strings.Contains(detail, "users_email_key"):
		return "该邮箱已被注册"
	case strings.Contains(detail, "duplicate key value violates unique constraint"):
		if isPhoneIdentifier {
			return "该手机号已被注册"
		}
		return "该邮箱已被注册"
	default:
		return "注册失败，请稍后重试"
	}
}

func translateAdminCreateError(err error) string {
	if err == nil {
		return "管理员账号创建失败，请稍后重试"
	}
	detail := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(detail, "admin_users_email_key"):
		return "该管理员邮箱已存在"
	case strings.Contains(detail, "duplicate key value violates unique constraint"):
		return "管理员账号已存在，请勿重复创建"
	default:
		return "管理员账号创建失败，请稍后重试"
	}
}

func randomURLToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func adminBootstrapStateKey(token string) string {
	return "admin:bootstrap:" + token
}
