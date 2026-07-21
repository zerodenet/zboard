package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
