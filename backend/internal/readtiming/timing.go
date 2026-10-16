// Package readtiming records bounded, request-local operation timings. It never
// records SQL, parameters, request values, or response contents.
package readtiming

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type key struct{}
type Recorder struct {
	mu        sync.Mutex
	durations map[string]time.Duration
	order     []string
}

func NewContext(ctx context.Context) (context.Context, *Recorder) {
	r := &Recorder{durations: make(map[string]time.Duration)}
	return context.WithValue(ctx, key{}, r), r
}

// Start accepts code-owned metric names, never names derived from request data.
// At most 16 names are kept per request, including concurrent operations.
func Start(ctx context.Context, name string) func() {
	r, _ := ctx.Value(key{}).(*Recorder)
	if r == nil || !validName(name) {
		return func() {}
	}
	began := time.Now()
	var once sync.Once
	return func() { once.Do(func() { r.record(name, time.Since(began)) }) }
}

func Record(ctx context.Context, name string, duration time.Duration) {
	r, _ := ctx.Value(key{}).(*Recorder)
	if r != nil && validName(name) {
		r.record(name, duration)
	}
}
func (r *Recorder) record(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.durations[name]; !exists {
		if len(r.order) >= 16 {
			return
		}
		r.order = append(r.order, name)
	}
	r.durations[name] += duration
}

func validName(name string) bool {
	if name == "" || len(name) > 40 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z') && c != '_' {
			return false
		}
	}
	return true
}

func (r *Recorder) Header() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := make([]string, 0, len(r.order))
	for _, name := range r.order {
		parts = append(parts, fmt.Sprintf("%s;dur=%.3f", name, float64(r.durations[name])/float64(time.Millisecond)))
	}
	return strings.Join(parts, ", ")
}
