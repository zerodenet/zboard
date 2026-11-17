package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeDatabaseDiagnosticsKeepDurableCallsAndFreshnessSeparate(t *testing.T) {
	now := time.Now()
	r := &Runtime{}
	empty := r.DatabaseDiagnostics(now, time.Minute)
	if empty.Claim.Known || empty.Claim.MaximumMS != nil || empty.Finish.Known || empty.Finish.P95MS != nil {
		t.Fatal("no samples became zero", empty)
	}
	r.pressure.recordOperation(now.Add(-time.Millisecond), now, true, nil, false, pressureClaim)
	r.pressure.recordOperation(now.Add(-3*time.Millisecond), now, false, nil, false, pressureEmptyClaim)
	r.pressure.recordOperation(now.Add(-2*time.Millisecond), now, true, errors.New("unknown commit"), false, pressureFinish)
	r.pressure.recordKind(now.Add(-time.Hour), now, true, nil, true)
	actual := r.DatabaseDiagnostics(now, time.Minute)
	if actual.Claim.SampleCount != 2 || actual.Claim.EmptyProbes != 1 || actual.Claim.SuccessfulWrites != 1 || actual.Claim.MaximumMS == nil || *actual.Claim.MaximumMS != 3 || actual.Finish.SampleCount != 1 || actual.Finish.ErrorCount != 1 || actual.Finish.SuccessfulWrites != 0 {
		t.Fatal("projection/empty/failed calls mixed", actual)
	}
	stale := r.DatabaseDiagnostics(now.Add(2*time.Minute), time.Minute)
	if stale.Claim.Known || stale.Claim.ObservedAt == nil || stale.Claim.MeanMS != nil || stale.Claim.SampleCount != 0 {
		t.Fatal("stale diagnostics lost history or invented zero", stale)
	}
	for range 100 {
		r.pressure.recordOperation(now.Add(-time.Millisecond), now, true, nil, false, pressureFinish)
	}
	bounded := r.DatabaseDiagnostics(now, time.Minute)
	if bounded.Claim.Known || bounded.Claim.ObservedAt != nil || bounded.Finish.SampleCount != pressureWindow {
		t.Fatal("shared sample ring was not bounded", bounded)
	}
}
func TestRuntimeDatabaseDiagnosticsObserveActualClaimAndFinishHooks(t *testing.T) {
	r := &Runtime{}
	start := time.Now()
	r.observeClaim(start, Claim{}, ErrEmpty)
	view := r.DatabaseDiagnostics(time.Now(), time.Minute)
	if view.Claim.SampleCount != 1 || view.Claim.EmptyProbes != 1 || view.Claim.MaximumMS == nil {
		t.Fatal(view)
	}
	// The actual completion wrapper is invoked; its Store error must preserve
	// identity while yielding a failed measured call, not a successful write.
	sentinel := errors.New("finish fixture")
	wrapper := runtimeExecutionStore{Store: diagnosticFinishStore{err: sentinel}, runtime: r}
	if e := wrapper.Finish(context.Background(), Claim{}, Succeeded); e != sentinel {
		t.Fatal(e)
	}
	view = r.DatabaseDiagnostics(time.Now(), time.Minute)
	if view.Finish.SampleCount != 1 || view.Finish.ErrorCount != 1 || view.Finish.SuccessfulWrites != 0 || view.Finish.MaximumMS == nil {
		t.Fatal(view)
	}
}

type diagnosticFinishStore struct {
	Store
	err error
}

func (s diagnosticFinishStore) Finish(context.Context, Claim, State) error { return s.err }
