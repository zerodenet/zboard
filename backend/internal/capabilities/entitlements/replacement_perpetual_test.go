package entitlements

import (
	"testing"
	"time"
)

func TestPerpetualFullReplacementEligibilityKeepsLegacyChangeBoundary(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := Subscription{Status: "active", StartAt: now.Add(-time.Hour), EndAt: PerpetualEnd, FlowTotal: 100, FlowUsed: 20, ResetPolicy: 5, EndsOnQuotaExhaustion: true}
	if !CanReplaceAt(valid, now) || CanChangeAt(valid, now) {
		t.Fatal("perpetual full change and legacy keep-term change were conflated")
	}
	for _, name := range []string{"ended", "exhausted", "canceled", "recurring_quota", "pending_reset", "negative_usage"} {
		t.Run(name, func(t *testing.T) {
			sub := valid
			switch name {
			case "ended":
				sub.EndedAt = &now
			case "exhausted":
				sub.FlowUsed = sub.FlowTotal
			case "canceled":
				sub.Status = "canceled"
			case "recurring_quota":
				sub.ResetPolicy = 1
			case "pending_reset":
				sub.NextResetAt = &now
			case "negative_usage":
				sub.FlowUsed = -1
			}
			if CanReplaceAt(sub, now) {
				t.Fatal("ineligible perpetual service accepted")
			}
		})
	}
}
