package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/readtiming"
)

func TestDashboardViewsRemainAdministratorReadsAndOmitUnrelatedFields(t *testing.T) {
	f := newOrderFixture(t)
	for _, view := range []string{"current", "statistics", "trend", ""} {
		request := announcementRequest(http.MethodGet, "/api/v1/admin/dashboard/overview?view="+view+"&range=today", f.token, "")
		ctx, timing := readtiming.NewContext(request.Context())
		w := httptest.NewRecorder()
		f.h.DashboardOverviewSystemCalendarHandler(w, request.WithContext(ctx))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", view, w.Code, w.Body.String())
		}
		var response struct{ Data map[string]json.RawMessage }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"as_of"} {
			if response.Data[name] == nil {
				t.Fatalf("%s missing %s", view, name)
			}
		}
		switch view {
		case "current":
			if response.Data["business"] != nil || response.Data["trend"] != nil || response.Data["traffic"] != nil || response.Data["infrastructure"] == nil {
				t.Fatal(response.Data)
			}
			var service map[string]json.RawMessage
			json.Unmarshal(response.Data["service"], &service)
			if service["traffic_bytes"] != nil {
				t.Fatal("current pretends history is zero")
			}
		case "statistics":
			if response.Data["attention"] != nil || response.Data["trend"] != nil || response.Data["traffic"] == nil {
				t.Fatal(response.Data)
			}
		case "trend":
			if len(response.Data) != 5 || response.Data["trend"] == nil {
				t.Fatal(response.Data)
			}
		}
		if timing.Header() == "" {
			t.Fatal("missing operation timing")
		}
	}
	w := httptest.NewRecorder()
	f.h.DashboardOverviewSystemCalendarHandler(w, announcementRequest(http.MethodGet, "/?view=unknown", f.token, ""))
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	f.h.DashboardOverviewSystemCalendarHandler(w, announcementRequest(http.MethodGet, "/?view=current", "", ""))
	if w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	f.h.DashboardOverviewSystemCalendarHandler(w, announcementRequest(http.MethodGet, "/?view=current", f.token, ""))
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

type dashboardReadFailure struct {
	observability.DashboardRepository
	cause error
}

func (s dashboardReadFailure) LoadDashboardView(ctx context.Context, _ observability.DashboardPeriod, _ time.Time, _ []observability.DashboardTrendBucket, _ string) (observability.DashboardSnapshot, error) {
	if _, ok := ctx.Deadline(); !ok {
		return observability.DashboardSnapshot{}, errors.New("missing read budget")
	}
	return observability.DashboardSnapshot{}, s.cause
}
func TestDashboardReadTimeoutIsExplicitAndDoesNotExposeDatabaseErrors(t *testing.T) {
	f := newOrderFixture(t)
	original := f.h.services.Dashboard.Repository
	for _, test := range []struct {
		cause  error
		status int
		code   string
	}{{context.DeadlineExceeded, 504, "deadline_exceeded"}, {errors.New("SQL contains private-value"), 500, "unavailable"}} {
		f.h.services.Dashboard.Repository = dashboardReadFailure{original, test.cause}
		w := httptest.NewRecorder()
		f.h.DashboardOverviewSystemCalendarHandler(w, announcementRequest(http.MethodGet, "/?view=statistics", f.token, ""))
		if w.Code != test.status || strings.Contains(w.Body.String(), "private-value") {
			t.Fatal(w.Code, w.Body.String())
		}
		var body APIResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error == nil || body.Error.Code != test.code {
			t.Fatal(w.Body.String())
		}
	}
}
