package jobrun

import (
	"sync"
	"testing"
	"time"
)

func TestConcurrentExecutionsAndFinishOnce(t *testing.T) {
	r := New()
	r.Register("a", "a", "queue", time.Second, 4)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); finish := r.Begin("a"); finish("injected"); finish("") }()
	}
	wg.Wait()
	s := r.Snapshot()[0]
	if s.Running != 0 || s.Runs != 100 || s.Failures != 100 || s.State != "idle" {
		t.Fatalf("bad snapshot: %+v", s)
	}
}
