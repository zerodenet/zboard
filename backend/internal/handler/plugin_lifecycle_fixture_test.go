package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"golang.org/x/crypto/bcrypt"
)

type pluginLifecycleFixture struct {
	h            *handlers
	manager      *plugins.Manager
	installation plugins.Installation
	adminToken   string
	memberToken  string
	server       *httptest.Server
}

func newPluginLifecycleFixture(t *testing.T, archive []byte, keys map[string]string) *pluginLifecycleFixture {
	t.Helper()
	h, _ := newAnnouncementTestHandlers(t)
	password, err := bcrypt.GenerateFromPassword([]byte("plugin-management-test-only"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"is_admin": true, "email": "admin@example.test", "account_name": "admin@example.test", "password": string(password)}).Error; err != nil {
		t.Fatal(err)
	}
	member := model.User{Email: "member@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.FirstOrCreate(&model.Installation{ID: 1, SiteName: "Plugin acceptance", InstalledAt: time.Now().UTC()}, model.Installation{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	admin, _, err := h.issueToken(authClaims{UserID: 1, Email: "admin@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	memberToken, _, err := h.issueToken(authClaims{UserID: member.ID, Email: member.Email})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(h.db, newTestCredentialCipher(t), plugins.Options{Directory: t.TempDir(), TrustedPublishers: keys}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	h.SetPluginManager(manager)
	installation, err := manager.Import(archive, "admin@example.test")
	if err != nil {
		t.Fatal(err)
	}
	f := &pluginLifecycleFixture{h: h, manager: manager, installation: installation, adminToken: admin, memberToken: memberToken}
	mux := http.NewServeMux()
	for path, handler := range map[string]http.HandlerFunc{
		"/api/v1/admin/plugins": h.AdminPluginsHandler,
		"/api/v1/auth/login":    h.LoginHandler, "/api/v1/auth/me": h.MeHandler,
		"/api/v1/setup/status": h.SetupStatusHandler, "/api/v1/system/status": h.SystemStatusHandler,
		"/api/v1/system/configs": h.PublicSystemConfigsHandler, "/api/v1/navigation": h.NavigationHandler,
		"/api/v1/plugin-ui/bridge": h.PluginBridgeHandler,
	} {
		mux.HandleFunc(path, handler)
	}
	mux.HandleFunc("/api/v1/admin/plugins/", h.PluginGuard(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/plugins/"), "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		r = pathvar.WithVars(r, map[string]string{"id": parts[0]})
		switch parts[1] {
		case "actions":
			if len(parts) != 3 {
				http.NotFound(w, r)
				return
			}
			h.AdminPluginActionHandler(w, pathvar.WithVars(r, map[string]string{"id": parts[0], "action": parts[2]}))
		case "operations":
			h.AdminPluginOperationsHandler(w, r)
		case "config":
			h.AdminPluginConfigHandler(w, r)
		case "test":
			h.AdminPluginTestHandler(w, r)
		case "migrations":
			h.AdminPluginMigrationsHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	mux.HandleFunc("/api/v1/plugin-ui/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/plugin-ui/"), "/")
		if len(parts) != 2 || parts[1] != "session" {
			http.NotFound(w, r)
			return
		}
		h.PluginSessionHandler(w, pathvar.WithVars(r, map[string]string{"id": parts[0]}))
	})
	mux.HandleFunc("/api/v1/plugin-assets/", h.PluginAssetHandler)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *pluginLifecycleFixture) request(t *testing.T, method, path, token, body string) (int, []byte) {
	t.Helper()
	r := announcementRequest(method, f.server.URL+path, token, body)
	r.RequestURI = ""
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var data json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}

// Opt-in only: the normal test suite never leaves a server running. The fixture
// uses a temporary SQLite database, signed generic config plugin, and local HTTP.
func TestPluginManagementBrowserFixture(t *testing.T) {
	ready := os.Getenv("ZBOARD_PLUGIN_BROWSER_READY_FILE")
	if ready == "" {
		t.Skip("opt-in local browser acceptance fixture")
	}
	archive, keys := configBridgePackage(t)
	f := newPluginLifecycleFixture(t, archive, keys)
	data, _ := json.Marshal(map[string]string{"url": f.server.URL, "plugin_id": f.installation.ID})
	if err := os.WriteFile(ready, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Browser fixture ready; seeded admin@example.test with test-only password")
	for {
		if _, err := os.Stat(ready + ".stop"); err == nil {
			return
		}
		select {
		case <-t.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
