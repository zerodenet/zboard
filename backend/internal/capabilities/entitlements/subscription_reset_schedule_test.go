package entitlements

import (
	"testing"
	"time"
)

func TestAdministrativeResetScheduleUsesOriginalAnchorWithoutGrantingPastCycles(t *testing.T) {
	anchor := time.Date(2024, 1, 31, 8, 30, 0, 0, time.UTC)
	now := time.Date(2024, 2, 29, 8, 30, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	sub := Subscription{Status: "active", StartAt: anchor, EndAt: anchor.AddDate(2, 0, 0), ResetPolicy: 0, NextResetAt: &old, FlowTotal: 1000, FlowUsed: 900, ResetQuotaBytes: 1000, CycleStartUsed: 0}
	for _, policy := range []int16{0, 1, 2, 3, 4, 5} {
		updated, err := ChangeTrafficResetSchedule(sub, policy, now)
		if err != nil || updated.FlowTotal != 1000 || updated.FlowUsed != 900 || updated.CycleStartUsed != 0 || updated.ResetQuotaBytes != 1000 || !updated.EndAt.Equal(sub.EndAt) {
			t.Fatal("schedule correction altered current entitlement", updated, err)
		}
		if policy == 0 || policy == 5 {
			if updated.NextResetAt != nil {
				t.Fatal("disabled reset retained old date")
			}
		} else if updated.NextResetAt == nil || !updated.NextResetAt.After(now) {
			t.Fatal("correction scheduled a past reset", updated)
		}
		if policy == 2 && !updated.NextResetAt.Equal(time.Date(2024, 3, 31, 8, 30, 0, 0, time.UTC)) {
			t.Fatal("clamped month lost original purchase day", updated.NextResetAt)
		}
	}
	if sub.ResetPolicy != 0 || sub.NextResetAt != &old {
		t.Fatal("correction mutated input")
	}
	sub.StartAt = now.AddDate(0, 2, 0)
	updated, err := ChangeTrafficResetSchedule(sub, 1, now)
	if err != nil || updated.NextResetAt == nil || !updated.NextResetAt.After(sub.StartAt) {
		t.Fatal("future subscription scheduled before service start", updated, err)
	}
}
