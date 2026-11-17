package application

import "testing"

func TestRuntimeMonitorDoesNotStartWithoutPersistenceDatabase(t *testing.T) {
	s := New(nil, "test-secret")
	defer s.Close()
	if s.RuntimeMonitor != nil {
		t.Fatal("monitor must not start with an unusable persistence adapter")
	}
	s.StartWork()
}
