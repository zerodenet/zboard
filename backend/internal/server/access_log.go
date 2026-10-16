package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/readtiming"
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
		ctx, timings := readtiming.NewContext(r.Context())
		writer := &statusResponseWriter{ResponseWriter: w, startedAt: startedAt, timings: timings}
		next(writer, r.WithContext(ctx))
		if writer.statusCode == 0 {
			writer.statusCode = http.StatusOK
		}
		const logFormat = "[HTTP] %d - %s %s - %s - %s - timings=%s"
		logger := logx.WithContext(r.Context())
		arguments := []any{
			writer.statusCode,
			r.Method,
			sanitizeAccessLogPath(r.URL.Path),
			httpx.GetRemoteAddr(r),
			time.Since(startedAt).Round(time.Microsecond),
			timings.Header(),
		}
		if writer.statusCode >= http.StatusInternalServerError {
			logger.Errorf(logFormat, arguments...)
			return
		}
		logger.Infof(logFormat, arguments...)
	}
}

func sanitizeAccessLogPath(path string) string {
	if strings.HasPrefix(path, "/api/v1/plugin-assets/") {
		return "/api/v1/plugin-assets/[redacted]"
	}
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
	startedAt  time.Time
	timings    *readtiming.Recorder
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	// Informational responses must not commit the final status or timings.
	if statusCode >= 100 && statusCode < 200 && statusCode != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}
	if w.timings != nil {
		if phases := w.timings.Header(); phases != "" {
			w.Header().Add("Server-Timing", phases)
		}
		w.Header().Add("Server-Timing", fmt.Sprintf("app_headers;dur=%.3f", float64(time.Since(w.startedAt))/float64(time.Millisecond)))
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
