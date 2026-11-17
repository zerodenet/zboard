package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAccountCleanupStatusIsOwnerScopedAndCurrent(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	if err := h.db.Create(&model.SystemConfig{ConfigKey: identity.CleanupDaysKey, Value: "14", ValueType: "int"}).Error; err != nil {
		t.Fatal(err)
	}
	other := model.User{Email: "private-owner@example.test", Password: "hash", Status: "active", IsAdmin: true}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	call := func(token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.AccountCleanupStatusHandler(w, announcementRequest(http.MethodGet, "/api/v1/account/cleanup-status?user_id=999", token, ""))
		return w
	}
	w := call(token)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data identity.AccountCleanupStatus `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.Enabled || response.Data.State != "scheduled" || response.Data.EligibleAt == nil {
		t.Fatal(response)
	}
	if w := call(""); w.Code != 401 {
		t.Fatal("unauthenticated policy read", w.Code)
	}
	claims, err := h.authFromRequest(announcementRequest(http.MethodGet, "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id=?", claims.UserID).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	if w := call(token); w.Code != 401 {
		t.Fatal("suspended account could read", w.Code, w.Body.String())
	}
}
