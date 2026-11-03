package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdminUserNotesArePrivateSearchableAndClearable(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	if err := h.db.Model(&model.User{}).Where("id=1").Updates(map[string]any{"is_admin": true, "admin_note": "PRIVATE-ADMIN-NOTE"}).Error; err != nil {
		t.Fatal(err)
	}
	target := model.User{Email: "noted@example.test", Status: "active", Password: "unchanged"}
	if err := h.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/admin/users/%d", target.ID)
	call := func(method, path, body, auth string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		fn(w, announcementRequest(method, path, auth, body))
		return w
	}
	for _, note := range []string{"  张三：研发团队  ", ""} {
		body, _ := json.Marshal(map[string]string{"admin_note": note})
		w := call(http.MethodPut, path, string(body), token, h.AdminUserUpdateHandler)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var current model.User
		if err := h.db.First(&current, target.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.AdminNote != strings.TrimSpace(note) || current.Status != "active" || current.Password != "unchanged" {
			t.Fatal(current)
		}
		w = call(http.MethodGet, path, "", token, h.AdminUserGetHandler)
		var detail struct {
			Data struct {
				Note string `json:"admin_note"`
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || w.Code != 200 || detail.Data.Note != strings.TrimSpace(note) {
			t.Fatal(w.Body.String(), err)
		}
		if note != "" {
			w = call(http.MethodGet, "/api/v1/admin/users?paged=true&q="+url.QueryEscape("研发团队"), "", token, h.AdminUsersListHandler)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "张三：研发团队") {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	w := call(http.MethodPut, path, `{"admin_note":"`+strings.Repeat("字", 501)+`"}`, token, h.AdminUserUpdateHandler)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "admin_note") {
		t.Fatal(w.Code, w.Body.String())
	}
	memberToken, _, err := h.issueToken(authClaims{UserID: target.ID, Email: target.Email})
	if err != nil {
		t.Fatal(err)
	}
	w = call(http.MethodPut, path, `{"admin_note":"unauthorized"}`, memberToken, h.AdminUserUpdateHandler)
	if w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(http.MethodGet, path, "", memberToken, h.AdminUserGetHandler)
	if w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(http.MethodGet, "/api/v1/auth/me", "", token, h.MeHandler)
	if w.Code != 200 || strings.Contains(w.Body.String(), "admin_note") || strings.Contains(w.Body.String(), "PRIVATE-ADMIN-NOTE") {
		t.Fatal(w.Code, w.Body.String())
	}
	var audit model.AuditLog
	if err := h.db.Where("action=? AND target=?", "user.update", fmt.Sprintf("user:%d", target.ID)).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Detail != "fields=admin_note" {
		t.Fatal(audit.Detail)
	}
}
