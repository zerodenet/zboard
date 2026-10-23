package commerce

import (
	"testing"
	"time"
)

func TestPerpetualFIFOExhaustedLifecycleHasZeroResidual(t *testing.T) {
	root := perpetualValuePurchase()
	add := perpetualAddition(root, 2, 20, 50, 20000)
	ended := add.CapturedAt.Add(time.Hour)
	for _, used := range []int64{150, 170} {
		current := add.After
		current.FlowUsed = used
		current.Status = "expired"
		current.EndedAt = &ended
		current.EndReason = "exhausted"
		out, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, current, "CNY", ended.Add(time.Minute))
		if err != nil {
			t.Fatalf("normally exhausted FIFO quota %d cannot be settled: %v", used, err)
		}
		assertProjectionConserves(t, out, 0)
	}
	// Other terminal reasons or positive residual with an exhaustion marker are
	// inconsistent evidence, not permission to conceal or discard paid value.
	for _, reason := range []string{"canceled", "expired", "exhausted"} {
		current := add.After
		current.FlowUsed = 140
		current.Status = "expired"
		current.EndedAt = &ended
		current.EndReason = reason
		if _, err := ProjectEntitlementValue([]EntitlementValueSnapshot{root, add}, current, "CNY", ended.Add(time.Minute)); err == nil {
			t.Fatal("terminal state concealed residual value", reason)
		}
	}
}
