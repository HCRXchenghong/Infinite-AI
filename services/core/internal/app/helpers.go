package app

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
)

func currentUserID(r *http.Request) string {
	return principalFromContext(r.Context()).subjectID
}

func currentAdminID(r *http.Request) string {
	return principalFromContext(r.Context()).subjectID
}

func currentPrincipal(r *http.Request) principal {
	return principalFromContext(r.Context())
}

func badRequest(w http.ResponseWriter, err error) {
	httpx.Error(w, http.StatusBadRequest, "invalid_request", "请求参数不正确")
}

func requestBaseURLForPort(r *http.Request, targetPort string, fallback string) string {
	host := strings.TrimSpace(r.Header.Get("X-Original-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
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
		return replaceURLPort(fallback, targetPort)
	}
	return scheme + "://" + replaceHostPort(host, targetPort)
}

func replaceURLPort(raw string, targetPort string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	parsed.Host = replaceHostPort(parsed.Host, targetPort)
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func replaceHostPort(host string, targetPort string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return net.JoinHostPort(name, targetPort)
	}
	if strings.HasPrefix(host, "[") && strings.Contains(host, "]") {
		return host
	}
	if strings.Contains(host, ":") && !strings.Contains(host, ".") && !strings.Contains(host, "localhost") {
		return host
	}
	return net.JoinHostPort(host, targetPort)
}
