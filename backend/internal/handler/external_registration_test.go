package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func registrationIdentity(t *testing.T, verified bool) (*handlers, *fakeIdentityRuntime) {
	h, _, runtime := identityTestHandlers(t)
	h.db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", true)
	runtime.identity = &pluginv1.VerifiedIdentity{Issuer: runtime.snapshot.Provider.Issuer, Subject: "new-identity", Email: "new@example.com", EmailVerified: verified}
	return h, runtime
}
func identityFinishRequest(h *handlers, previous *httptest.ResponseRecorder, body string) *httptest.ResponseRecorder {
	r := announcementRequest("POST", "https://panel.example.test/api/v1/auth/oidc/finish", "", body)
	for _, c := range previous.Result().Cookies() {
		if c.Name == "__Host-"+resultCookie && c.Value != "" {
			r.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	h.ExternalAuthFinishHandler(w, r)
	return w
}
func newIdentityCallback(t *testing.T, h *handlers) *httptest.ResponseRecorder {
	cookie, q := startIdentityTest(t, h, "", false)
	return callbackIdentityTest(h, cookie, q)
}
func TestExternalIdentityRegistersInCoreAndAllowsRecentPasswordSetup(t *testing.T) {
	h, _ := registrationIdentity(t, true)
	cb := newIdentityCallback(t, h)
	var count int64
	h.db.Model(&model.User{}).Count(&count)
	if count != 1 {
		t.Fatal("callback created account before core completion")
	}
	finish := finishIdentityTest(h, cb)
	var result struct {
		Data struct {
			User userPublic    `json:"user"`
			Auth tokenResponse `json:"auth"`
		} `json:"data"`
	}
	if json.Unmarshal(finish.Body.Bytes(), &result) != nil || finish.Code != 200 || result.Data.Auth.Token == "" || result.Data.User.IsAdmin {
		t.Fatalf("registration failed %s", finish.Body)
	}
	var user model.User
	h.db.First(&user, result.Data.User.ID)
	if user.Password != "!external" || user.EmailVerifiedAt == nil || user.Email != "new@example.com" {
		t.Fatal("incorrect core account defaults")
	}
	setup := func(cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := announcementRequest("POST", "https://panel.example.test/api/v1/auth/oidc/password", result.Data.Auth.Token, `{"password":"my-local-password-123"}`)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ExternalPasswordSetupHandler(w, r)
		return w
	}
	if setup(nil).Code != 401 {
		t.Fatal("password setup without recent identity proof accepted")
	}
	if w := setup(finish.Result().Cookies()); w.Code != 200 {
		t.Fatalf("password setup failed %s", w.Body)
	}
	if setup(finish.Result().Cookies()).Code != 401 {
		t.Fatal("password setup proof replayed")
	}
	h.db.First(&user, user.ID)
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("my-local-password-123")) != nil {
		t.Fatal("core password missing")
	}
	cb = newIdentityCallback(t, h)
	again := finishIdentityTest(h, cb)
	if again.Code != 200 {
		t.Fatal("registered identity cannot log in")
	}
	h.db.Model(&model.User{}).Count(&count)
	if count != 2 {
		t.Fatal("returning login created duplicate user")
	}
}
func TestExternalIdentityRegistrationRechecksCorePolicyAndDoesNotMergeEmail(t *testing.T) {
	for _, mode := range []string{"registration-disabled", "email-collision", "provider-disabled", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			h, runtime := registrationIdentity(t, true)
			cb := newIdentityCallback(t, h)
			switch mode {
			case "registration-disabled":
				h.db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", false)
			case "email-collision":
				var user model.User
				h.db.First(&user, 1)
				runtime.identity.Email = user.Email
				// The assertion object is already captured; use a fresh authorization.
				cb = newIdentityCallback(t, h)
			case "provider-disabled":
				runtime.disabled = true
			case "audit-failure":
				h.db.Migrator().DropTable(&model.AuditLog{})
			}
			if finishIdentityTest(h, cb).Code == 200 {
				t.Fatal("invalid registration succeeded")
			}
			var users, bindings int64
			h.db.Model(&model.User{}).Count(&users)
			h.db.Model(&model.ExternalIdentity{}).Count(&bindings)
			if users != 1 || bindings != 0 {
				t.Fatal("failed registration left account or binding")
			}
		})
	}
}
func TestExternalIdentityRequiresCoreEmailVerificationAndConsumesChallenge(t *testing.T) {
	h, _ := registrationIdentity(t, false)
	cb := newIdentityCallback(t, h)
	pending := finishIdentityTest(h, cb)
	if pending.Code != 200 || !strings.Contains(pending.Body.String(), `"registration_required":true`) {
		t.Fatalf("missing completion prompt %s", pending.Body)
	}
	now := time.Now()
	challenge := model.RegistrationEmailChallenge{Email: "chosen@example.com", Purpose: registrationChallengePurpose, CodeHash: h.registrationCodeDigest("chosen@example.com", "123456"), ExpiresAt: now.Add(time.Minute), LastSentAt: now}
	if err := h.db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	finished := identityFinishRequest(h, pending, `{"email":"chosen@example.com","verification_code":"123456"}`)
	if finished.Code != 200 || !strings.Contains(finished.Body.String(), `"token"`) {
		t.Fatalf("verified registration failed %s", finished.Body)
	}
	var user model.User
	h.db.Where("email = ?", "chosen@example.com").First(&user)
	if user.EmailVerifiedAt == nil || user.IsAdmin {
		t.Fatal("missing verification or elevated role")
	}
	h.db.First(&challenge, challenge.ID)
	if challenge.ConsumedAt == nil {
		t.Fatal("verification challenge not consumed")
	}
	if identityFinishRequest(h, pending, `{"email":"chosen@example.com","verification_code":"123456"}`).Code != 401 {
		t.Fatal("registration ticket replayed")
	}
}
func TestExternalIdentityEmailAttemptBudgetSurvivesFailedRegistration(t *testing.T) {
	h, _ := registrationIdentity(t, false)
	challenge := model.RegistrationEmailChallenge{Email: "new@example.com", Purpose: registrationChallengePurpose, CodeHash: h.registrationCodeDigest("new@example.com", "123456"), ExpiresAt: time.Now().Add(time.Minute)}
	h.db.Create(&challenge)
	for i := 0; i < 6; i++ {
		cb := newIdentityCallback(t, h)
		pending := finishIdentityTest(h, cb)
		if identityFinishRequest(h, pending, `{"email":"new@example.com","verification_code":"000000"}`).Code == 200 {
			t.Fatal("wrong code accepted")
		}
	}
	h.db.First(&challenge, challenge.ID)
	if challenge.Attempts != registrationCodeMaxAttempts {
		t.Fatalf("attempt budget lost: %d", challenge.Attempts)
	}
	var users int64
	h.db.Model(&model.User{}).Count(&users)
	if users != 1 {
		t.Fatal("wrong code created user")
	}
}
func TestExternalIdentityProviderKeysSeparateSameIssuerSubjects(t *testing.T) {
	h, _, runtime := identityTestHandlers(t)
	identity := &pluginv1.VerifiedIdentity{Issuer: runtime.snapshot.Provider.Issuer, Subject: "same"}
	runtime.snapshot.Provider.ProviderId = "one"
	a := externalIdentityID(runtime.snapshot, identity)
	runtime.snapshot.Provider.ProviderId = "two"
	b := externalIdentityID(runtime.snapshot, identity)
	if a == b {
		t.Fatal("provider selector absent from identity namespace")
	}
	_ = h
}
