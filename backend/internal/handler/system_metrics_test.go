package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSystemMetricsAdminOnlyAndRange(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	rec := httptest.NewRecorder()
	h.AdminSystemMetricsHandler(rec, announcementRequest("GET", "/api/v1/admin/system-metrics", token, ""))
	if rec.Code != 403 {
		t.Fatalf("nonadmin=%d", rec.Code)
	}
	if err := h.db.Model(&model.User{}).Where("id = 1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, _ = h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	for _, value := range []string{"60", "360", "720", "1440", "3", "-1", "oops"} {
		rec = httptest.NewRecorder()
		h.AdminSystemMetricsHandler(rec, announcementRequest("GET", "/api/v1/admin/system-metrics?range="+value, token, ""))
		want := 200
		if value == "3" || value == "-1" || value == "oops" {
			want = 400
		}
		if rec.Code != want {
			t.Fatalf("range %s: %d %s", value, rec.Code, rec.Body.String())
		}
		if want == 200 && (!strings.Contains(rec.Body.String(), `"range_minutes":`+value) || rec.Header().Get("Cache-Control") != "no-store") {
			t.Fatalf("cold start/cache: %s", rec.Body.String())
		}
	}
}
func TestRuntimeStatusErrorClassificationDoesNotLeakSQL(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{context.DeadlineExceeded, "timeout"}, {&mysql.MySQLError{Number: 1064, Message: "secret SQL here"}, "query_incompatible"}, {&mysql.MySQLError{Number: 1045, Message: "password=secret"}, "database_error"}, {errors.New("DSN password=secret"), "read_failed"}} {
		code, detail := runtimeStatusError(tc.err)
		if code != tc.code || strings.Contains(detail, "secret") {
			t.Fatalf("classification=%s/%s", code, detail)
		}
	}
}
