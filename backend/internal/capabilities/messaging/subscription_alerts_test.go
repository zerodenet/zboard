package messaging

import (
	"math"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

func TestSubscriptionAlertClassification(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	p := AlertPolicy{Low: true, Exhausted: true, Expiring: true, Expired: true, RemainingPercent: 20, ExpiringDays: 3, IntervalHours: 24}
	base := entitlements.Subscription{ID: 1, Status: "active", StartAt: now.Add(-24 * time.Hour), EndAt: now.Add(30 * 24 * time.Hour), FlowTotal: 100, FlowUsed: 80}
	for _, tt := range []struct {
		name   string
		change func(*entitlements.Subscription)
		kinds  []string
	}{
		{"threshold inclusive", func(s *entitlements.Subscription) {}, []string{"low"}},
		{"healthy", func(s *entitlements.Subscription) { s.FlowUsed = 79 }, nil},
		{"exhausted", func(s *entitlements.Subscription) { s.FlowUsed = 100; s.Status = "expired" }, []string{"exhausted"}},
		{"priority", func(s *entitlements.Subscription) { s.FlowUsed = 100; s.EndAt = now.Add(time.Hour) }, []string{"exhausted", "expiring"}},
		{"cycle baseline", func(s *entitlements.Subscription) { s.FlowTotal = 1000; s.FlowUsed = 980; s.CycleStartUsed = 900 }, []string{"low"}},
		{"overflow", func(s *entitlements.Subscription) { s.FlowTotal = math.MaxInt64; s.FlowUsed = math.MaxInt64 - 1 }, []string{"low"}},
		{"zero quota", func(s *entitlements.Subscription) { s.FlowTotal = 0; s.FlowUsed = 0 }, nil},
		{"expired", func(s *entitlements.Subscription) { s.EndAt = now.Add(-time.Hour) }, []string{"expired"}},
		{"no historic backfill", func(s *entitlements.Subscription) { s.EndAt = now.Add(-8 * 24 * time.Hour) }, nil},
		{"cancelled", func(s *entitlements.Subscription) { s.Status = "cancelled" }, nil},
		{"not started", func(s *entitlements.Subscription) { s.StartAt = now.Add(time.Hour) }, nil},
		{"perpetual", func(s *entitlements.Subscription) { s.EndAt = entitlements.PerpetualEnd; s.FlowUsed = 0 }, nil},
		{"fixed exhaustion", func(s *entitlements.Subscription) {
			s.FlowUsed = 100
			s.EndedAt = &now
			s.EndReason = "exhausted"
			s.EndsOnQuotaExhaustion = true
		}, []string{"exhausted"}},
		{"ended cancellation", func(s *entitlements.Subscription) { s.EndedAt = &now; s.EndReason = "cancelled" }, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := base
			tt.change(&sub)
			out := AlertCandidates(sub, p, now)
			if len(out) != len(tt.kinds) {
				t.Fatalf("%+v want %v", out, tt.kinds)
			}
			for i, kind := range tt.kinds {
				if out[i].Kind != kind {
					t.Fatalf("%+v want %v", out, tt.kinds)
				}
			}
		})
	}
	if out := AlertCandidates(base, AlertPolicy{}, now); len(out) != 0 {
		t.Fatal("disabled policy generated alerts", out)
	}
}

func TestSubscriptionAlertEpisodeTracksResetsAndRenewal(t *testing.T) {
	now := time.Now().UTC()
	sub := entitlements.Subscription{ID: 1, Status: "active", StartAt: now.Add(-24 * time.Hour), EndAt: now.Add(time.Hour), ResetPolicy: 1, FlowTotal: 100, FlowUsed: 90}
	p := AlertPolicy{Low: true, Expiring: true, RemainingPercent: 20, ExpiringDays: 3}
	first := AlertCandidates(sub, p, now)
	sub.NextResetAt = entitlements.NextTrafficReset(sub.StartAt, sub.ResetPolicy)
	initialized := AlertCandidates(sub, p, now)
	if first[1] != initialized[1] {
		t.Fatal("initializing reset schedule created another cycle")
	}
	sub.FlowTotal = 110
	sub.FlowUsed = 108
	sub.CycleStartUsed = 90
	reset := AlertCandidates(sub, p, now)
	if reset[1].Episode == first[1].Episode {
		t.Fatal("reset retained old cycle")
	}
	sub.EndAt = sub.EndAt.Add(time.Hour)
	renewed := AlertCandidates(sub, p, now)
	if renewed[0].Episode == first[0].Episode {
		t.Fatal("renewal retained old expiry")
	}
}
