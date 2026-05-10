package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
)

func (s *Server) handleIFPayWebhook(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "webhook_read_failed", "无法读取支付回调内容")
		return
	}
	payload := parseIFPayWebhookPayload(r.Header.Get("Content-Type"), rawBody)
	eventType := firstNonEmpty(
		webhookString(payload, "event"),
		webhookString(payload, "eventType"),
		webhookString(payload, "notifyType"),
		webhookString(payload, "type"),
		"ifpay.webhook",
	)
	rawStatus := firstNonEmpty(
		webhookString(payload, "status"),
		webhookString(payload, "tradeStatus"),
		webhookString(payload, "trade_status"),
		webhookString(payload, "paymentStatus"),
		webhookString(payload, "payment_status"),
		webhookString(payload, "orderStatus"),
		webhookString(payload, "order_status"),
		eventType,
	)
	orderID := firstNonEmpty(
		webhookString(payload, "orderId"),
		webhookString(payload, "order_id"),
		webhookString(payload, "merchantOrderId"),
		webhookString(payload, "merchant_order_id"),
		webhookString(payload, "merchantOrderNo"),
		webhookString(payload, "merchant_order_no"),
		webhookString(payload, "outTradeNo"),
		webhookString(payload, "out_trade_no"),
		webhookString(payload, "data.orderId"),
		webhookString(payload, "data.order_id"),
		webhookString(payload, "metadata.orderId"),
		webhookString(payload, "metadata.order_id"),
	)
	ifpayPaymentID := firstNonEmpty(
		webhookString(payload, "paymentId"),
		webhookString(payload, "payment_id"),
		webhookString(payload, "tradeNo"),
		webhookString(payload, "trade_no"),
		webhookString(payload, "transactionId"),
		webhookString(payload, "transaction_id"),
		webhookString(payload, "data.paymentId"),
		webhookString(payload, "data.payment_id"),
	)
	ifpayOrderID := firstNonEmpty(
		webhookString(payload, "ifpayOrderId"),
		webhookString(payload, "ifpay_order_id"),
		webhookString(payload, "providerOrderId"),
		webhookString(payload, "provider_order_id"),
		webhookString(payload, "gatewayOrderId"),
		webhookString(payload, "gateway_order_id"),
		webhookString(payload, "data.ifpayOrderId"),
		webhookString(payload, "data.ifpay_order_id"),
	)
	signatureOK := s.verifyIFPayWebhookSignature(r, rawBody)
	_ = s.Store.RecordPaymentEvent(r.Context(), orderID, eventType, payload, signatureOK)

	switch normalizeWebhookPaymentStatus(rawStatus, eventType) {
	case "succeeded":
		if orderID == "" {
			httpx.JSON(w, http.StatusAccepted, map[string]any{
				"ok":      false,
				"status":  "received",
				"message": "支付回调缺少订单号，暂未执行履约",
			})
			return
		}
		if err := s.Store.ApplySuccessfulPayment(r.Context(), orderID, ifpayPaymentID, ifpayOrderID, payload); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "payment_apply_failed", err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"status":  "succeeded",
			"orderId": orderID,
		})
		return
	case "failed", "cancelled":
		if orderID != "" {
			_ = s.Store.UpdatePaymentOrderStatus(r.Context(), orderID, normalizeWebhookPaymentStatus(rawStatus, eventType), ifpayPaymentID, ifpayOrderID, payload)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"status":  normalizeWebhookPaymentStatus(rawStatus, eventType),
			"orderId": orderID,
		})
		return
	default:
		if orderID != "" {
			_ = s.Store.UpdatePaymentOrderStatus(r.Context(), orderID, "processing", ifpayPaymentID, ifpayOrderID, payload)
		}
		httpx.JSON(w, http.StatusAccepted, map[string]any{
			"ok":      true,
			"status":  "received",
			"orderId": orderID,
		})
		return
	}
}

func parseIFPayWebhookPayload(contentType string, rawBody []byte) map[string]any {
	payload := map[string]any{}
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		if err := json.Unmarshal(rawBody, &payload); err == nil && len(payload) > 0 {
			return payload
		}
	}
	if values, err := url.ParseQuery(string(rawBody)); err == nil && len(values) > 0 {
		for key, items := range values {
			if len(items) == 1 {
				payload[key] = items[0]
				continue
			}
			copied := make([]string, 0, len(items))
			copied = append(copied, items...)
			payload[key] = copied
		}
		if len(payload) > 0 {
			return payload
		}
	}
	payload["raw"] = string(rawBody)
	return payload
}

func webhookString(payload map[string]any, path string) string {
	current := any(payload)
	for _, part := range strings.Split(path, ".") {
		mapped, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = mapped[part]
		if !ok {
			return ""
		}
	}
	switch value := current.(type) {
	case string:
		return strings.TrimSpace(value)
	case []string:
		if len(value) == 0 {
			return ""
		}
		return strings.TrimSpace(value[0])
	case []any:
		if len(value) == 0 {
			return ""
		}
		if first, ok := value[0].(string); ok {
			return strings.TrimSpace(first)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeWebhookPaymentStatus(rawStatus string, eventType string) string {
	normalized := strings.ToLower(strings.TrimSpace(firstNonEmpty(rawStatus, eventType)))
	switch normalized {
	case "success", "succeed", "succeeded", "paid", "completed", "complete", "trade_success", "payment_success", "pay_success", "finish", "finished":
		return "succeeded"
	case "fail", "failed", "payment_failed", "trade_failed":
		return "failed"
	case "cancel", "cancelled", "canceled", "closed", "expired":
		return "cancelled"
	default:
		return "processing"
	}
}

func (s *Server) verifyIFPayWebhookSignature(r *http.Request, rawBody []byte) bool {
	signature := firstNonEmpty(
		r.Header.Get("X-IFPay-Signature"),
		r.Header.Get("X-Signature"),
		r.Header.Get("Signature"),
		webhookString(parseIFPayWebhookPayload(r.Header.Get("Content-Type"), rawBody), "sign"),
		webhookString(parseIFPayWebhookPayload(r.Header.Get("Content-Type"), rawBody), "signature"),
	)
	if signature == "" {
		return true
	}
	ifpayConfig, err := s.Store.GetIFPayConfig(r.Context())
	if err != nil {
		return false
	}
	secretKey, _ := ifpayConfig["secretKey"].(string)
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write(rawBody)
	sum := mac.Sum(nil)
	expectedHex := hex.EncodeToString(sum)
	expectedBase64 := base64.StdEncoding.EncodeToString(sum)
	signature = strings.TrimSpace(signature)
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expectedHex))) || hmac.Equal([]byte(signature), []byte(expectedBase64))
}
