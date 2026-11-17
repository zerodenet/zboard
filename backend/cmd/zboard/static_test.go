package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendDocumentsAndMissingAssetsAreNeverCached(t *testing.T) {
	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "assets", "0.0.3-dev.202610160859"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.html":         "<html>current frontend</html>",
		"assets/app-123.js":  "export const current = true",
		"assets/app-123.css": "body { color: black }",
		"assets/0.0.3-dev.202610160859/app-123.js":  "export const current = true",
		"assets/0.0.3-dev.202610160859/app-123.css": "body { color: black }",
	} {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := staticFallbackHandler(webDir)
	for _, target := range []string{"/", "/index.html", "/admin/nodes?tab=protocols", "/admin/nodes/"} {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("document may be cached: %v", recorder.Header())
			}
			if request.URL.Path != strings.Split(target, "?")[0] {
				t.Fatal("handler mutated the original request URL")
			}
		})
	}
	for _, target := range []string{"/assets/missing.js", "/assets/missing.css", "/assets/0.0.3-dev.202610160859/missing.css", "/assets/", "/missing.css", "/api/v1/absent", "/media/absent.png"} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusNotFound || recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(recorder.Body.String(), "current frontend") {
				t.Fatalf("missing resource returned cacheable content or SPA document: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
		})
	}
	for target, contentType := range map[string]string{"/assets/app-123.js": "javascript", "/assets/app-123.css": "text/css", "/assets/0.0.3-dev.202610160859/app-123.js": "javascript", "/assets/0.0.3-dev.202610160859/app-123.css": "text/css"} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), contentType) || recorder.Header().Get("Cache-Control") != "public, max-age=14400" {
				t.Fatalf("valid asset not served with its MIME/cache policy: status=%d headers=%v", recorder.Code, recorder.Header())
			}
		})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/admin/nodes", nil))
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD document: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
