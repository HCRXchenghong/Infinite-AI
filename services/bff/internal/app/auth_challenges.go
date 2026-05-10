package app

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/big"
	mathrand "math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type sendPhoneCodeRequest struct {
	Phone         string `json:"phone"`
	Purpose       string `json:"purpose"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type sendContactCodeRequest struct {
	Identifier    string `json:"identifier"`
	Purpose       string `json:"purpose"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type captchaOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	captchaID, err := randomURLToken(18)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "captcha_generate_failed", "验证码生成失败，请稍后重试")
		return
	}
	challenge, answer, err := randomCaptchaChallenge()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "captcha_generate_failed", "验证码生成失败，请稍后重试")
		return
	}
	if err := s.Redis.Set(r.Context(), captchaKey(captchaID), strings.ToLower(answer), 5*time.Minute).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "captcha_store_failed", "验证码保存失败，请稍后重试")
		return
	}
	challenge["captchaId"] = captchaID
	challenge["expiresInSeconds"] = 300
	httpx.JSON(w, http.StatusOK, challenge)
}

func (s *Server) handleSendPhoneCode(w http.ResponseWriter, r *http.Request) {
	var body sendPhoneCodeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	s.sendContactCodeResponse(w, r, body.Phone, body.Purpose, body.CaptchaID, body.CaptchaAnswer)
}

func (s *Server) handleSendContactCode(w http.ResponseWriter, r *http.Request) {
	var body sendContactCodeRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_payload", "请求参数不正确")
		return
	}
	s.sendContactCodeResponse(w, r, body.Identifier, body.Purpose, body.CaptchaID, body.CaptchaAnswer)
}

func (s *Server) sendContactCodeResponse(w http.ResponseWriter, r *http.Request, identifier string, purpose string, captchaID string, captchaAnswer string) {
	purpose = strings.TrimSpace(strings.ToLower(purpose))
	if purpose == "" {
		purpose = "register"
	}
	if purpose != "register" {
		if err := s.validateCaptcha(r.Context(), captchaID, captchaAnswer); err != nil {
			httpx.Error(w, http.StatusBadRequest, "captcha_invalid", err.Error())
			return
		}
	}
	authSecurity, err := s.Store.GetAuthSecuritySettings(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "settings_load_failed", "系统配置加载失败，请稍后重试")
		return
	}
	contact, err := resolveVerificationContact(identifier)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "contact_invalid", err.Error())
		return
	}
	if exists, _ := s.Redis.Exists(r.Context(), contactSendCooldownKey(contact.Kind, contact.Value, purpose)).Result(); exists > 0 {
		httpx.Error(w, http.StatusTooManyRequests, "verification_rate_limited", "请求过于频繁，请稍后再试")
		return
	}
	code, err := randomDigits(6)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "verification_code_generate_failed", "验证码生成失败，请稍后重试")
		return
	}
	previewCode := ""
	deliveryMode := "email"
	switch {
	case authSecurity.VerificationTestMode:
		deliveryMode = "test"
		previewCode = code
	case contact.Kind == "phone":
		deliveryMode = "sms"
		gateway, err := s.Store.GetSMSGatewayConfig(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "sms_gateway_load_failed", "短信网关配置读取失败，请稍后重试")
			return
		}
		if !gateway.Enabled || strings.TrimSpace(gateway.EndpointURL) == "" {
			httpx.Error(w, http.StatusFailedDependency, "sms_gateway_not_configured", "短信网关尚未配置完成，暂时无法发送验证码")
			return
		}
		if err := s.sendSMS(r.Context(), gateway, contact.Value, code, purpose, authSecurity.SMSCodeTTLSeconds); err != nil {
			httpx.Error(w, http.StatusBadGateway, "sms_send_failed", "短信验证码发送失败，请稍后重试")
			return
		}
	default:
		gateway, err := s.Store.GetEmailGatewayConfig(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "email_gateway_load_failed", "邮箱网关配置读取失败，请稍后重试")
			return
		}
		if !gateway.Enabled || strings.TrimSpace(gateway.EndpointURL) == "" {
			httpx.Error(w, http.StatusFailedDependency, "email_gateway_not_configured", "邮箱网关尚未配置完成，暂时无法发送验证码")
			return
		}
		if err := s.sendEmail(r.Context(), gateway, contact.Value, code, purpose, authSecurity.SMSCodeTTLSeconds); err != nil {
			httpx.Error(w, http.StatusBadGateway, "email_send_failed", "邮箱验证码发送失败，请稍后重试")
			return
		}
	}
	if err := s.Redis.Set(r.Context(), contactCodeKey(contact.Kind, contact.Value, purpose), code, time.Duration(authSecurity.SMSCodeTTLSeconds)*time.Second).Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "verification_code_store_failed", "验证码保存失败，请稍后重试")
		return
	}
	_ = s.Redis.Set(r.Context(), contactSendCooldownKey(contact.Kind, contact.Value, purpose), "1", 60*time.Second).Err()
	response := map[string]any{
		"ok":               true,
		"identifier":       contact.Masked,
		"kind":             contact.Kind,
		"purpose":          purpose,
		"deliveryMode":     deliveryMode,
		"expiresInSeconds": authSecurity.SMSCodeTTLSeconds,
	}
	if previewCode != "" {
		response["previewCode"] = previewCode
	}
	s.logSystemEvent(r, "info", "auth", "verification_code_sent", "验证码已发送", map[string]any{
		"identifier":       contact.Value,
		"maskedIdentifier": contact.Masked,
		"kind":             contact.Kind,
		"purpose":          purpose,
		"code":             code,
		"deliveryMode":     deliveryMode,
		"expiresInSeconds": authSecurity.SMSCodeTTLSeconds,
	})
	httpx.JSON(w, http.StatusOK, response)
}

func (s *Server) validateCaptcha(ctx context.Context, captchaID string, answer string) error {
	if strings.TrimSpace(captchaID) == "" || strings.TrimSpace(answer) == "" {
		return fmt.Errorf("请输入图形验证码")
	}
	expected, err := s.Redis.Get(ctx, captchaKey(captchaID)).Result()
	if err != nil {
		return fmt.Errorf("图形验证码已过期，请刷新后重试")
	}
	if strings.ToLower(strings.TrimSpace(answer)) != expected {
		return fmt.Errorf("图形验证码不正确")
	}
	_, _ = s.Redis.Del(ctx, captchaKey(captchaID)).Result()
	return nil
}

func (s *Server) verifyPhoneCode(ctx context.Context, phone string, code string, purpose string) error {
	return s.verifyContactCode(ctx, phone, code, purpose)
}

func (s *Server) verifyContactCode(ctx context.Context, identifier string, code string, purpose string) error {
	if strings.TrimSpace(identifier) == "" || strings.TrimSpace(code) == "" {
		return fmt.Errorf("请输入验证码")
	}
	contact, err := resolveVerificationContact(identifier)
	if err != nil {
		return err
	}
	expected, err := s.Redis.Get(ctx, contactCodeKey(contact.Kind, contact.Value, purpose)).Result()
	if err != nil {
		return fmt.Errorf("验证码已过期，请重新获取")
	}
	if strings.TrimSpace(code) != expected {
		return fmt.Errorf("验证码不正确")
	}
	_, _ = s.Redis.Del(ctx, contactCodeKey(contact.Kind, contact.Value, purpose)).Result()
	return nil
}

func (s *Server) sendSMS(ctx context.Context, gateway *store.SMSGatewayConfig, phone string, code string, purpose string, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	message := gateway.MessageTemplate
	if strings.TrimSpace(message) == "" {
		message = "【Infinite-AI】您的验证码是 {{code}}，{{minutes}} 分钟内有效。"
	}
	message = strings.ReplaceAll(message, "{{code}}", code)
	message = strings.ReplaceAll(message, "{{minutes}}", fmt.Sprintf("%d", ttlSeconds/60))
	payload := map[string]any{
		"to":       phone,
		"code":     code,
		"message":  message,
		"purpose":  purpose,
		"provider": gateway.ProviderName,
		"senderId": gateway.SenderID,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.EndpointURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	switch strings.ToLower(strings.TrimSpace(gateway.AuthScheme)) {
	case "header":
		headerName := gateway.HeaderName
		if headerName == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, gateway.AuthToken)
	case "bearer":
		if gateway.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+gateway.AuthToken)
		}
	default:
		if gateway.AuthToken != "" {
			req.Header.Set("Authorization", gateway.AuthToken)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("sms gateway returned %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (s *Server) sendEmail(ctx context.Context, gateway *store.EmailGatewayConfig, email string, code string, purpose string, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	subject := gateway.SubjectTemplate
	if strings.TrimSpace(subject) == "" {
		subject = "【Infinite-AI】您的验证码是 {{code}}"
	}
	content := gateway.ContentTemplate
	if strings.TrimSpace(content) == "" {
		content = "您的验证码是 {{code}}，{{minutes}} 分钟内有效。"
	}
	subject = strings.ReplaceAll(subject, "{{code}}", code)
	subject = strings.ReplaceAll(subject, "{{minutes}}", fmt.Sprintf("%d", ttlSeconds/60))
	content = strings.ReplaceAll(content, "{{code}}", code)
	content = strings.ReplaceAll(content, "{{minutes}}", fmt.Sprintf("%d", ttlSeconds/60))
	payload := map[string]any{
		"to":          email,
		"code":        code,
		"subject":     subject,
		"text":        content,
		"html":        strings.ReplaceAll(content, "\n", "<br>"),
		"purpose":     purpose,
		"provider":    gateway.ProviderName,
		"fromAddress": gateway.FromAddress,
		"fromName":    gateway.FromName,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.EndpointURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	switch strings.ToLower(strings.TrimSpace(gateway.AuthScheme)) {
	case "header":
		headerName := gateway.HeaderName
		if headerName == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, gateway.AuthToken)
	case "bearer":
		if gateway.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+gateway.AuthToken)
		}
	default:
		if gateway.AuthToken != "" {
			req.Header.Set("Authorization", gateway.AuthToken)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("email gateway returned %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func profileField(payload map[string]any, path string) any {
	current := any(payload)
	parts := strings.Split(strings.TrimSpace(path), ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func stringField(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func looksLikePhone(value string) bool {
	if strings.Contains(value, "@") {
		return false
	}
	return normalizePhone(value) != ""
}

func looksLikeEmail(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	return normalized != "" && strings.Contains(normalized, "@")
}

func normalizePhone(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "", "\t", "")
	cleaned = replacer.Replace(cleaned)
	if strings.HasPrefix(cleaned, "00") {
		cleaned = "+" + strings.TrimPrefix(cleaned, "00")
	}
	if strings.HasPrefix(cleaned, "+") {
		digits := cleaned[1:]
		if len(digits) < 8 || len(digits) > 15 {
			return ""
		}
		for _, ch := range digits {
			if ch < '0' || ch > '9' {
				return ""
			}
		}
		return "+" + digits
	}
	for _, ch := range cleaned {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	if len(cleaned) == 11 && strings.HasPrefix(cleaned, "1") {
		return "+86" + cleaned
	}
	if len(cleaned) >= 8 && len(cleaned) <= 15 {
		return "+" + cleaned
	}
	return ""
}

func maskPhone(phone string) string {
	if len(phone) <= 6 {
		return phone
	}
	return phone[:4] + strings.Repeat("*", max(0, len(phone)-7)) + phone[len(phone)-3:]
}

func normalizeEmail(value string) string {
	email := strings.TrimSpace(strings.ToLower(value))
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	return email
}

func maskEmail(email string) string {
	parts := strings.SplitN(normalizeEmail(email), "@", 2)
	if len(parts) != 2 {
		return email
	}
	local := parts[0]
	if len(local) <= 2 {
		return local + "***@" + parts[1]
	}
	return local[:1] + strings.Repeat("*", max(0, len(local)-2)) + local[len(local)-1:] + "@" + parts[1]
}

type verificationContact struct {
	Kind   string
	Value  string
	Masked string
}

func resolveVerificationContact(identifier string) (verificationContact, error) {
	if looksLikePhone(identifier) {
		phone := normalizePhone(identifier)
		if phone == "" {
			return verificationContact{}, fmt.Errorf("请输入正确的手机号")
		}
		return verificationContact{Kind: "phone", Value: phone, Masked: maskPhone(phone)}, nil
	}
	email := normalizeEmail(identifier)
	if email == "" {
		return verificationContact{}, fmt.Errorf("请输入正确的邮箱或手机号")
	}
	return verificationContact{Kind: "email", Value: email, Masked: maskEmail(email)}, nil
}

func buildPhonePlaceholderEmail(phone string) string {
	digits := strings.TrimPrefix(phone, "+")
	if digits == "" {
		digits = "user"
	}
	return "phone+" + digits + "@phone.infinite.local"
}

func randomCaptchaText(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	return randomFromAlphabet(alphabet, length)
}

func randomCaptchaChallenge() (map[string]any, string, error) {
	challengeType, err := randomInt(3)
	if err != nil {
		return nil, "", err
	}
	switch challengeType {
	case 1:
		targets := []int{18, 26, 34, 42, 58, 66, 74, 82}
		targetIndex, err := randomInt(len(targets))
		if err != nil {
			return nil, "", err
		}
		target := targets[targetIndex]
		return map[string]any{
			"challengeType": "slide",
			"prompt":        fmt.Sprintf("请把滑块拖到 %d%% 后提交。", target),
			"imageDataUrl":  renderSlideCaptchaDataURL(target),
		}, fmt.Sprintf("%d", target), nil
	case 2:
		return randomChoiceCaptchaChallenge()
	default:
		answer, err := randomCaptchaText(5)
		if err != nil {
			return nil, "", err
		}
		return map[string]any{
			"challengeType": "text",
			"prompt":        "请输入图中的验证码。",
			"imageDataUrl":  renderCaptchaDataURL(answer),
		}, answer, nil
	}
}

func randomChoiceCaptchaChallenge() (map[string]any, string, error) {
	options := []captchaOption{
		{Label: "蓝色圆形", Value: "blue-circle"},
		{Label: "橙色三角形", Value: "orange-triangle"},
		{Label: "绿色方形", Value: "green-square"},
		{Label: "银色星形", Value: "silver-star"},
	}
	targetIndex, err := randomInt(len(options))
	if err != nil {
		return nil, "", err
	}
	target := options[targetIndex]
	return map[string]any{
		"challengeType": "choice",
		"prompt":        "请选择图中带高亮边框的图形描述。",
		"imageDataUrl":  renderChoiceCaptchaDataURL(target.Value),
		"options":       options,
	}, target.Value, nil
}

func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func randomDigits(length int) (string, error) {
	return randomFromAlphabet("0123456789", length)
}

func randomFromAlphabet(alphabet string, length int) (string, error) {
	var builder strings.Builder
	for i := 0; i < length; i++ {
		n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		builder.WriteByte(alphabet[n.Int64()])
	}
	return builder.String(), nil
}

func renderCaptchaDataURL(answer string) string {
	seed := int64(0)
	for _, ch := range answer {
		seed += int64(ch)
	}
	rng := mathrand.New(mathrand.NewSource(seed + time.Now().UnixNano()))
	var builder strings.Builder
	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="56" viewBox="0 0 160 56" fill="none">`)
	builder.WriteString(`<rect width="160" height="56" rx="14" fill="#111827"/>`)
	for i := 0; i < 10; i++ {
		builder.WriteString(fmt.Sprintf(`<path d="M%d %d Q %d %d %d %d" stroke="rgba(148,163,184,0.25)" stroke-width="1.2" fill="none"/>`,
			rng.Intn(30),
			rng.Intn(56),
			rng.Intn(80)+20,
			rng.Intn(56),
			rng.Intn(80)+70,
			rng.Intn(56),
		))
	}
	for index, ch := range answer {
		x := 18 + index*26
		y := 34 + rng.Intn(10) - 5
		rotation := rng.Intn(24) - 12
		builder.WriteString(fmt.Sprintf(`<text x="%d" y="%d" transform="rotate(%d %d %d)" fill="#F8FAFC" font-size="28" font-family="Arial, Helvetica, sans-serif" font-weight="700">%s</text>`,
			x, y, rotation, x, y, html.EscapeString(string(ch)),
		))
	}
	builder.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(builder.String()))
}

func renderSlideCaptchaDataURL(target int) string {
	var builder strings.Builder
	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="320" height="92" viewBox="0 0 320 92" fill="none">`)
	builder.WriteString(`<rect width="320" height="92" rx="20" fill="#111827"/>`)
	builder.WriteString(`<rect x="28" y="42" width="264" height="10" rx="5" fill="#374151"/>`)
	markerX := 28 + int(float64(264)*float64(target)/100)
	builder.WriteString(fmt.Sprintf(`<line x1="%d" y1="26" x2="%d" y2="68" stroke="#F8FAFC" stroke-width="3" stroke-linecap="round"/>`, markerX, markerX))
	builder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="47" r="13" fill="#10B981" opacity="0.9"/>`, markerX))
	builder.WriteString(`<text x="28" y="24" fill="#CBD5E1" font-size="13" font-family="Arial, Helvetica, sans-serif">拖动到高亮位置</text>`)
	builder.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(builder.String()))
}

func renderChoiceCaptchaDataURL(targetValue string) string {
	type shape struct {
		Value string
		Fill  string
		Kind  string
		X     int
		Y     int
	}
	shapes := []shape{
		{Value: "blue-circle", Fill: "#3B82F6", Kind: "circle", X: 62, Y: 58},
		{Value: "orange-triangle", Fill: "#F97316", Kind: "triangle", X: 142, Y: 58},
		{Value: "green-square", Fill: "#22C55E", Kind: "square", X: 222, Y: 58},
		{Value: "silver-star", Fill: "#CBD5E1", Kind: "star", X: 302, Y: 58},
	}
	var builder strings.Builder
	builder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="364" height="116" viewBox="0 0 364 116" fill="none">`)
	builder.WriteString(`<rect width="364" height="116" rx="22" fill="#111827"/>`)
	builder.WriteString(`<text x="22" y="25" fill="#CBD5E1" font-size="13" font-family="Arial, Helvetica, sans-serif">选择高亮图形</text>`)
	for _, item := range shapes {
		if item.Value == targetValue {
			builder.WriteString(fmt.Sprintf(`<rect x="%d" y="34" width="58" height="58" rx="16" stroke="#F8FAFC" stroke-width="3" fill="rgba(255,255,255,0.08)"/>`, item.X-29))
		}
		switch item.Kind {
		case "circle":
			builder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="18" fill="%s"/>`, item.X, item.Y, item.Fill))
		case "triangle":
			builder.WriteString(fmt.Sprintf(`<path d="M%d %d L%d %d L%d %d Z" fill="%s"/>`, item.X, item.Y-21, item.X-22, item.Y+18, item.X+22, item.Y+18, item.Fill))
		case "square":
			builder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="36" height="36" rx="8" fill="%s"/>`, item.X-18, item.Y-18, item.Fill))
		case "star":
			builder.WriteString(fmt.Sprintf(`<path d="M%d %d L%d %d L%d %d L%d %d L%d %d L%d %d L%d %d L%d %d L%d %d L%d %d Z" fill="%s"/>`, item.X, item.Y-23, item.X+7, item.Y-7, item.X+24, item.Y-6, item.X+11, item.Y+5, item.X+15, item.Y+22, item.X, item.Y+13, item.X-15, item.Y+22, item.X-11, item.Y+5, item.X-24, item.Y-6, item.X-7, item.Y-7, item.Fill))
		}
	}
	builder.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(builder.String()))
}

func captchaKey(captchaID string) string {
	return "auth:captcha:" + captchaID
}

func contactCodeKey(kind string, value string, purpose string) string {
	return "auth:" + kind + ":" + purpose + ":" + value
}

func contactSendCooldownKey(kind string, value string, purpose string) string {
	return "auth:" + kind + ":cooldown:" + purpose + ":" + value
}

func phoneCodeKey(phone string, purpose string) string {
	return contactCodeKey("phone", phone, purpose)
}

func phoneSendCooldownKey(phone string, purpose string) string {
	return contactSendCooldownKey("phone", phone, purpose)
}
