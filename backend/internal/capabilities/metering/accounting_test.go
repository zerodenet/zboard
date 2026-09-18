package metering

import (
	"math"
	"testing"
)

func TestAccountingChargeBoundsAndCounterOverflow(t *testing.T) {
	if _, err := CheckedTrafficBytes(math.MaxInt64, 1, 0); err == nil {
		t.Fatal("overflow accepted")
	}
	if got, err := CheckedTrafficBytes(math.MaxInt64, 1, 1); err != nil || got != math.MaxInt64 {
		t.Fatalf("upload only: %d %v", got, err)
	}
	if _, err := BilledTrafficBytes(math.MaxInt64, 1001); err == nil {
		t.Fatal("billed overflow accepted")
	}
	current := FlowCounters{Raw: 1001, Upload: 500, Download: 501}
	delta, err := CalculateCharge(current, FlowCounters{}, 1500, 2000, 0)
	if err != nil || delta.Charged != 1502 {
		t.Fatalf("rounding: %+v %v", delta, err)
	}
	delta, err = CalculateCharge(current, FlowCounters{}, 1500, 1000, 990)
	if err != nil || delta.Charged != 10 {
		t.Fatalf("remaining quota: %+v %v", delta, err)
	}
	delta, err = CalculateCharge(current, current, 1500, 2000, 0)
	if err != nil || delta.Charged != 0 {
		t.Fatalf("replay delta: %+v %v", delta, err)
	}
	if _, err = CalculateCharge(FlowCounters{Raw: 1, Upload: 1}, current, 1000, 2000, 0); err == nil {
		t.Fatal("regressed counters accepted")
	}
}
