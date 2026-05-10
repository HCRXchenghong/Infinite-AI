package app

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *loggingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		if w.statusCode == 0 {
			w.statusCode = http.StatusOK
		}
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(w.ResponseWriter, reader)
}

func (w *loggingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (s *Server) systemLogMiddleware(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			recorder := &loggingResponseWriter{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			if recorder.statusCode == 0 {
				recorder.statusCode = http.StatusOK
			}

			current := principal{}
			if value := r.Context().Value(contextKeyPrincipal); value != nil {
				current, _ = value.(principal)
			}
			account := current.email
			if account == "" {
				account = current.subjectID
			}
			level := "info"
			message := "请求完成"
			if recorder.statusCode >= 500 {
				level = "error"
				message = "请求失败"
			} else if recorder.statusCode >= 400 {
				level = "warn"
				message = "请求返回业务错误"
			}
			_ = s.Store.CreateSystemLog(r.Context(), store.CreateSystemLogInput{
				Service:     service,
				Level:       level,
				Category:    "request",
				EventType:   "http_request",
				Method:      r.Method,
				Path:        r.URL.Path,
				StatusCode:  recorder.statusCode,
				UserID:      userIDForPrincipal(current),
				AdminID:     adminIDForPrincipal(current),
				Account:     account,
				IP:          clientIPFromRequest(r),
				Fingerprint: strings.TrimSpace(r.Header.Get("X-Device-Fingerprint")),
				Message:     message,
				Payload: map[string]any{
					"latencyMs": time.Since(startedAt).Milliseconds(),
					"userAgent": r.UserAgent(),
					"query":     r.URL.RawQuery,
				},
			})
		})
	}
}

func (s *Server) logSystemEvent(r *http.Request, level string, category string, eventType string, message string, payload map[string]any) {
	current := principal{}
	if value := r.Context().Value(contextKeyPrincipal); value != nil {
		current, _ = value.(principal)
	}
	account := current.email
	if account == "" {
		account = current.subjectID
	}
	if account == "" {
		account = firstNonEmptyString(payload["identifier"], payload["target"])
	}
	_ = s.Store.CreateSystemLog(r.Context(), store.CreateSystemLogInput{
		Service:     "bff",
		Level:       level,
		Category:    category,
		EventType:   eventType,
		Method:      r.Method,
		Path:        r.URL.Path,
		UserID:      userIDForPrincipal(current),
		AdminID:     adminIDForPrincipal(current),
		Account:     account,
		IP:          clientIPFromRequest(r),
		Fingerprint: strings.TrimSpace(r.Header.Get("X-Device-Fingerprint")),
		Message:     message,
		Payload:     payload,
	})
}

func userIDForPrincipal(current principal) string {
	if current.kind == "user" {
		return current.subjectID
	}
	return ""
}

func adminIDForPrincipal(current principal) string {
	if current.kind == "admin" {
		return current.subjectID
	}
	return ""
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		}
	}
	return ""
}
