package handler

import (
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntegrationHTTPCredentialAndInvocationLifecycle(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	create := httptest.NewRecorder()
	body := fmt.Sprintf(`{"name":"http client","scopes":["metering.usage.query"],"expires_at":%q}`, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	f.h.IntegrationCredentialCreateHandler(create, announcementRequest(http.MethodPost, "/api/v1/account/integrations/credentials", f.token, body))
	if create.Code != 200 {
		t.Fatal(create.Body.String())
	}
	var issued struct{ Data identity.IssuedIntegration }
	if err := json.Unmarshal(create.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.Data.Token == "" || create.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing one-time credential")
	}
	list := httptest.NewRecorder()
	f.h.IntegrationCredentialListHandler(list, announcementRequest(http.MethodGet, "/api/v1/account/integrations/credentials", f.token, ""))
	if list.Code != 200 || strings.Contains(list.Body.String(), issued.Data.Token) {
		t.Fatal("credential leaked in list")
	}
	external := func(token, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/capabilities/metering.usage.query/invoke", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		return pathvar.WithVars(req, map[string]string{"name": "metering.usage.query"})
	}
	query := `{"from":"2026-09-01T00:00:00Z","to":"2026-09-02T00:00:00Z","bucket":"hour","include_totals":true}`
	call := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(call, external(issued.Data.Token, query))
	if call.Code != 200 {
		t.Fatal(call.Body.String())
	}
	window := time.Now().UTC().Truncate(time.Minute)
	if err := f.h.db.Model(&model.UserAPIToken{}).Where("id = ?", issued.Data.Credential.ID).Updates(map[string]interface{}{"invocation_window_started_at": window, "invocation_window_count": 60}).Error; err != nil {
		t.Fatal(err)
	}
	limited := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(limited, external(issued.Data.Token, query))
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "60" || !strings.Contains(limited.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("rate limit: %d %s %s", limited.Code, limited.Header().Get("Retry-After"), limited.Body.String())
	}
	if err := f.h.db.Model(&model.UserAPIToken{}).Where("id = ?", issued.Data.Credential.ID).Update("invocation_window_count", 0).Error; err != nil {
		t.Fatal(err)
	}
	jwt := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(jwt, external(f.admin, query))
	if jwt.Code != 401 {
		t.Fatalf("login token accepted: %d", jwt.Code)
	}
	denied := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(denied, external(issued.Data.Token, strings.Replace(query, `"bucket":"hour"`, `"bucket":"hour","user_id":2`, 1)))
	if denied.Code != 401 {
		t.Fatalf("cross-account: %d", denied.Code)
	}
	oversized := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(oversized, external(issued.Data.Token, strings.Repeat(" ", 65537)))
	if oversized.Code != 400 {
		t.Fatalf("oversized: %d", oversized.Code)
	}
	revoke := httptest.NewRecorder()
	req := pathvar.WithVars(announcementRequest(http.MethodDelete, "/api/v1/account/integrations/credentials/1", f.token, ""), map[string]string{"id": fmt.Sprint(issued.Data.Credential.ID)})
	f.h.IntegrationCredentialRevokeHandler(revoke, req)
	if revoke.Code != 200 {
		t.Fatal(revoke.Body.String())
	}
	revoked := httptest.NewRecorder()
	f.h.IntegrationInvokeHandler(revoked, external(issued.Data.Token, query))
	if revoked.Code != 401 {
		t.Fatalf("revoked invocation: %d", revoked.Code)
	}
	directory := httptest.NewRecorder()
	f.h.IntegrationCapabilitiesHandler(directory, external(issued.Data.Token, ""))
	if directory.Code != 401 {
		t.Fatalf("revoked directory: %d", directory.Code)
	}
}
