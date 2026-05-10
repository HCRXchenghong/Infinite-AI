package app

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyUpstreamErrorStatus(t *testing.T) {
	if got := classifyUpstreamErrorStatus(errors.New("Post \"https://example.com\": EOF")); got != "upstream_disconnected" {
		t.Fatalf("classifyUpstreamErrorStatus() = %q, want upstream_disconnected", got)
	}
	if got := classifyUpstreamErrorStatus(newUpstreamHTTPError("openai-compatible", 504, "<html><body><h1>504 Gateway Time-out</h1></body></html>")); got != "upstream_disconnected" {
		t.Fatalf("classifyUpstreamErrorStatus() = %q, want upstream_disconnected", got)
	}
	if got := classifyUpstreamErrorStatus(errors.New("openai-compatible upstream failed: invalid api key")); got != "upstream_failed" {
		t.Fatalf("classifyUpstreamErrorStatus() = %q, want upstream_failed", got)
	}
}

func TestSanitizeUserFacingChatError(t *testing.T) {
	if got := sanitizeUserFacingChatError(errors.New("some raw upstream failure")); got != userFacingChatDisconnectMessage {
		t.Fatalf("sanitizeUserFacingChatError() = %q, want %q", got, userFacingChatDisconnectMessage)
	}
	if got := sanitizeUserFacingChatError(newUpstreamHTTPError("openai-compatible image", 503, "auth_unavailable: no auth available")); !strings.Contains(got, "认证不可用") {
		t.Fatalf("sanitizeUserFacingChatError() = %q, want auth hint", got)
	}
	if got := sanitizeUserFacingChatError(newUpstreamHTTPError("openai-compatible image", 524, "error code: 524")); !strings.Contains(got, "响应超时") {
		t.Fatalf("sanitizeUserFacingChatError() = %q, want timeout hint", got)
	}
}

func TestSummarizeErrorDetailStripsGatewayHTML(t *testing.T) {
	err := newUpstreamHTTPError("openai-compatible", 504, "<html>\n<head><title>504 Gateway Time-out</title></head>\n<body bgcolor=\"white\">\n<center><h1>504 Gateway Time-out</h1></center>\n<hr><center>alb</center>\n</body>\n</html>")
	got := summarizeErrorDetail(err)
	if got != "openai-compatible upstream failed (HTTP 504): 504 Gateway Time-out 504 Gateway Time-out alb" {
		t.Fatalf("summarizeErrorDetail() = %q", got)
	}
	if strings.Contains(got, "<html>") || strings.Contains(got, "\n") {
		t.Fatalf("expected HTML to be stripped, got %q", got)
	}
}
