package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"
)

const userFacingChatDisconnectMessage = "与服务器断联，请重试"
const maxUpstreamErrorBodyBytes = 4096
const maxStoredErrorDetailRunes = 600

type upstreamHTTPError struct {
	Service    string
	StatusCode int
	Body       string
}

func (e *upstreamHTTPError) Error() string {
	if e == nil {
		return ""
	}
	service := strings.TrimSpace(e.Service)
	if service == "" {
		service = "upstream"
	}
	detail := sanitizeRawErrorText(e.Body)
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	if detail == "" {
		detail = "request failed"
	}
	return fmt.Sprintf("%s upstream failed (HTTP %d): %s", service, e.StatusCode, detail)
}

func newUpstreamHTTPError(service string, statusCode int, body string) error {
	return &upstreamHTTPError{
		Service:    service,
		StatusCode: statusCode,
		Body:       body,
	}
}

func classifyUpstreamErrorStatus(err error) string {
	if err == nil {
		return "ok"
	}
	if isUpstreamConnectivityError(err) {
		return "upstream_disconnected"
	}
	return "upstream_failed"
}

func sanitizeUserFacingChatError(err error) string {
	if err == nil {
		return userFacingChatDisconnectMessage
	}
	detail := strings.ToLower(summarizeErrorDetail(err))
	if strings.Contains(detail, "auth_unavailable") ||
		strings.Contains(detail, "no auth available") ||
		strings.Contains(detail, "invalid api key") ||
		strings.Contains(detail, "invalid_api_key") ||
		strings.Contains(detail, "unauthorized") ||
		strings.Contains(detail, "incorrect api key") {
		return "模型供应商认证不可用，请检查 API Key 或切换可用线路后重试。"
	}
	if strings.Contains(detail, "insufficient_quota") ||
		strings.Contains(detail, "quota") ||
		strings.Contains(detail, "rate limit") ||
		strings.Contains(detail, "rate_limit") ||
		strings.Contains(detail, "too many requests") {
		return "模型供应商额度不足或限流，请稍后重试或切换可用线路。"
	}
	if isUpstreamConnectivityError(err) {
		if strings.Contains(detail, "timeout") ||
			strings.Contains(detail, "time-out") ||
			strings.Contains(detail, "timed out") ||
			strings.Contains(detail, "deadline") ||
			strings.Contains(detail, "524") {
			return "模型供应商响应超时，请稍后重试或切换可用线路。"
		}
		return "模型供应商暂时不可用，请稍后重试或切换可用线路。"
	}
	return userFacingChatDisconnectMessage
}

func summarizeErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	return clipRunes(sanitizeRawErrorText(err.Error()), maxStoredErrorDetailRunes)
}

func isUpstreamConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var upstreamErr *upstreamHTTPError
	if errors.As(err, &upstreamErr) {
		return upstreamErr.StatusCode == http.StatusRequestTimeout ||
			upstreamErr.StatusCode == http.StatusTooManyRequests ||
			upstreamErr.StatusCode >= http.StatusInternalServerError
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	lower := strings.ToLower(err.Error())
	needles := []string{
		" eof",
		": eof",
		"connection refused",
		"no such host",
		"timeout",
		"time-out",
		"timed out",
		"context deadline exceeded",
		"gateway timeout",
		"gateway time-out",
		"504 gateway",
		"tls handshake timeout",
		"reset by peer",
		"broken pipe",
		"dial tcp",
		"server misbehaving",
		"connection reset",
	}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func readUpstreamErrorBody(body io.Reader) string {
	if body == nil {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(body, maxUpstreamErrorBodyBytes))
	return strings.TrimSpace(string(raw))
}

func sanitizeRawErrorText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if message := extractJSONErrorMessage(value); message != "" {
		value = message
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<body") || strings.Contains(lower, "<center") || strings.Contains(lower, "</") {
		value = stripHTMLTags(value)
	}
	value = html.UnescapeString(value)
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func extractJSONErrorMessage(value string) string {
	var payload any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return ""
	}
	if message := extractMessageFromJSON(payload); message != "" {
		return message
	}
	return ""
}

func extractMessageFromJSON(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"message", "detail", "error_description"} {
			if message, ok := typed[key].(string); ok && strings.TrimSpace(message) != "" {
				return strings.TrimSpace(message)
			}
		}
		if nested, ok := typed["error"]; ok {
			if message := extractMessageFromJSON(nested); message != "" {
				return message
			}
		}
	case string:
		return strings.TrimSpace(typed)
	}
	return ""
}

func stripHTMLTags(value string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			builder.WriteRune(' ')
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}

func clipRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
