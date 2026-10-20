package commerce

import (
	"math"
	"testing"
)

func TestPlanQuotaSynchronizationPreservesAccountingBaseline(t *testing.T) {
	for _, test := range []struct{ baseline, used, old, quota, want int64 }{{0, 60, 100, 200, 200}, {200, 260, 300, 200, 400}, {200, 260, 300, 50, 250}, {0, 120, 100, 50, 50}} {
		got, err := SynchronizedPlanQuota(test.baseline, test.used, test.old, test.quota)
		if err != nil || got != test.want {
			t.Fatalf("%+v got=%d err=%v", test, got, err)
		}
	}
	for _, test := range []struct{ baseline, used, old, quota int64 }{{-1, 0, 100, 100}, {100, 99, 200, 100}, {100, 100, 99, 100}, {math.MaxInt64, math.MaxInt64, math.MaxInt64, 1}, {0, 0, 100, 0}} {
		if _, err := SynchronizedPlanQuota(test.baseline, test.used, test.old, test.quota); err == nil {
			t.Fatalf("unsafe quota accepted %+v", test)
		}
	}
}

func TestPlanQuotaSyncRequiresExplicitQuota(t *testing.T) {
	current := Plan{Revision: 1}
	name := "new"
	if _, _, err := ApplyPlanUpdate(current, PlanUpdateRequest{Name: &name, ExpectedRevision: &current.Revision, SyncTrafficQuota: true}); err == nil {
		t.Fatal("quota sync without quota accepted")
	}
}
