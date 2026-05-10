package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Error(w http.ResponseWriter, status int, code string, message string) {
	JSON(w, status, ErrorResponse{
		Error:   code,
		Message: UserFacingMessage(status, code, message),
	})
}

func Decode(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func UserFacingMessage(status int, code string, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return defaultStatusMessage(status)
	}
	if containsCJK(message) {
		return message
	}
	lower := strings.ToLower(message + " " + code)
	switch {
	case strings.Contains(lower, "not allowed") || strings.Contains(lower, "forbidden"):
		return "当前账号无权执行该操作"
	case strings.Contains(lower, "captcha"):
		return "图形验证码不正确"
	case strings.Contains(lower, "email or phone") && strings.Contains(lower, "password"):
		return "邮箱、手机号或密码不正确"
	case strings.Contains(lower, "api key is required"):
		return "请先提供 API Key"
	case strings.Contains(lower, "invalid api key") || strings.Contains(lower, "api key is invalid"):
		return "API Key 无效或已被撤销"
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests"):
		return "请求过于频繁，请稍后再试"
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "deadline") || strings.Contains(lower, "timeout") || strings.Contains(lower, "failed to fetch") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "eof"):
		return "与服务器断联，请重试"
	case strings.Contains(lower, "not found") || strings.Contains(lower, "no rows"):
		return "内容不存在或已被删除"
	case strings.Contains(lower, "duplicate key") || strings.Contains(lower, "unique constraint"):
		return "数据已存在，请检查后重试"
	case strings.Contains(lower, "invalid input syntax") || strings.Contains(lower, "invalid uuid") || strings.Contains(lower, "bad request"):
		return "请求参数不正确，请检查后重试"
	case strings.Contains(lower, "no active upstream endpoint") || strings.Contains(lower, "provider route") || strings.Contains(lower, "not configured"):
		return "模型上游端点未配置或未启用，请到后台模型管理检查配置"
	case strings.Contains(lower, "upstream") || strings.Contains(lower, "openai-compatible") || strings.Contains(lower, "anthropic"):
		return "上游模型返回异常，请检查模型配置或稍后重试"
	}
	return defaultStatusMessage(status)
}

func defaultStatusMessage(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "登录状态已失效，请重新登录"
	case status == http.StatusForbidden:
		return "当前账号无权执行该操作"
	case status == http.StatusNotFound:
		return "内容不存在或已被删除"
	case status == http.StatusTooManyRequests:
		return "请求过于频繁，请稍后再试"
	case status == http.StatusBadRequest:
		return "请求参数不正确，请检查后重试"
	case status == http.StatusBadGateway || status == http.StatusGatewayTimeout:
		return "与服务器断联，请重试"
	case status >= 500:
		return "服务器暂时不可用，请稍后重试"
	default:
		return "操作失败，请稍后重试"
	}
}

func containsCJK(value string) bool {
	for _, item := range value {
		if unicode.Is(unicode.Han, item) {
			return true
		}
	}
	return false
}
