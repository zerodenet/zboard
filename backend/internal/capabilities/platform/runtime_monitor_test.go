package platform

import (
	"testing"
	"time"
)

type fakeSystemReader struct{ sample SystemSample }

func (r *fakeSystemReader) Read() SystemSample { return r.sample }
func TestRuntimeMonitorMinuteAggregationAndBoundedRetention(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	r := &fakeSystemReader{sample: SystemSample{At: now}}
	m := NewRuntimeMonitor(r)
	if got := m.Snapshot(60, now); got.Current != nil || len(got.Points) != 0 {
		t.Fatal("unstarted monitor invented a sample")
	}
	m.collect()
	if m.points[0].CPU != nil {
		t.Fatal("unknown became zero")
	}
	zero, twenty := 0.0, 20.0
	r.sample.CPU = &zero
	r.sample.At = now.Add(5 * time.Second)
	m.collect()
	r.sample.CPU = &twenty
	r.sample.At = now.Add(10 * time.Second)
	m.collect()
	if got := *m.points[0].CPU; got != 10 {
		t.Fatalf("minute average=%v", got)
	}
	for i := 1; i <= 1500; i++ {
		r.sample.At = now.Add(time.Duration(i) * time.Minute)
		m.collect()
	}
	if len(m.points) != 1440 || cap(m.points) != 1440 {
		t.Fatalf("unbounded ring: %d/%d", len(m.points), cap(m.points))
	}
	got := m.Snapshot(60, r.sample.At)
	if len(got.Points) != 61 {
		t.Fatalf("range %d", len(got.Points))
	}
	got.Points[0].At = time.Time{}
	if m.points[len(m.points)-61].At.IsZero() {
		t.Fatal("snapshot aliases ring")
	}
}
func TestRuntimeMonitorLifecycle(t *testing.T) {
	m := NewRuntimeMonitor(&fakeSystemReader{sample: SystemSample{At: time.Now()}})
	m.Start()
	m.Start()
	m.Close()
	m.Close()
	m.Start()
	m2 := NewRuntimeMonitor(&fakeSystemReader{})
	m2.Close()
	m2.Start()
}

func TestRuntimeMonitorExpiresByAgeAfterCollectionGap(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	r := &fakeSystemReader{sample: SystemSample{At: now}}
	m := NewRuntimeMonitor(r)
	m.collect()
	stale := m.Snapshot(1440, now.Add(25*time.Hour))
	if len(stale.Points) != 0 || stale.Current != nil {
		t.Fatal("expired history/current survived a collection gap")
	}
	r.sample.At = now.Add(25 * time.Hour)
	m.collect()
	fresh := m.Snapshot(1440, r.sample.At)
	if len(fresh.Points) != 1 || !fresh.Points[0].At.Equal(r.sample.At) {
		t.Fatal("new collection retained old buckets")
	}
}
