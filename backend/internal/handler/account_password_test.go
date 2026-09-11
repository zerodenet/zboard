package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAccountPasswordConfirmationAndChange(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password-123"), bcrypt.MinCost)
	h.db.Model(&model.User{}).Where("id = ?", 1).Update("password", string(hash))
	call := func(body string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		fn(w, r)
		return w
	}
	if w := call(`{"password":"wrong"}`, h.ConfirmAccountPasswordHandler); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w := call(`{"password":"old-password-123"}`, h.ConfirmAccountPasswordHandler)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Data struct {
			Confirmation string `json:"confirmation"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	var user model.User
	h.db.First(&user, 1)
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-ZBoard-Account-Confirmation", response.Data.Confirmation)
	if !h.accountPasswordConfirmed(user, r) {
		t.Fatal("fresh confirmation rejected")
	}
	if err := h.db.Create(&model.ExternalIdentity{ID: "proof-binding", UserID: 1, PluginID: "test.oauth~github", Publisher: "official", Issuer: "https://github.com", Subject: "proof-user"}).Error; err != nil {
		t.Fatal(err)
	}
	session := plugins.Session{PluginID: "test.oauth", Surface: "account", Purpose: "slot", UserID: 1, TargetUserID: 1}
	if err := h.unlinkPluginIdentityConfirmed(session, authClaims{UserID: 1}, "proof-binding", "", r); err != nil {
		t.Fatalf("confirmed unlink without plaintext failed: %v", err)
	}
	r.Header.Set("Authorization", "Bearer other-session")
	if h.accountPasswordConfirmed(user, r) {
		t.Fatal("confirmation crossed login")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	expiry := strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10)
	r.Header.Set("X-ZBoard-Account-Confirmation", expiry+"."+h.accountConfirmationMAC(user, r.Header.Get("Authorization"), expiry))
	if h.accountPasswordConfirmed(user, r) {
		t.Fatal("expired confirmation accepted")
	}
	for _, body := range []string{`{"current_password":"wrong","password":"new-password-123"}`, `{"current_password":"old-password-123","password":"short"}`} {
		if w := call(body, h.ChangeAccountPasswordHandler); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if w := call(`{"current_password":"old-password-123","password":"new-password-123"}`, h.ChangeAccountPasswordHandler); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	h.db.First(&user, 1)
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("new-password-123")) != nil {
		t.Fatal("new password not stored")
	}
	r.Header.Set("X-ZBoard-Account-Confirmation", response.Data.Confirmation)
	if h.accountPasswordConfirmed(user, r) {
		t.Fatal("confirmation survived password change")
	}
	var count int64
	h.db.Model(&model.AuditLog{}).Where("action = ?", "account.password.change").Count(&count)
	if count != 1 {
		t.Fatal("missing audit")
	}
}
