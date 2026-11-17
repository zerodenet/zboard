package commerce

import (
	"math"
	"testing"
	"time"
)

func TestPaidMonthAndCalendarCycleAllocationConservesActualMoney(t *testing.T) {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	periods, err := UniformPaidPeriods(start, end, "year", 1, 12000)
	if err != nil || len(periods) != 12 {
		t.Fatalf("actual paid months: %+v %v", periods, err)
	}
	cycles, err := SplitPaidValue(periods, start, 1)
	if err != nil || len(cycles) != 13 || cycles[0].AmountMinor != 419 {
		t.Fatalf("calendar intersections: %+v %v", cycles, err)
	}
	var total int64
	for _, cycle := range cycles {
		total += cycle.AmountMinor
	}
	if total != 12000 {
		t.Fatalf("money lost: %d", total)
	}
	small, err := UniformPaidPeriods(start, end, "year", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	total = 0
	for _, p := range small {
		if p.AmountMinor != 8 && p.AmountMinor != 9 {
			t.Fatal("cents piled into one month", small)
		}
		total += p.AmountMinor
	}
	if total != 100 {
		t.Fatal(total)
	}
	split, err := SplitPaidValue(small, start, 1)
	if err != nil {
		t.Fatal(err)
	}
	total = 0
	for _, p := range split {
		total += p.AmountMinor
	}
	if total != 100 {
		t.Fatal(total)
	}
	// Explicit nonuniform source prices are preserved, never averaged to 10.
	periods[0].AmountMinor = 500
	periods[1].AmountMinor = 1500
	cycles, err = SplitPaidValue(periods, start, 1)
	if err != nil || cycles[0].AmountMinor != 210 {
		t.Fatalf("nonuniform prices overwritten: %+v %v", cycles, err)
	}
}
func TestAnnualRemainingValueExamplesUseActualQuotaCycles(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	periods, _ := UniformPaidPeriods(start, end, "year", 1, 12000)
	cycles, err := SplitPaidValue(periods, start, 2)
	if err != nil {
		t.Fatal(err)
	}
	remaining := int64(0)
	for _, c := range cycles[3:] {
		remaining += c.AmountMinor
	}
	if remaining != 9000 || remaining-2000 != 7000 {
		t.Fatalf("three complete months: %d", remaining)
	}
	c := cycles[2]
	basis := RemainingValueBasis{At: c.StartAt, EndAt: c.EndAt, AmountMinor: c.AmountMinor, QuotaBytes: 100}
	current, err := basis.Remaining(c.StartAt, 80)
	if err != nil {
		t.Fatal(err)
	}
	remaining = current
	for _, future := range cycles[3:] {
		remaining += future.AmountMinor
	}
	if current != 200 || remaining != 9200 || remaining-2000 != 7200 {
		t.Fatalf("third month 80 percent: current=%d residual=%d", current, remaining)
	}
	shared, err := SplitPaidValue(periods, start, 0)
	if err != nil || len(shared) != 1 {
		t.Fatal("year shared quota became twelve future months", shared, err)
	}
	basis = RemainingValueBasis{At: start, EndAt: end, AmountMinor: 12000, QuotaBytes: 100}
	value, err := basis.Remaining(start.AddDate(0, 3, 0), 80)
	if err != nil || value != 2400 {
		t.Fatalf("shared annual quota: %d %v", value, err)
	}
}
func TestPaidResetRebasesRemainingValueWithoutResurrectingConsumedMoney(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	at := start.AddDate(0, 0, 10)
	before := RemainingValueBasis{At: start, EndAt: end, AmountMinor: 1000, QuotaBytes: 100}
	basis, old, err := before.PaidReset(at, 80, 100, 100)
	if err != nil || old != 200 || basis.AmountMinor != 300 {
		t.Fatalf("reset did not preserve consumed value: %+v %d %v", basis, old, err)
	}
	immediate, err := basis.Remaining(at, 80)
	if err != nil || immediate != 300 {
		t.Fatalf("instant second discount: %d %v", immediate, err)
	}
	next, old, err := basis.PaidReset(at, 80, 100, 100)
	if err != nil || old != 300 || next.AmountMinor != 400 {
		t.Fatalf("repeated reset: %+v %d %v", next, old, err)
	}
	remaining, err := next.Remaining(at.AddDate(0, 0, 10), 130)
	if err != nil || remaining != 200 {
		t.Fatalf("both constraints after reset: %d %v", remaining, err)
	}
	last, old, err := next.PaidReset(at.AddDate(0, 0, 10), 130, 100, 100)
	if err != nil || old != 200 || last.AmountMinor != 300 {
		t.Fatalf("consumed reset value revived: %+v %d %v", last, old, err)
	}
	value, err := last.Remaining(last.At, 130)
	if err != nil || value != 300 {
		t.Fatal(value, err)
	}
	if totalCash := int64(1000 + 100 + 100 + 100); 800+(400-200)+value != totalCash {
		t.Fatal("consumed and remaining money do not conserve")
	}
	if _, err := last.Remaining(last.At, 129); err == nil {
		t.Fatal("regressed cumulative use accepted")
	}
	max := RemainingValueBasis{At: start, EndAt: end, AmountMinor: math.MaxInt64, QuotaBytes: 100}
	if _, _, err := max.PaidReset(start, 0, 1, 100); err == nil {
		t.Fatal("overflow accepted")
	}
}
