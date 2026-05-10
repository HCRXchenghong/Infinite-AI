package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
)

type riskRule struct {
	Window              time.Duration
	LimitPerIP          int64
	LimitPerFingerprint int64
	BlockDuration       time.Duration
	Message             string
}

func (s *Server) riskGuard(scope string, rule riskRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Redis == nil {
				next.ServeHTTP(w, r)
				return
			}
			blocked, err := s.enforceRiskRule(r, scope, rule)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if blocked {
				message := strings.TrimSpace(rule.Message)
				if message == "" {
					message = "操作过于频繁，请稍后再试。"
				}
				httpx.Error(w, http.StatusTooManyRequests, "risk_limited", message)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) enforceRiskRule(r *http.Request, scope string, rule riskRule) (bool, error) {
	if rule.Window <= 0 {
		rule.Window = 10 * time.Minute
	}
	if rule.BlockDuration <= 0 {
		rule.BlockDuration = 30 * time.Minute
	}
	ip := clientIPFromRequest(r)
	if ip != "" && rule.LimitPerIP > 0 {
		blocked, err := s.consumeRiskQuota(r, fmt.Sprintf("risk:%s:ip:%s", scope, ip), fmt.Sprintf("risk:block:%s:ip:%s", scope, ip), rule.LimitPerIP, rule.Window, rule.BlockDuration)
		if blocked || err != nil {
			return blocked, err
		}
	}
	fingerprint := requestFingerprint(r)
	if fingerprint != "" && rule.LimitPerFingerprint > 0 {
		blocked, err := s.consumeRiskQuota(r, fmt.Sprintf("risk:%s:fingerprint:%s", scope, fingerprint), fmt.Sprintf("risk:block:%s:fingerprint:%s", scope, fingerprint), rule.LimitPerFingerprint, rule.Window, rule.BlockDuration)
		if blocked || err != nil {
			return blocked, err
		}
	}
	return false, nil
}

func (s *Server) consumeRiskQuota(r *http.Request, counterKey string, blockKey string, limit int64, window time.Duration, blockDuration time.Duration) (bool, error) {
	exists, err := s.Redis.Exists(r.Context(), blockKey).Result()
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return true, nil
	}
	count, err := s.Redis.Incr(r.Context(), counterKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := s.Redis.Expire(r.Context(), counterKey, window).Err(); err != nil {
			return false, err
		}
	}
	if count <= limit {
		return false, nil
	}
	if err := s.Redis.Set(r.Context(), blockKey, "1", blockDuration).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func clientIPFromRequest(r *http.Request) string {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func requestFingerprint(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("X-Device-Fingerprint"))
	if raw == "" {
		raw = strings.TrimSpace(strings.Join([]string{
			r.UserAgent(),
			r.Header.Get("Accept-Language"),
			r.Header.Get("Sec-CH-UA-Platform"),
		}, "|"))
	}
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}
