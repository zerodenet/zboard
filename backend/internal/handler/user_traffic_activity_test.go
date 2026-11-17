package handler

import (
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminUserTrafficActivityHTTP(t *testing.T) {
	f := newOrderFixture(t)
	call := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.AdminUserTrafficActivityHandler(w, announcementRequest("GET", "/api/v1/admin/traffic/user-activity?"+query, f.token, ""))
		return w
	}
	for _, query := range []string{"", "user_ids=0", "user_ids=bad", "user_ids=1,-2", "user_ids=" + strings.Repeat("1,", 100) + "1"} {
		if w := call(query); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := call("user_ids=1,2,1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data metering.UserActivityData `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	out := response.Data
	today := systemDateAt(time.Now(), f.h.systemTimezoneLocation())
	if len(out.Items) != 2 || len(out.Items[0].Days) != 7 || out.From != today.AddDate(0, 0, -6).Format("2006-01-02") || out.To != today.Format("2006-01-02") || out.Items[0].UsedBytes != 0 {
		t.Fatal(out)
	}
	if !strings.Contains(w.Body.String(), `"used_bytes":"0"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String(), w.Header())
	}
	// Incomplete projections are explicit, never a fabricated zero total.
	at := time.Now().UTC().Truncate(time.Hour)
	row := model.TrafficRecord{UserID: 1, NodeID: 1, At: at, UsedBytes: 1, ReportID: "http-pending-activity", Nonce: "http-pending-activity"}
	if err := f.h.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.MeteringUsageProjectionPending{TrafficRecordID: row.ID, UserID: 1, NodeID: 1, RecordAt: &at, HourlyPending: true, DeltaUsedBytes: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if w := call("user_ids=1"); w.Code != 200 || !strings.Contains(w.Body.String(), `"projection_pending":true`) || !strings.Contains(w.Body.String(), `"items":[]`) || w.Header().Get("X-Zboard-Statistics-State") != "pending" {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if w := call("user_ids=1"); w.Code != 403 {
		t.Fatal(fmt.Sprint(w.Code), w.Body.String())
	}
}
