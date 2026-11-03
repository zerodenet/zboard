package platform

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testHistory struct {
	rows             []SystemMinute
	loadErr, saveErr error
	saves            int
}

func (s *testHistory) Load(context.Context, time.Time) ([]SystemMinute, error) {
	return append([]SystemMinute(nil), s.rows...), s.loadErr
}
func (s *testHistory) Save(_ context.Context, rows []SystemMinute, _ time.Time) error {
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.rows = append([]SystemMinute(nil), rows...)
	return nil
}
func TestPersistentMonitorRestoresWeightsWithinSameMinute(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	store := &testHistory{rows: []SystemMinute{{At: now, Sums: [5]float64{60}, Counts: [5]int{2}}}}
	cpu := 90.0
	r := &fakeSystemReader{sample: SystemSample{At: now.Add(15 * time.Second), CPU: &cpu}}
	m := NewRuntimeMonitor(r, store)
	if !m.restore(now.Add(10 * time.Second)) {
		t.Fatal("restore failed")
	}
	m.collect()
	got := m.Snapshot(60, r.sample.At)
	if len(got.Points) != 1 || *got.Points[0].CPU != 50 {
		t.Fatalf("restarted average/duplicate minute: %+v", got.Points)
	}
	if !got.History.Persistent || got.History.State != "ready" {
		t.Fatal(got.History)
	}
}
func TestPersistentMonitorWritesCompletedMinutesAndFlushesCurrent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	store := &testHistory{}
	r := &fakeSystemReader{sample: SystemSample{At: now}}
	m := NewRuntimeMonitor(r, store)
	m.restore(now)
	for i := 0; i < 12; i++ {
		r.sample.At = now.Add(time.Duration(i) * 5 * time.Second)
		m.collect()
		m.Snapshot(60, r.sample.At)
	}
	if store.saves != 0 {
		t.Fatal("sampling/page reads wrote the database")
	}
	m.flush(now.Add(time.Minute), false)
	if store.saves != 1 || len(store.rows) != 1 {
		t.Fatal("completed minute not persisted")
	}
	m.flush(now.Add(time.Minute+time.Second), false)
	if store.saves != 1 {
		t.Fatal("unchanged minute written repeatedly")
	}
	r.sample.At = now.Add(time.Minute + 5*time.Second)
	m.collect()
	m.flush(r.sample.At, true)
	if len(store.rows) != 1 || !store.rows[0].At.Equal(now.Add(time.Minute)) {
		t.Fatal("shutdown failed to save partial minute")
	}
}
func TestPersistentMonitorFailureBacklogIsBoundedAndRetries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	store := &testHistory{saveErr: errors.New("offline")}
	r := &fakeSystemReader{sample: SystemSample{At: now}}
	m := NewRuntimeMonitor(r, store)
	m.restore(now)
	for i := 0; i < 1500; i++ {
		r.sample.At = now.Add(time.Duration(i) * time.Minute)
		m.collect()
	}
	m.flush(r.sample.At, false)
	if got := m.Snapshot(1440, r.sample.At); got.History.State != "error" || got.History.PendingMinutes != 1440 {
		t.Fatal(got.History)
	}
	if len(m.minutes) != 1440 || len(m.points) != 1440 {
		t.Fatal("unbounded outage backlog")
	}
	store.saveErr = nil
	m.flush(r.sample.At, false)
	if len(store.rows) != 1439 || m.Snapshot(1440, r.sample.At).History.PendingMinutes != 1 {
		t.Fatal("unexpired backlog not flushed")
	}
	m.flush(r.sample.At.Add(25*time.Hour), false)
	if len(store.rows) != 0 || len(m.minutes) != 0 || len(m.dirty) != 0 {
		t.Fatal("outage/idle expiry retained old data")
	}
}
func TestPersistentMonitorNeverOverwritesHistoryBeforeRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	store := &testHistory{loadErr: errors.New("offline"), rows: []SystemMinute{{At: now.Add(-time.Minute)}}}
	r := &fakeSystemReader{sample: SystemSample{At: now}}
	m := NewRuntimeMonitor(r, store)
	m.restore(now)
	m.collect()
	m.flush(now.Add(time.Minute), true)
	if store.saves != 0 || len(store.rows) != 1 {
		t.Fatal("failed restore overwrote retained history")
	}
	store.loadErr = nil
	m.flush(now.Add(time.Minute), true)
	if m.Snapshot(60, now.Add(time.Minute)).History.State != "ready" {
		t.Fatal("restore did not recover")
	}
}
