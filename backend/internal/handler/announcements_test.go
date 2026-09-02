package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func newAnnouncementTestHandlers(t *testing.T) (*handlers, string) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "announcements.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{Email: "reader@example.test", Password: "unused", Status: userStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	h, err := NewHandlers(db, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email})
	if err != nil {
		t.Fatal(err)
	}
	return h, token
}

func announcementRequest(method, target, token, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestAnnouncementReadReceiptTracksRevision(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	record := model.Announcement{Title: "Maintenance", Content: "**Details**", Severity: "warning", Audience: "user", Status: "published", Dismissible: false, Revision: 1}
	if err := h.db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	items, err := h.activeAnnouncements(announcementRequest(http.MethodGet, "/api/v1/announcements", token, ""))
	if err != nil || len(items) != 1 || items[0].Read {
		t.Fatalf("activeAnnouncements() = %+v, %v; want one unread item", items, err)
	}

	response := httptest.NewRecorder()
	h.AccountAnnouncementReadHandler(response, announcementRequest(http.MethodPost, "/api/v1/account/announcements/1/read", token, `{"revision":1}`))
	if response.Code != http.StatusOK {
		t.Fatalf("read status = %d body = %s", response.Code, response.Body.String())
	}
	items, err = h.activeAnnouncements(announcementRequest(http.MethodGet, "/api/v1/announcements", token, ""))
	if err != nil || !items[0].Read || items[0].ReadAt == nil {
		t.Fatalf("activeAnnouncements() after read = %+v, %v", items, err)
	}

	if err := h.db.Model(&record).Updates(map[string]interface{}{"revision": 2, "content": "Updated"}).Error; err != nil {
		t.Fatal(err)
	}
	items, err = h.activeAnnouncements(announcementRequest(http.MethodGet, "/api/v1/announcements", token, ""))
	if err != nil || items[0].Read {
		t.Fatalf("edited announcement should be unread again: %+v, %v", items, err)
	}
}

func TestAccountAnnouncementHistoryIncludesEndedAndArchivedNotices(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	past := time.Now().UTC().Add(-time.Hour)
	records := []model.Announcement{
		{Title: "Ended", Content: "ended", Severity: "info", Audience: "user", Status: "published", StartsAt: &past, EndsAt: &past, Revision: 1},
		{Title: "Archived", Content: "archived", Severity: "info", Audience: "all", Status: "archived", StartsAt: &past, Revision: 1},
		{Title: "Guest", Content: "guest", Severity: "info", Audience: "guest", Status: "published", StartsAt: &past, Revision: 1},
	}
	if err := h.db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	h.AccountAnnouncementsListHandler(response, announcementRequest(http.MethodGet, "/api/v1/account/announcements?offset=0&limit=20", token, ""))
	if response.Code != http.StatusOK {
		t.Fatalf("history status = %d body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"unread_count":2`) || !strings.Contains(body, `"title":"Ended"`) || !strings.Contains(body, `"title":"Archived"`) {
		t.Fatalf("history response missing published history: %s", body)
	}
	if strings.Contains(body, `"title":"Guest"`) {
		t.Fatalf("history response leaked guest-only announcement: %s", body)
	}
}
