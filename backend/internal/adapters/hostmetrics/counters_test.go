package hostmetrics

import "testing"

func TestDifferentialCounters(t *testing.T) {
	for _, tc := range []struct {
		value, previous uint64
		seconds         float64
		valid           bool
		want            *float64
	}{
		{10, 5, 5, true, value(1)}, {10, 10, 5, true, value(0)}, {1, 10, 5, true, nil}, {10, 5, 0, true, nil}, {10, 5, 5, false, nil},
	} {
		got := counterRate(tc.value, tc.previous, tc.seconds, tc.valid)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Fatalf("rate %v", got)
		}
	}
	for _, tc := range []struct {
		total, idle, prevTotal, prevIdle uint64
		valid                            bool
		want                             *float64
	}{
		{200, 150, 100, 75, true, value(25)}, {200, 200, 100, 100, true, value(0)}, {100, 100, 100, 100, true, nil}, {10, 2, 100, 5, true, nil}, {200, 150, 100, 75, false, nil},
	} {
		got := cpuPercent(tc.total, tc.idle, tc.prevTotal, tc.prevIdle, tc.valid)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Fatalf("cpu %v", got)
		}
	}
}
func value(n float64) *float64 { return &n }
