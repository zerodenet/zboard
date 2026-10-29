package commerce

import (
	"testing"
	"time"
)

func TestTimedPaidBasisRoundUpProofDoesNotAcceptFutureOrUnprovedCapture(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	clock := start.AddDate(0, 0, 5)
	captured := clock.Add(500 * time.Microsecond)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 60
	for _, variant := range []string{"before-capture", "row-too-far", "row-not-millisecond-representation", "capture-after-row", "missing-capture", "unverified"} {
		t.Run(variant, func(t *testing.T) {
			paid := timedAdditionFact(root, before, 2, captured, 2000, 100)
			paid.QuotaEventAt = clock.Add(time.Millisecond)
			readAt := captured
			switch variant {
			case "before-capture":
				readAt = captured.Add(-time.Nanosecond)
			case "row-too-far":
				paid.QuotaEventAt = clock.Add(2 * time.Millisecond)
			case "row-not-millisecond-representation":
				paid.QuotaEventAt = clock.Add(750 * time.Microsecond)
			case "capture-after-row":
				paid.CapturedAt = clock.Add(1500 * time.Microsecond)
				paid.TermStartAt = paid.CapturedAt
			case "missing-capture":
				paid.CapturedAt = time.Time{}
			case "unverified":
				paid.PaymentVerified = false
			}
			if out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, paid}, paid.After, "CNY", readAt); err == nil || out.Fingerprint != "" {
				t.Fatal("bounded row representation bridge accepted an unproved future payment", variant, out, err)
			}
		})
	}
}
func TestTimedPaidResetRoundUpPreservesExistingActualCashAndBasis(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	clock := start.AddDate(0, 0, 5)
	captured := clock.Add(500 * time.Microsecond)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	before := root.After
	before.FlowUsed = 60
	paid := resetProjectionFact(root, before, 2, 2, captured, 100)
	paid.QuotaEventAt = clock.Add(time.Millisecond)
	out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, paid}, paid.After, "CNY", captured)
	if err != nil {
		t.Fatal("actual paid reset is committed within its rounded row interval", err)
	}
	assertProjectionConserves(t, out, 500)
	if !paid.QuotaEventAt.Equal(clock.Add(time.Millisecond)) || !paid.CapturedAt.Equal(captured) {
		t.Fatal("immutable paid clocks were rewritten")
	}
	later, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, paid}, paid.After, "CNY", paid.QuotaEventAt)
	if err != nil || later.TotalMinor != out.TotalMinor || later.Fingerprint != out.Fingerprint {
		t.Fatal("normal at-row billing or quote fingerprint changed", out, later, err)
	}
}
