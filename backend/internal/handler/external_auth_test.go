package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type fakeIdentityRuntime struct {
	db        *gorm.DB
	snapshot  plugins.IdentitySnapshot
	disabled  bool
	exchanges int
}

func (f *fakeIdentityRuntime) IdentityProviders() ([]plugins.IdentityProviderView, error) {
	return []plugins.IdentityProviderView{{ID: f.snapshot.ID, Name: "Test login"}}, nil
}
func (f *fakeIdentityRuntime) IdentityProvider(context.Context, string) (plugins.IdentitySnapshot, error) {
	return f.snapshot, nil
}
func (f *fakeIdentityRuntime) WithIdentityProvider(s plugins.IdentitySnapshot, commit func(*gorm.DB) error) error {
	if f.disabled || s.Revision != f.snapshot.Revision {
		return errors.New("revoked")
	}
	return f.db.Transaction(commit)
}
func (f *fakeIdentityRuntime) ExchangeIdentity(_ context.Context, s plugins.IdentitySnapshot, r *pluginv1.IdentityExchange, commit func(*pluginv1.VerifiedIdentity, *gorm.DB) error) error {
	f.exchanges++
	if len(r.Nonce) != 43 || len(r.PkceVerifier) != 43 || r.RedirectUri != "https://panel.example.test/api/v1/auth/oidc/callback" {
		return errors.New("missing core authentication constraints")
	}
	return f.WithIdentityProvider(s, func(tx *gorm.DB) error {
		return commit(&pluginv1.VerifiedIdentity{Issuer: r.Issuer, Subject: "subject-123"}, tx)
	})
}
func identityTestHandlers(t *testing.T) (*handlers, string, *fakeIdentityRuntime) {
	h, token := newAnnouncementTestHandlers(t)
	if err := h.db.Create(&model.Installation{ID: 1, SiteURL: "https://panel.example.test", AllowRegistration: false}).Error; err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("account-password"), bcrypt.MinCost)
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("password", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	runtime := &fakeIdentityRuntime{db: h.db, snapshot: plugins.IdentitySnapshot{ID: "test.oauth", Publisher: "trusted", Generation: 1, Revision: 1, Provider: &pluginv1.IdentityProvider{Issuer: "https://id.example.test", AuthorizationEndpoint: "https://id.example.test/auth", ClientId: "client", Scopes: []string{"openid"}}}}
	h.identityProviders = runtime
	return h, token, runtime
}
func startIdentityTest(t *testing.T, h *handlers, token string, bind bool) (*http.Cookie, url.Values) {
	t.Helper()
	body := "{}"
	if bind {
		body = `{"password":"account-password"}`
	}
	req := pathvar.WithVars(announcementRequest("POST", "https://panel.example.test/start", token, body), map[string]string{"id": "test.oauth"})
	req.Header.Set("Origin", "https://panel.example.test")
	response := httptest.NewRecorder()
	if bind {
		h.ExternalIdentityBindHandler(response, req)
	} else {
		h.ExternalAuthStartHandler(response, req)
	}
	if response.Code != 200 {
		t.Fatalf("start: %d %s", response.Code, response.Body)
	}
	var result struct {
		Data struct {
			URL string `json:"authorization_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(result.Data.URL)
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 || q.Get("redirect_uri") != "https://panel.example.test/api/v1/auth/oidc/callback" {
		t.Fatal("core authorization URL lacks binding")
	}
	for _, c := range response.Result().Cookies() {
		if c.Name == "__Host-"+flowCookie {
			if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Fatal("unsafe cookie")
			}
			return c, q
		}
	}
	t.Fatal("flow cookie missing")
	return nil, nil
}
func callbackIdentityTest(h *handlers, cookie *http.Cookie, q url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "https://panel.example.test/api/v1/auth/oidc/callback?state="+q.Get("state")+"&code=code", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ExternalAuthCallbackHandler(response, req)
	return response
}
func finishIdentityTest(h *handlers, callback *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	req := announcementRequest("POST", "https://panel.example.test/api/v1/auth/oidc/finish", "", "{}")
	req.Header.Del("Authorization")
	for _, c := range callback.Result().Cookies() {
		if c.Name == "__Host-"+resultCookie {
			req.AddCookie(c)
		}
	}
	response := httptest.NewRecorder()
	h.ExternalAuthFinishHandler(response, req)
	return response
}
func TestExternalIdentityRequiresLinkThenCoreIssuesSession(t *testing.T) {
	h, token, runtime := identityTestHandlers(t)
	cookie, q := startIdentityTest(t, h, "", false)
	failed := callbackIdentityTest(h, cookie, q)
	if !strings.Contains(failed.Header().Get("Location"), "error=failed") {
		t.Fatal("unlinked identity logged in")
	}
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	if count != 1 {
		t.Fatal("plugin created user")
	}
	cookie, q = startIdentityTest(t, h, token, true)
	callback := callbackIdentityTest(h, cookie, q)
	if strings.Contains(callback.Header().Get("Location"), "error=") {
		t.Fatal("binding failed")
	}
	finished := finishIdentityTest(h, callback)
	if finished.Code != 200 || !strings.Contains(finished.Body.String(), `"linked":true`) || strings.Contains(finished.Body.String(), `"token"`) {
		t.Fatalf("binding should not mint session: %s", finished.Body)
	}
	cookie, q = startIdentityTest(t, h, "", false)
	callback = callbackIdentityTest(h, cookie, q)
	if strings.Contains(callback.Header().Get("Location"), "token") || strings.Contains(callback.Header().Get("Location"), "code=") {
		t.Fatal("credential in redirect")
	}
	finished = finishIdentityTest(h, callback)
	var login struct {
		Data struct {
			User userPublic    `json:"user"`
			Auth tokenResponse `json:"auth"`
		} `json:"data"`
	}
	if json.Unmarshal(finished.Body.Bytes(), &login) != nil || finished.Code != 200 || login.Data.User.ID != 1 || login.Data.User.IsAdmin || login.Data.Auth.Token == "" {
		t.Fatalf("core login failed: %s", finished.Body)
	}
	me := httptest.NewRecorder()
	h.MeHandler(me, announcementRequest("GET", "/auth/me", login.Data.Auth.Token, ""))
	if me.Code != 200 {
		t.Fatal("issued token not accepted by core")
	}
	if finishIdentityTest(h, callback).Code != 401 {
		t.Fatal("result ticket replayed")
	}
	before := runtime.exchanges
	callbackIdentityTest(h, cookie, q)
	if runtime.exchanges != before {
		t.Fatal("callback replay reached plugin")
	}
}
func TestExternalIdentityRejectsCrossBrowserAndChangedPlugin(t *testing.T) {
	h, token, runtime := identityTestHandlers(t)
	cookie, q := startIdentityTest(t, h, token, true)
	wrong := *cookie
	wrong.Value = strings.Repeat("x", 43)
	if !strings.Contains(callbackIdentityTest(h, &wrong, q).Header().Get("Location"), "error=failed") || runtime.exchanges != 0 {
		t.Fatal("cross-browser callback accepted")
	}
	runtime.snapshot.Revision++
	if !strings.Contains(callbackIdentityTest(h, cookie, q).Header().Get("Location"), "error=failed") {
		t.Fatal("changed configuration accepted")
	}
	var n int64
	h.db.Model(&model.ExternalIdentity{}).Count(&n)
	if n != 0 {
		t.Fatal("revoked plugin committed identity")
	}
}
func TestExternalIdentityRechecksAccountAndBindingAtFinish(t *testing.T) {
	h, token, _ := identityTestHandlers(t)
	cookie, q := startIdentityTest(t, h, token, true)
	callback := callbackIdentityTest(h, cookie, q)
	if finishIdentityTest(h, callback).Code != 200 {
		t.Fatal("bind failed")
	}
	cookie, q = startIdentityTest(t, h, "", false)
	callback = callbackIdentityTest(h, cookie, q)
	h.db.Model(&model.User{}).Where("id = ?", 1).Update("status", userStatusSuspended)
	if finishIdentityTest(h, callback).Code != 401 {
		t.Fatal("suspended account got session")
	}
}
func TestExternalIdentityPasswordAndOriginChecks(t *testing.T) {
	h, token, _ := identityTestHandlers(t)
	for _, tc := range []struct {
		origin, body string
		want         int
	}{{"https://evil.example", `{}`, 403}, {"https://panel.example.test", `{"password":"wrong"}`, 401}} {
		req := pathvar.WithVars(announcementRequest("POST", "/bind", token, tc.body), map[string]string{"id": "test.oauth"})
		req.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		h.ExternalIdentityBindHandler(response, req)
		if response.Code != tc.want {
			t.Fatalf("got %d want %d", response.Code, tc.want)
		}
	}
	h.SetPluginManager(nil)
	response := httptest.NewRecorder()
	h.ExternalAuthProvidersHandler(response, httptest.NewRequest("GET", "/providers", nil))
	if response.Code != 200 {
		t.Fatal("optional plugin outage broke provider catalog")
	}
}
func TestExternalIdentityExpiryAndCaseSensitiveNamespace(t *testing.T) {
	var state externalAuthState
	key, err := state.add(externalAuthFlow{Binding: strings.Repeat("b", 43)}, "")
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	flow := state.flows[key]
	flow.Expires = time.Now().Add(-time.Second)
	state.flows[key] = flow
	state.mu.Unlock()
	if _, err := state.take(key, strings.Repeat("b", 43)); err == nil {
		t.Fatal("expired state accepted")
	}
	p := plugins.IdentitySnapshot{ID: "p.id", Publisher: "publisher"}
	a := externalIdentityID(p, &pluginv1.VerifiedIdentity{Issuer: "https://id.test", Subject: "A"})
	b := externalIdentityID(p, &pluginv1.VerifiedIdentity{Issuer: "https://id.test", Subject: "a"})
	p.Publisher = "other"
	c := externalIdentityID(p, &pluginv1.VerifiedIdentity{Issuer: "https://id.test", Subject: "A"})
	if a == b || a == c {
		t.Fatal("identity namespace folded case or publisher")
	}
}

func TestExternalIdentityCannotBeStolenAndUnlinkInvalidatesPendingLogin(t *testing.T) {
	h, token, _ := identityTestHandlers(t)
	cookie, q := startIdentityTest(t, h, token, true)
	if finishIdentityTest(h, callbackIdentityTest(h, cookie, q)).Code != 200 {
		t.Fatal("binding failed")
	}
	var row model.ExternalIdentity
	if err := h.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("account-password"), bcrypt.MinCost)
	other := model.User{Email: "other@example.test", Password: string(hash), Status: userStatusActive}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherToken, _, _ := h.issueToken(authClaims{UserID: other.ID, Email: other.Email})
	cookie, q = startIdentityTest(t, h, otherToken, true)
	if !strings.Contains(callbackIdentityTest(h, cookie, q).Header().Get("Location"), "error=failed") {
		t.Fatal("identity transferred to another user")
	}
	for _, tc := range []struct {
		token, body string
		want        int
	}{{otherToken, `{"password":"account-password"}`, 400}, {token, `{"password":"wrong"}`, 401}} {
		r := pathvar.WithVars(announcementRequest("POST", "/unlink", tc.token, tc.body), map[string]string{"id": row.ID})
		w := httptest.NewRecorder()
		h.ExternalIdentityUnlinkHandler(w, r)
		if w.Code != tc.want {
			t.Fatal("foreign binding or wrong password accepted", w.Code)
		}
	}
	cookie, q = startIdentityTest(t, h, "", false)
	pending := callbackIdentityTest(h, cookie, q)
	r := pathvar.WithVars(announcementRequest("POST", "/unlink", token, `{"password":"account-password"}`), map[string]string{"id": row.ID})
	w := httptest.NewRecorder()
	h.ExternalIdentityUnlinkHandler(w, r)
	if w.Code != 200 || finishIdentityTest(h, pending).Code != 401 {
		t.Fatal("unlink did not invalidate pending login")
	}
}
func TestExternalIdentityPasswordChangeAndAuditFailureCancelBinding(t *testing.T) {
	h, token, _ := identityTestHandlers(t)
	cookie, q := startIdentityTest(t, h, token, true)
	original := model.User{}
	h.db.First(&original, 1)
	h.db.Model(&model.User{}).Where("id = ?", 1).Update("password", "changed")
	if !strings.Contains(callbackIdentityTest(h, cookie, q).Header().Get("Location"), "error=failed") {
		t.Fatal("password confirmation survived password change")
	}
	h.db.Model(&model.User{}).Where("id = ?", 1).Update("password", original.Password)
	if err := h.db.Exec(`CREATE TRIGGER fail_identity_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	cookie, q = startIdentityTest(t, h, token, true)
	if !strings.Contains(callbackIdentityTest(h, cookie, q).Header().Get("Location"), "error=failed") {
		t.Fatal("audit failure was ignored")
	}
	var count int64
	h.db.Model(&model.ExternalIdentity{}).Count(&count)
	if count != 0 {
		t.Fatal("binding committed despite failed audit")
	}
}
func TestExternalIdentityCallbackRespectsDatabaseMigrationWriteLock(t *testing.T) {
	h, _, _ := identityTestHandlers(t)
	h.maintenanceMu.Lock()
	h.maintenanceState = maintenanceState{MigrationInProgress: true}
	h.maintenanceLoadedAt = time.Now()
	h.maintenanceMu.Unlock()
	called := false
	w := httptest.NewRecorder()
	h.InstallationMiddleware(func(http.ResponseWriter, *http.Request) { called = true })(w, httptest.NewRequest("GET", externalAuthPath+"/callback?state=sensitive", nil))
	if called || w.Code != http.StatusServiceUnavailable {
		t.Fatalf("callback bypassed migration lock: %d", w.Code)
	}
}
