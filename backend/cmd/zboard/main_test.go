package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	cfgpkg "github.com/zerodenet/zboard/backend/internal/config"
)

func TestStaticFallbackHandlerServesFrontendAndPreservesAPINotFound(t *testing.T) {
	webDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<div id=\"app\">zboard</div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("console.log('zboard')"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := staticFallbackHandler(webDir)
	tests := []struct {
		path       string
		statusCode int
		bodyPart   string
	}{
		{path: "/", statusCode: http.StatusOK, bodyPart: "zboard"},
		{path: "/tasks", statusCode: http.StatusOK, bodyPart: "zboard"},
		{path: "/assets/app.js", statusCode: http.StatusOK, bodyPart: "console.log"},
		{path: "/api/v1/missing", statusCode: http.StatusNotFound, bodyPart: "404 page not found"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != test.statusCode {
				t.Fatalf("status = %d, want %d", recorder.Code, test.statusCode)
			}
			if !strings.Contains(recorder.Body.String(), test.bodyPart) {
				t.Fatalf("body = %q, want substring %q", recorder.Body.String(), test.bodyPart)
			}
		})
	}
}

func TestBuildBootstrapAdminHashesPassword(t *testing.T) {
	config := cfgpkg.BootstrapAdmin{
		Email:    "operator@example.com",
		Password: "strong-admin-password",
	}
	admin, err := buildBootstrapAdmin(config, cfgpkg.EnvironmentProduction)
	if err != nil {
		t.Fatalf("buildBootstrapAdmin() error = %v", err)
	}
	if admin.Email != config.Email || !admin.IsAdmin || admin.Status != "active" {
		t.Fatalf("admin = %+v, want active configured administrator", admin)
	}
	if admin.Password == config.Password {
		t.Fatal("bootstrap password was stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(config.Password)); err != nil {
		t.Fatalf("stored bootstrap password hash does not match: %v", err)
	}
}

func TestBuildBootstrapAdminRejectsIncompleteConfig(t *testing.T) {
	_, err := buildBootstrapAdmin(cfgpkg.BootstrapAdmin{Email: "operator@example.com"}, cfgpkg.EnvironmentProduction)
	if err == nil {
		t.Fatal("buildBootstrapAdmin() error = nil, want incomplete config rejection")
	}
}
