package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func unspentPerpetualHistory(n int) ([]EntitlementValueSnapshot, EntitlementValueState, time.Time) {
	records := make([]EntitlementValueSnapshot, 1, n)
	records[0] = perpetualValuePurchase()
	records[0].ActualPaidMinor = 101
	for id := 2; id <= n; id++ {
		previous := records[len(records)-1]
		records = append(records, perpetualAddition(previous, uint(id), 0, 100, int64(100+id%13)))
	}
	last := records[len(records)-1]
	return records, last.After, last.CapturedAt.Add(time.Hour)
}

func TestValueProjectionLargePerpetualHistoryUsesEveryOriginalPaidLot(t *testing.T) {
	const purchases = 1200
	records, original, at := unspentPerpetualHistory(purchases)
	for _, used := range []int64{0, 60150, 119950, 120000, 120025} {
		current := original
		current.FlowUsed = used
		remainingUse := used
		var want int64
		sources := map[uint]int64{}
		for _, fact := range records {
			if remainingUse >= 100 {
				remainingUse -= 100
				continue
			}
			amount := fact.ActualPaidMinor
			if remainingUse != 0 {
				if remainingUse != 50 {
					t.Fatal("invalid fixture's half-used lot")
				}
				amount = (amount + 1) / 2 // Established single-lot half-up.
				remainingUse = 0
			}
			sources[fact.OrderID] = amount
			want += amount
		}
		out, err := ProjectEntitlementValue(records, current, "CNY", at)
		if err != nil {
			t.Fatalf("used=%d: %v", used, err)
		}
		assertProjectionConserves(t, out, want)
		if !reflect.DeepEqual(out.Sources, sources) || out.FutureMinor != 0 || out.ValuationModel != PerpetualQuotaFIFOAllocation {
			t.Fatalf("used=%d: original lot values were merged or lost", used)
		}
	}
}

func TestValueProjectionLargeSequentialHistoryDoesNotReviveConsumedCash(t *testing.T) {
	for _, overrun := range []int64{0, 25} {
		records, current, at := perpetualSequentialHistory(1200, overrun)
		out, err := ProjectEntitlementValue(records, current, "CNY", at)
		if err != nil {
			t.Fatal(err)
		}
		want := (records[len(records)-1].ActualPaidMinor + 1) / 2
		assertProjectionConserves(t, out, want)
		if len(out.Sources) != 1 || out.Sources[1200] != want {
			t.Fatal("past overrun or a consumed lot spent future paid cash")
		}
	}
}

func largeTimedHistory(t *testing.T, n int) ([]EntitlementValueSnapshot, EntitlementValueState, time.Time) {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	root := projectionPurchase(t, start, "month", 1, 1000, 2)
	records := make([]EntitlementValueSnapshot, 1, n)
	records[0] = root
	for id := 2; id <= n; id++ {
		previous := records[len(records)-1]
		records = append(records, projectionRenewal(t, root, previous.After, uint(id), start.Add(time.Duration(id)*time.Second), int64(100+id%13)))
	}
	return records, records[len(records)-1].After, start.AddDate(0, 0, 1)
}

func TestValueProjectionLargeTimedHistoryKeepsFutureSourcePricesAndIdentity(t *testing.T) {
	records, current, at := largeTimedHistory(t, 1200)
	current.FlowUsed = 25
	var future int64
	for _, fact := range records[1:] {
		future += fact.ActualPaidMinor
	}
	before, _ := json.Marshal(records)
	out, err := ProjectEntitlementValueContext(t.Context(), records, current, "CNY", at)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectionConserves(t, out, 750+future)
	if out.CurrentMinor != 750 || out.FutureMinor != future || len(out.Sources) != len(records) || out.Sources[1] != 750 {
		t.Fatal("current or future paid cycles were repriced")
	}
	for _, fact := range records[1:] {
		if out.Sources[fact.OrderID] != fact.ActualPaidMinor {
			t.Fatalf("source %d lost its actual purchase price", fact.OrderID)
		}
	}
	reversed := append([]EntitlementValueSnapshot(nil), records...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	replayed, err := ProjectEntitlementValue(reversed, current, "CNY", at)
	if err != nil || !reflect.DeepEqual(out, replayed) {
		t.Fatal("input order changed the financial quote", err)
	}
	after, _ := json.Marshal(records)
	if string(before) != string(after) {
		t.Fatal("projection mutated immutable purchase evidence")
	}
}

func TestValueProjectionLargeHistoryRejectsUnknownSourcesWithoutZeroQuote(t *testing.T) {
	for _, timed := range []bool{false, true} {
		for _, kind := range []string{"unverified", "missing-before", "wrong-currency"} {
			records, current, at := unspentPerpetualHistory(1200)
			if timed {
				records, current, at = largeTimedHistory(t, 1200)
			}
			switch kind {
			case "unverified":
				records[1100].PaymentVerified = false
			case "missing-before":
				records[1100].Before = nil
			case "wrong-currency":
				records[1100].Currency = "USD"
			}
			out, err := ProjectEntitlementValue(records, current, "CNY", at)
			if !errors.Is(err, ErrValueEvidence) || out.Fingerprint != "" || out.TotalMinor != 0 || len(out.Sources) != 0 {
				t.Fatalf("timed=%v kind=%s: unknown history produced a usable quote", timed, kind)
			}
		}
	}
}

// Cancellation at a deterministic checkpoint uses an ordinary cancel context;
// its Done channel closes and Err remains canceled thereafter. No timing sleep
// or change to monetary evidence is used to interrupt an in-progress replay.
type valueReplayCancellation struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *valueReplayCancellation) Err() error {
	if c.remaining > 0 {
		c.remaining--
		if c.remaining == 0 {
			c.cancel()
		}
	}
	return c.Context.Err()
}

func TestValueProjectionContextStopsDuringLargeReplayWithoutQuote(t *testing.T) {
	for _, timed := range []bool{false, true} {
		records, current, at := unspentPerpetualHistory(1200)
		if timed {
			records, current, at = largeTimedHistory(t, 1200)
		}
		base, cancel := context.WithCancel(t.Context())
		ctx := &valueReplayCancellation{Context: base, cancel: cancel, remaining: len(records) + 100}
		out, err := ProjectEntitlementValueContext(ctx, records, current, "CNY", at)
		cancel()
		if !errors.Is(err, context.Canceled) || out.Fingerprint != "" || out.TotalMinor != 0 || len(out.Sources) != 0 || ctx.remaining != 0 {
			t.Fatalf("timed=%v: canceled replay returned a financial quote: %v", timed, err)
		}
	}
}
