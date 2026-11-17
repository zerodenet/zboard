package observability

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dashboardRepositoryStub struct{ called bool }

func (s *dashboardRepositoryStub) LoadTrafficRankings(context.Context, DashboardPeriod, ...string) (DashboardTrafficRankings, error) {
	s.called = true
	return DashboardTrafficRankings{}, nil
}

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

type dashboardViewStub struct {
	dashboardRepositoryStub
	view string
}

func (s *dashboardViewStub) LoadDashboardView(_ context.Context, _ DashboardPeriod, _ time.Time, _ []DashboardTrendBucket, view string) (DashboardSnapshot, error) {
	s.view = view
	return DashboardSnapshot{}, nil
}
func TestDashboardFocusedReadsValidateBeforeDispatch(t *testing.T) {
	now := time.Now().UTC()
	period := DashboardPeriod{From: now.Add(-time.Hour), To: now, PreviousFrom: now.Add(-2 * time.Hour), PreviousTo: now.Add(-time.Hour)}
	stub := &dashboardViewStub{}
	service := Dashboard{Repository: stub}
	for _, view := range []string{"current", "statistics", "trend"} {
		if _, err := service.View(context.Background(), period, now, nil, view); err != nil || stub.view != view {
			t.Fatal(view, err)
		}
	}
	stub.view = ""
	if _, err := service.View(context.Background(), period, now, nil, "unknown"); !errors.Is(err, ErrDashboardInvalid) || stub.view != "" {
		t.Fatal(err)
	}
	if _, err := service.View(context.Background(), DashboardPeriod{}, now, nil, "current"); !errors.Is(err, ErrDashboardInvalid) {
		t.Fatal(err)
	}
}
