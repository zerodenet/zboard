package commerce

import (
	"fmt"
	"testing"
	"time"
)

// Repeatedly exhaust each purchased lot before the next purchase. Any initial
// overrun predates the new grants and must never spend their paid quota.
func perpetualSequentialHistory(n int, overrun int64) ([]EntitlementValueSnapshot, EntitlementValueState, time.Time) {
	records := []EntitlementValueSnapshot{perpetualValuePurchase()}
	for id := 2; id <= n; id++ {
		previous := records[len(records)-1]
		used := previous.After.FlowUsed + 100
		if id == 2 {
			used += overrun
		}
		records = append(records, perpetualAddition(previous, uint(id), used, 100, int64(100+id%13)))
	}
	last := records[len(records)-1]
	current := last.After
	current.FlowUsed += 50
	return records, current, last.CapturedAt.Add(time.Hour)
}

func TestPerpetualFIFOSequentialHistoryKeepsOnlyFinalLotResidual(t *testing.T) {
	// Captured from the previous implementation before changing its traversal.
	// Quote identities must remain valid across this implementation upgrade.
	fingerprints := map[string]string{
		"lots2/overrun0":     "bf93be402784630b02bcc1cc911651d289126938b55b4eee14bc330e8812384f",
		"lots2/overrun25":    "d32bbde4a6f92260d45576c7884e88b7d90859fd880185b08543030e20b46694",
		"lots17/overrun0":    "a865bdce3d5e6b3753d0ebd0c19ef900b45d29be0ccca6b07092de7a1be7343f",
		"lots17/overrun25":   "551f1ab4e03d6c57f501207d0cb9e2e6ca10efe93ced014b2a01615f6c9a18b6",
		"lots1000/overrun0":  "7e2ae2eb622a25672e8b84f0500d4f6fcfbb201bf8a2a87c8762ff36c4c93dec",
		"lots1000/overrun25": "29e822b1156510120582a820ada007ef508f81382983c7a82b2991f17fae58ae",
	}
	for _, n := range []int{2, 17, 1000} {
		for _, overrun := range []int64{0, 25} {
			name := fmt.Sprintf("lots%d/overrun%d", n, overrun)
			t.Run(name, func(t *testing.T) {
				records, current, at := perpetualSequentialHistory(n, overrun)
				out, err := ProjectEntitlementValue(records, current, "CNY", at)
				if err != nil {
					t.Fatal(err)
				}
				want := (records[n-1].ActualPaidMinor + 1) / 2 // existing half-up rule
				assertProjectionConserves(t, out, want)
				if len(out.Sources) != 1 || out.Sources[uint(n)] != want || out.FutureMinor != 0 {
					t.Fatalf("consumed lots regained value: %+v", out)
				}
				if out.Fingerprint != fingerprints[name] {
					t.Fatal("existing quote identity changed", out.Fingerprint)
				}
				t.Logf("unchanged quote fingerprint %s", out.Fingerprint)
			})
		}
	}
	tooMany, current, at := perpetualSequentialHistory(1001, 0)
	if _, err := ProjectEntitlementValue(tooMany, current, "CNY", at); err != ErrValueEvidence {
		t.Fatal("existing source bound changed", err)
	}
}

func BenchmarkPerpetualFIFOSequentialPurchases(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			records, current, at := perpetualSequentialHistory(n, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := ProjectEntitlementValue(records, current, "CNY", at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
