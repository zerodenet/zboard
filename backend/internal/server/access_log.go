package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

const redactedSubscriptionPath = "/api/v1/client/subscription/[redacted]"

// ConfigureSafeHTTPLogging disables go-zero's native request logger because it
// dumps complete headers and bodies for 5xx responses. SafeAccessLogMiddleware
// replaces it with metadata-only access logs.
func ConfigureSafeHTTPLogging(config *rest.RestConf) {
	if config != nil {
		config.Middlewares.Log = false
	}
}

// SafeAccessLogMiddleware records status, method, a sanitized path, remote
// address and duration. It deliberately excludes query strings, headers and
// request bodies so credentials cannot be copied into error logs.
func SafeAccessLogMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		writer := &statusResponseWriter{ResponseWriter: w}
		next(writer, r)
		if writer.statusCode == 0 {
			writer.statusCode = http.StatusOK
		}
		const logFormat = "[HTTP] %d - %s %s - %s - %s"
		logger := logx.WithContext(r.Context())
		arguments := []any{
			writer.statusCode,
			r.Method,
			sanitizeAccessLogPath(r.URL.Path),
			httpx.GetRemoteAddr(r),
			time.Since(startedAt).Round(time.Microsecond),
		}
		if writer.statusCode >= http.StatusInternalServerError {
			logger.Errorf(logFormat, arguments...)
			return
		}
		logger.Infof(logFormat, arguments...)
	}
}

func sanitizeAccessLogPath(path string) string {
	const subscriptionPrefix = "/api/v1/client/subscription/"
	if strings.HasPrefix(path, subscriptionPrefix) {
		return redactedSubscriptionPath
	}
	if path == "" {
		return "/"
	}
	return path
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Write(payload []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *statusResponseWriter) Flush() {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	connection, readWriter, err := hijacker.Hijack()
	if err == nil {
		w.statusCode = http.StatusSwitchingProtocols
	}
	return connection, readWriter, err
}

func (w *statusResponseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (w *statusResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
