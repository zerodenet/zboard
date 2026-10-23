package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
)

func TestDashboardDoesNotPromotePrincipalSnapshotsToCompleteCoverage(t *testing.T) {
	assertDashboardObservationCoverage(t, newOrderFixture(t))
}

func TestMySQLDashboardDoesNotPromotePrincipalSnapshotsToCompleteCoverage(t *testing.T) {
	assertDashboardObservationCoverage(t, newMySQLBusinessOrderFixture(t))
}

func assertDashboardObservationCoverage(t *testing.T, f orderFixture) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-30 * 24 * time.Hour)
	rows := []meteringstore.PrincipalFlowScopeCurrent{
		{ScopeType: meteringstore.ScopeSubscription, ScopeID: 4001, ActiveFlows: 3, UpdatedAt: now},
		{ScopeType: meteringstore.ScopeSubscription, ScopeID: 4002, ActiveFlows: 4, UpdatedAt: old},
	}
	if err := f.h.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// Compare database-decoded timestamps on both sides; drivers may use a
	// different internal time.Location than the seed's equivalent UTC value.
	var before []meteringstore.PrincipalFlowScopeCurrent
	if err := f.h.db.Order("scope_id").Find(&before).Error; err != nil || len(before) != 2 || !before[1].UpdatedAt.Equal(old) {
		t.Fatalf("observation fixture did not retain original event age: %+v %v", before, err)
	}
	read := func(want *float64, reason string) {
		t.Helper()
		for _, view := range []string{"current", ""} {
			w := httptest.NewRecorder()
			f.h.DashboardOverviewSystemCalendarHandler(w, announcementRequest(http.MethodGet, "/?view="+view, f.token, ""))
			if w.Code != http.StatusOK {
				t.Fatalf("dashboard %s: %d %s", view, w.Code, w.Body.String())
			}
			var response struct {
				Data struct {
					Service  map[string]any
					Coverage map[string]any
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			service, coverage := response.Data.Service, response.Data.Coverage
			if service["active_flows"] != nil || service["active_subscriptions"] != nil || coverage["principal_flows"] != false {
				t.Fatalf("partial/stale principal snapshots became complete current metrics: service=%+v coverage=%+v", service, coverage)
			}
			if coverage["principal_flows_state"] != "unknown" || coverage["principal_flows_reason"] != reason {
				t.Fatalf("unknown coverage lost explanation: %+v", coverage)
			}
			if want == nil {
				if service["observed_active_flows"] != nil || service["observed_active_subscriptions"] != nil {
					t.Fatalf("missing observations became zero: %+v", service)
				}
			} else if service["observed_active_flows"] != *want || service["observed_active_subscriptions"] != map[bool]float64{true: 2, false: 0}[*want > 0] {
				t.Fatalf("known snapshot aggregate lost: %+v", service)
			}
		}
	}
	seven := float64(7)
	read(&seven, "snapshot_coverage_unverified")
	var after []meteringstore.PrincipalFlowScopeCurrent
	if err := f.h.db.Order("scope_id").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("dashboard read changed observation source: %+v %v", after, err)
	}
	if err := f.h.db.Model(&meteringstore.PrincipalFlowScopeCurrent{}).Where("scope_type = ?", meteringstore.ScopeSubscription).Update("active_flows", 0).Error; err != nil {
		t.Fatal(err)
	}
	zero := float64(0)
	read(&zero, "snapshot_coverage_unverified")
	if err := f.h.db.Where("scope_type = ?", meteringstore.ScopeSubscription).Delete(&meteringstore.PrincipalFlowScopeCurrent{}).Error; err != nil {
		t.Fatal(err)
	}
	read(nil, "no_principal_observations")
}
