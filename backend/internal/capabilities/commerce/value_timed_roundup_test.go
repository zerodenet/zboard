package commerce

import (
	"testing"
	"time"
)

// Model legal persisted datetime(3) representations of the exact same core
// payment clock. No database, synthetic payment authority, or changed cents.
func TestTimedQuotaAdditionRoundUpReadAtCapturedInstant(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	clock := start.AddDate(0, 0, 5)
	captured := clock.Add(500 * time.Microsecond)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 60
	for _, shape := range []string{"sqlite-exact", "mysql-rounded-up", "mysql-truncated"} {
		t.Run(shape, func(t *testing.T) {
			paid := timedAdditionFact(root, before, 2, captured, 2000, 100)
			switch shape {
			case "mysql-rounded-up":
				paid.QuotaEventAt = clock.Add(time.Millisecond)
			case "mysql-truncated":
				paid.QuotaEventAt = clock
			}
			records := []EntitlementValueSnapshot{root, paid}
			// At the actual captured payment instant, current rights and all cash are
			// already committed. A row rounded500us ahead is not a future real payment.
			old, err := ProjectEntitlementValue(records, paid.After, "CNY", captured)
			if err != nil {
				t.Fatal("persisted row representation rejected an already captured payment", shape, err)
			}
			assertProjectionConserves(t, old, 2400)
			out, err := ProjectTimedEntitlementValueWithPlanSyncContext(t.Context(), records, nil, paid.After, "CNY", captured, 7)
			if err != nil {
				t.Fatal("strict timed reader rejected an already captured payment", shape, err)
			}
			assertProjectionConserves(t, out, 2400)
			if out.Fingerprint != old.Fingerprint {
				t.Fatal("representation bridge changed the existing no-sync quote identity", shape, out, old)
			}
			prepared, err := PrepareTimedPlanQuotaSyncContext(t.Context(), records, nil, paid.After, "CNY", captured, 7, 200, 10)
			if err != nil || prepared.CurrentMinorBefore != 2400 || prepared.CurrentMinorAfter != 2400 {
				t.Fatal("same captured clock could not prepare a free base resize", shape, prepared, err)
			}
		})
	}
}
