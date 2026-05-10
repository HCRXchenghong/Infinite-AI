package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env                    string
	PublicBaseURL          string
	UserBaseURL            string
	APIBaseURL             string
	AdminBaseURL           string
	RedeemBaseURL          string
	SiteBaseURL            string
	WebPort                string
	BFFPort                string
	CorePort               string
	CoreBaseURL            string
	WorkerPort             string
	DatabaseURL            string
	RedisAddr              string
	NATSURL                string
	MinIOEndpoint          string
	MinIOAccessKey         string
	MinIOSecretKey         string
	MinIOBucket            string
	MinIOUseSSL            bool
	SessionCookieName      string
	AdminSessionCookieName string
	SessionTTL             time.Duration
	AdminSessionTTL        time.Duration
	WorkerPollInterval     time.Duration
	InternalJWTSecret      string
	CookieHashSecret       string
	MasterKey              string
	DefaultAdminEmail      string
	DefaultAdminPassword   string
	DefaultAdminDisplay    string
	DefaultAdminRole       string
	DefaultAdminTOTPSecret string
	IFPayBaseURL           string
	IFPayPartnerAppID      string
	IFPayClientID          string
	IFPayClientSecret      string
	IFPayPrivateKeyPEM     string
	IFPayWebhookPublicPEM  string
	DefaultChatRoute       string
	DeepSearchRoute        string
}

func Load() Config {
	userBaseURL := env("USER_BASE_URL", env("PUBLIC_BASE_URL", "http://127.0.0.1:1001"))
	return Config{
		Env:                    env("APP_ENV", "development"),
		PublicBaseURL:          userBaseURL,
		UserBaseURL:            userBaseURL,
		APIBaseURL:             env("API_BASE_URL", "http://127.0.0.1:1002"),
		AdminBaseURL:           env("ADMIN_BASE_URL", "http://127.0.0.1:1003"),
		RedeemBaseURL:          env("REDEEM_BASE_URL", "http://127.0.0.1:1004"),
		SiteBaseURL:            env("SITE_BASE_URL", "http://127.0.0.1:1000"),
		WebPort:                env("WEB_PORT", "1002"),
		BFFPort:                env("BFF_PORT", "1003"),
		CorePort:               env("CORE_PORT", "1004"),
		CoreBaseURL:            env("CORE_BASE_URL", "http://127.0.0.1:1004"),
		WorkerPort:             env("WORKER_PORT", "1005"),
		DatabaseURL:            env("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:1009/infinite_ai?sslmode=disable"),
		RedisAddr:              env("REDIS_ADDR", "127.0.0.1:1010"),
		NATSURL:                env("NATS_URL", "nats://127.0.0.1:1008"),
		MinIOEndpoint:          env("MINIO_ENDPOINT", "127.0.0.1:1006"),
		MinIOAccessKey:         env("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:         env("MINIO_SECRET_KEY", "minioadmin"),
		MinIOBucket:            env("MINIO_BUCKET", "infinite-ai"),
		MinIOUseSSL:            envBool("MINIO_USE_SSL", false),
		SessionCookieName:      env("SESSION_COOKIE_NAME", "ia_session"),
		AdminSessionCookieName: env("ADMIN_SESSION_COOKIE_NAME", "ia_admin_session"),
		SessionTTL:             envDuration("SESSION_TTL", 7*24*time.Hour),
		AdminSessionTTL:        envDuration("ADMIN_SESSION_TTL", 12*time.Hour),
		WorkerPollInterval:     envDuration("WORKER_POLL_INTERVAL", 30*time.Second),
		InternalJWTSecret:      env("INTERNAL_JWT_SECRET", "change-me-internal-jwt-secret"),
		CookieHashSecret:       env("COOKIE_HASH_SECRET", "change-me-cookie-hash-secret"),
		MasterKey:              env("MASTER_KEY", "0123456789abcdef0123456789abcdef"),
		DefaultAdminEmail:      env("DEFAULT_ADMIN_EMAIL", "admin@infinite.local"),
		DefaultAdminPassword:   env("DEFAULT_ADMIN_PASSWORD", "ChangeThisAdminPassword123!"),
		DefaultAdminDisplay:    env("DEFAULT_ADMIN_DISPLAY_NAME", "Admin Root"),
		DefaultAdminRole:       env("DEFAULT_ADMIN_ROLE", "super_admin"),
		DefaultAdminTOTPSecret: env("DEFAULT_ADMIN_TOTP_SECRET", "JBSWY3DPEHPK3PXP"),
		IFPayBaseURL:           env("IFPAY_BASE_URL", ""),
		IFPayPartnerAppID:      env("IFPAY_PARTNER_APP_ID", ""),
		IFPayClientID:          env("IFPAY_CLIENT_ID", ""),
		IFPayClientSecret:      env("IFPAY_CLIENT_SECRET", ""),
		IFPayPrivateKeyPEM:     multilineEnv("IFPAY_PRIVATE_KEY_PEM"),
		IFPayWebhookPublicPEM:  multilineEnv("IFPAY_WEBHOOK_PUBLIC_KEY_PEM"),
		DefaultChatRoute:       env("DEFAULT_CHAT_ROUTE", "infinite-ai-standard"),
		DeepSearchRoute:        env("DEEP_SEARCH_ROUTE", "infinite-ai-pro"),
	}
}

func (c Config) IsProd() bool {
	return strings.EqualFold(c.Env, "production")
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func multilineEnv(key string) string {
	value := os.Getenv(key)
	value = strings.ReplaceAll(value, `\n`, "\n")
	return value
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("invalid bool for %s: %v", key, err)
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("invalid duration for %s: %v", key, err)
		return fallback
	}
	return parsed
}
