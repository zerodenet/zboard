package server

import (
	"bufio"
	"bytes"
	"context"
	"github.com/zerodenet/zboard/backend/internal/readtiming"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

func TestConfigureSafeHTTPLoggingDisablesNativeRequestDump(t *testing.T) {
	config := rest.RestConf{Middlewares: rest.MiddlewaresConf{Log: true}}
	ConfigureSafeHTTPLogging(&config)
	if config.Middlewares.Log {
		t.Fatal("native request logger is enabled")
	}
}

func TestSanitizeAccessLogPath(t *testing.T) {
	tests := map[string]string{
		"/api/v1/plugin-assets/secret-token/ui/index.html": "/api/v1/plugin-assets/[redacted]",
		"":                       "/",
		"/api/v1/admin/tasks/42": "/api/v1/admin/tasks/42",
		"/api/v1/client/subscription/secret-token":    redactedSubscriptionPath,
		"/api/v1/client/subscription/another/segment": redactedSubscriptionPath,
	}
	for input, want := range tests {
		if got := sanitizeAccessLogPath(input); got != want {
			t.Fatalf("sanitizeAccessLogPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeAccessLogMiddlewarePreservesStatusAndResponse(t *testing.T) {
	var logOutput bytes.Buffer
	previousWriter := logx.Reset()
	logx.SetWriter(logx.NewWriter(&logOutput))
	t.Cleanup(func() {
		logx.SetWriter(previousWriter)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tasks?token=query-secret", strings.NewReader(`{"password":"body-secret"}`))
	request.Header.Set("Authorization", "Bearer header-secret")

	middleware := SafeAccessLogMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	})
	middleware(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() != "failed\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if request.Header.Get("Authorization") != "Bearer header-secret" {
		t.Fatal("middleware mutated the request authorization header")
	}
	logged := logOutput.String()
	for _, secret := range []string{"header-secret", "query-secret", "body-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("access log contains secret %q: %s", secret, logged)
		}
	}
	for _, expected := range []string{"500", http.MethodPost, "/api/v1/admin/tasks"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("access log does not contain %q: %s", expected, logged)
		}
	}
	if !strings.Contains(logged, `"level":"error"`) {
		t.Fatalf("5xx access log is not error-level: %s", logged)
	}
}

func TestSafeTimingHeadersDoNotBufferStreamingAndPreserveExistingTiming(t *testing.T) {
	recorder := httptest.NewRecorder()
	SafeAccessLogMiddleware(func(w http.ResponseWriter, r *http.Request) {
		finish := readtiming.Start(r.Context(), "current_state")
		finish()
		w.Header().Add("Server-Timing", "existing;dur=1")
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		if recorder.Body.String() != "first" || !recorder.Flushed {
			t.Fatal("stream was buffered")
		}
		_, _ = w.Write([]byte("second"))
	})(recorder, httptest.NewRequest(http.MethodGet, "/?secret=hidden", nil))
	header := strings.Join(recorder.Result().Header.Values("Server-Timing"), ", ")
	for _, want := range []string{"current_state;dur=", "app_headers;dur=", "existing;dur=1"} {
		if !strings.Contains(header, want) {
			t.Fatal(header)
		}
	}
	if recorder.Body.String() != "firstsecond" {
		t.Fatal(recorder.Body.String())
	}
}

func TestSafeResponseWriterPreservesUpgradeAndUnwrap(t *testing.T) {
	underlying := &upgradeResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	_, recorder := readtiming.NewContext(context.Background())
	writer := &statusResponseWriter{ResponseWriter: underlying, timings: recorder, startedAt: time.Now()}
	if writer.Unwrap() != underlying {
		t.Fatal("lost response controller access")
	}
	connection, _, err := writer.Hijack()
	if err != nil || connection == nil || writer.statusCode != 101 {
		t.Fatal(err)
	}
	connection.Close()
	underlying.other.Close()
	if err := writer.Push("/test", nil); err != http.ErrNotSupported {
		t.Fatal(err)
	}
}

type upgradeResponseWriter struct {
	*httptest.ResponseRecorder
	other net.Conn
}

func (w *upgradeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, other := net.Pipe()
	w.other = other
	return connection, bufio.NewReadWriter(bufio.NewReader(connection), bufio.NewWriter(connection)), nil
}
