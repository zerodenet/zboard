package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestLocalRegistrationHTTPPreservesSessionAndValidationContract(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "test", InstalledAt: time.Now().UTC(), AllowRegistration: true}).Error; err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.RegisterAuthRoutes(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body)))
		return w
	}
	body := `{"email":" New@Example.Test ","password":"ValidPassword2026!"}`
	w := call(body)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			User userPublic    `json:"user"`
			Auth tokenResponse `json:"auth"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.User.Email != "new@example.test" || response.Data.User.IsAdmin || response.Data.Auth.Token == "" {
		t.Fatal(response)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer "+response.Data.Auth.Token)
	claims, err := h.authFromRequest(r)
	if err != nil || claims.UserID != response.Data.User.ID {
		t.Fatal(claims, err)
	}
	if w := call(body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "该邮箱已存在") {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := h.db.Model(&model.Installation{}).Where("id = ?", 1).Update("allow_registration", false).Error; err != nil {
		t.Fatal(err)
	}
	if w := call(body); w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
}
