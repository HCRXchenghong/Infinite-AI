package httpx

import (
	"net/http"
	"testing"
)

func TestUserFacingMessagePreservesChinese(t *testing.T) {
	got := UserFacingMessage(http.StatusBadRequest, "captcha_invalid", "图形验证码不正确")
	if got != "图形验证码不正确" {
		t.Fatalf("UserFacingMessage() = %q", got)
	}
}

func TestUserFacingMessageTranslatesRawEnglish(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		code    string
		message string
		want    string
	}{
		{
			name:    "not allowed",
			status:  http.StatusForbidden,
			code:    "model_membership_limits_update_failed",
			message: "Not Allowed",
			want:    "当前账号无权执行该操作",
		},
		{
			name:    "captcha",
			status:  http.StatusBadRequest,
			code:    "captcha_invalid",
			message: "captcha answer is incorrect",
			want:    "图形验证码不正确",
		},
		{
			name:    "deadline",
			status:  http.StatusBadGateway,
			code:    "upstream_generation_failed",
			message: `Post "https://example.com/v1/chat/completions": context deadline exceeded`,
			want:    "与服务器断联，请重试",
		},
		{
			name:    "raw postgres",
			status:  http.StatusInternalServerError,
			code:    "settings_update_failed",
			message: `ERROR: duplicate key value violates unique constraint "example"`,
			want:    "数据已存在，请检查后重试",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := UserFacingMessage(test.status, test.code, test.message); got != test.want {
				t.Fatalf("UserFacingMessage() = %q, want %q", got, test.want)
			}
		})
	}
}
