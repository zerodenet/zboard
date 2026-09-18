package observability

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dashboardRepositoryStub struct{ called bool }

func (s *dashboardRepositoryStub) LoadDashboard(context.Context, DashboardPeriod, time.Time, []DashboardTrendBucket) (DashboardSnapshot, error) {
	s.called = true
	return DashboardSnapshot{}, nil
}

func (s *dashboardRepositoryStub) LoadDashboardTotals(context.Context, time.Time) (DashboardTotals, error) {
	s.called = true
	return DashboardTotals{}, nil
}

func TestDashboardRejectsIncompleteReadWindows(t *testing.T) {
	now := time.Now().UTC()
	valid := DashboardPeriod{From: now.Add(-time.Hour), To: now, PreviousFrom: now.Add(-2 * time.Hour), PreviousTo: now.Add(-time.Hour)}
	bucket := []DashboardTrendBucket{{Key: "b000", StartUTC: valid.From, EndUTC: valid.To}}
	for _, input := range []struct {
		service Dashboard
		period  DashboardPeriod
		buckets []DashboardTrendBucket
	}{{Dashboard{}, valid, bucket}, {Dashboard{Repository: &dashboardRepositoryStub{}}, DashboardPeriod{}, bucket}} {
		if _, err := input.service.Load(context.Background(), input.period, now, input.buckets); !errors.Is(err, ErrDashboardInvalid) {
			t.Fatalf("error = %v, want ErrDashboardInvalid", err)
		}
	}
	stub := &dashboardRepositoryStub{}
	instant := DashboardPeriod{From: now, To: now, PreviousFrom: now.Add(-24 * time.Hour), PreviousTo: now.Add(-24 * time.Hour)}
	if _, err := (Dashboard{Repository: stub}).Load(context.Background(), instant, now, nil); err != nil || !stub.called {
		t.Fatalf("zero-duration midnight window was rejected: called=%v err=%v", stub.called, err)
	}
}
