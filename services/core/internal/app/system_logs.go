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

			current := principalFromContext(r.Context())
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
			userID := ""
			adminID := ""
			switch current.kind {
			case "user":
				userID = current.subjectID
			case "admin":
				adminID = current.subjectID
			}
			_ = s.Store.CreateSystemLog(r.Context(), store.CreateSystemLogInput{
				Service:     service,
				Level:       level,
				Category:    "request",
				EventType:   "http_request",
				Method:      r.Method,
				Path:        r.URL.Path,
				StatusCode:  recorder.statusCode,
				UserID:      userID,
				AdminID:     adminID,
				Account:     account,
				IP:          strings.TrimSpace(r.Header.Get("X-Forwarded-For")),
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
