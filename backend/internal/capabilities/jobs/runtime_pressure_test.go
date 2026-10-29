package jobs

import (
	"context"
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

func TestRuntimeStorageObservationTracksInflightSeparatelyAndClosesExactlyOnce(t *testing.T) {
	r := &Runtime{}
	finish := r.BeginStorageObservation()
	p := r.DatabasePressure(time.Now().Add(3*time.Second), time.Minute)
	if p.StorageActive != 1 || p.MaxStorageLatency < 3*time.Second || p.MaxLatency != 0 || p.Writes != 0 {
		t.Fatal("active storage looked idle or was mislabeled SQL latency", p)
	}
	finish(true, nil)
	finish(true, nil)
	p = r.DatabasePressure(time.Now(), time.Minute)
	if p.StorageActive != 0 || p.StorageFailed || p.StorageUnknown || p.Writes != 1 {
		t.Fatal("completion lost or duplicated storage evidence", p)
	}
	failed := r.BeginStorageObservation()
	failed(false, context.Canceled)
	p = r.DatabasePressure(time.Now(), time.Minute)
	if p.StorageActive != 0 || !p.StorageFailed || p.Failed || p.Writes != 1 {
		t.Fatal("cancellation became a successful write or fake SQL failure", p)
	}
}

func TestRuntimeStorageObservationFixedSlotsOverflowStaysUnknownUntilFinished(t *testing.T) {
	r := &Runtime{}
	var finishes []func(bool, error)
	for range 9 {
		finishes = append(finishes, r.BeginStorageObservation())
	}
	p := r.DatabasePressure(time.Now(), time.Minute)
	if p.StorageActive != 8 || !p.StorageUnknown || p.Writes != 0 {
		t.Fatal("overflow looked like observed spare capacity", p)
	}
	finishes[8](false, nil)
	p = r.DatabasePressure(time.Now(), time.Minute)
	if p.StorageUnknown || p.StorageActive != 8 {
		t.Fatal("overflow finish corrupted owned slots", p)
	}
	for _, finish := range finishes[:8] {
		finish(false, nil)
	}
	p = r.DatabasePressure(time.Now(), time.Minute)
	if p.StorageActive != 0 || p.StorageUnknown || p.Writes != 0 {
		t.Fatal("slot cleanup invented writes", p)
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
