package jobs

import (
	"errors"
	"testing"
	"time"
)

func TestRuntimeDatabasePressureDoesNotInventWriteCapacityFromEmptyQueue(t *testing.T) {
	r := &Runtime{}
	now := time.Now()
	r.pressure.record(now.Add(-time.Millisecond), now, false, nil)
	p := r.DatabasePressure(now, time.Minute)
	if p.Writes != 0 || p.ObservedAt.IsZero() || p.Failed {
		t.Fatal("read-only empty probe became successful write evidence", p)
	}
	for i := range 100 {
		at := now.Add(time.Duration(i+1) * time.Millisecond)
		r.pressure.record(at.Add(-time.Millisecond), at, true, nil)
	}
	p = r.DatabasePressure(now.Add(time.Second), time.Minute)
	if p.Writes != pressureWindow || p.MaxLatency != time.Millisecond {
		t.Fatal("pressure ring did not retain its fixed bound", p)
	}
	if p := r.DatabasePressure(now.Add(2*time.Minute), time.Minute); p.Writes != 0 || !p.ObservedAt.IsZero() {
		t.Fatal("expired observations became fresh spare capacity", p)
	}
}

func TestRuntimeDatabasePressureRetainsFailureAfterHealthyStatements(t *testing.T) {
	r := &Runtime{}
	now := time.Now()
	r.pressure.record(now.Add(-time.Second), now, true, errors.New("unknown commit result"))
	for i := range 100 {
		at := now.Add(time.Duration(i+1) * time.Millisecond)
		r.pressure.record(at.Add(-time.Millisecond), at, true, nil)
	}
	if p := r.DatabasePressure(now.Add(time.Second), time.Minute); !p.Failed {
		t.Fatal("later writes erased recent failed/unknown completion", p)
	}
	if p := r.DatabasePressure(now.Add(2*time.Minute), time.Minute); p.Failed {
		t.Fatal("failed observation never expired", p)
	}
}
