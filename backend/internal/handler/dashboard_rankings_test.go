package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDashboardRankingsRequiresCurrentAdministratorAndUsesSystemTimezone(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Create(&model.TrafficRecord{UserID: 1, NodeID: 1, ReportID: "dashboard-rank", Nonce: "dashboard-rank", UsedBytes: 500, At: time.Now().UTC().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	request := func(path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.DashboardTrafficRankingsHandler(w, announcementRequest(http.MethodGet, path, token, ""))
		return w
	}
	w := request("/api/v1/admin/dashboard/traffic-rankings?range=7d&user_id=999", f.token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data observability.DashboardTrafficRankings
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Users) != 1 || response.Data.Users[0].ID != 1 || response.Data.Users[0].TrafficBytes != 500 {
		t.Fatalf("rankings %+v", response.Data)
	}
	if response.Data.Period.Timezone != f.h.systemTimezoneLocation().String() {
		t.Fatalf("timezone %s", response.Data.Period.Timezone)
	}
	w = request("/api/v1/admin/dashboard/traffic-rankings?range=7d&dimension=users", f.token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Nodes) != 0 || len(response.Data.Users) != 1 {
		t.Fatalf("selected dimension %+v", response.Data)
	}
	if w := request("/api/v1/admin/dashboard/traffic-rankings?dimension=invalid", f.token); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request("/api/v1/admin/dashboard/traffic-rankings?range=year", f.token); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request("/api/v1/admin/dashboard/traffic-rankings", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if err := f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if w := request("/api/v1/admin/dashboard/traffic-rankings", f.token); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
