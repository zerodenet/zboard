package commerce

import "testing"

func TestAdministrativeRefundUsesOnlyFundedSourcesAndKeepsResidualFirst(t *testing.T) {
	remaining := map[uint]int64{2: 4800}
	limits := map[uint]int64{1: 10000, 2: 6000}
	for _, amount := range []int64{0, 1, 3000, 4800, 9500, 16000} {
		out, err := AllocateAdministrativeRefund(amount, remaining, limits)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		for id, part := range out {
			if part < 0 || part > limits[id] {
				t.Fatal(out)
			}
			total += part
		}
		if total != amount {
			t.Fatalf("approved refund %d sources %+v", amount, out)
		}
		if amount <= 4800 && out[1] != 0 || amount > 4800 && out[2] < 4800 {
			t.Fatalf("default source precedence %+v", out)
		}
	}
	for _, amount := range []int64{-1, 16001} {
		if _, err := AllocateAdministrativeRefund(amount, remaining, limits); err == nil {
			t.Fatal("unfunded refund accepted", amount)
		}
	}
	if _, err := AllocateAdministrativeRefund(1, map[uint]int64{2: 6001}, limits); err == nil {
		t.Fatal("inconsistent default evidence accepted")
	}
	if out, err := AllocateAdministrativeRefund(0, map[uint]int64{}, map[uint]int64{}); err != nil || len(out) != 0 {
		t.Fatal(out, err)
	}
}
