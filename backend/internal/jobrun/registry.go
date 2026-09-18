// Package jobrun records bounded, process-local execution observations.
// Durable queue ownership remains with the business executor.
package jobrun

import (
	"sort"
	"sync"
	"time"
)

type Snapshot struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	IntervalSeconds float64    `json:"interval_seconds"`
	Concurrency     int        `json:"concurrency"`
	State           string     `json:"state"`
	Running         int        `json:"running"`
	Runs            uint64     `json:"runs"`
	Failures        uint64     `json:"failures"`
	MissedRuns      uint64     `json:"missed_runs"`
	TimeZone        string     `json:"timezone,omitempty"`
	MisfirePolicy   string     `json:"misfire_policy,omitempty"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastFinishedAt  *time.Time `json:"last_finished_at"`
	LastDurationMS  int64      `json:"last_duration_ms"`
	LastResult      string     `json:"last_result"`
	LastError       string     `json:"last_error"`
	NextScanAt      *time.Time `json:"next_scan_at"`
}
type Registry struct {
	mu   sync.Mutex
	rows map[string]Snapshot
}

func New() *Registry { return &Registry{rows: map[string]Snapshot{}} }
func (r *Registry) Register(id, name, kind string, interval time.Duration, concurrency int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[id]; ok {
		return
	}
	r.rows[id] = Snapshot{ID: id, Name: name, Kind: kind, IntervalSeconds: interval.Seconds(), Concurrency: concurrency, State: "idle", LastResult: "never"}
}
func (r *Registry) Begin(id string) func(string) {
	started := time.Now().UTC()
	r.mu.Lock()
	v := r.rows[id]
	v.Running++
	v.Runs++
	v.LastStartedAt = &started
	v.NextScanAt = nil
	v.State = "running"
	r.rows[id] = v
	r.mu.Unlock()
	var once sync.Once
	return func(message string) {
		once.Do(func() {
			finished := time.Now().UTC()
			r.mu.Lock()
			defer r.mu.Unlock()
			v := r.rows[id]
			v.Running--
			v.LastFinishedAt = &finished
			v.LastDurationMS = finished.Sub(started).Milliseconds()
			v.LastError = message
			v.LastResult = "succeeded"
			if message != "" {
				v.Failures++
				v.LastResult = "failed"
			}
			if v.Running == 0 && v.State != "stopped" {
				v.State = "idle"
			}
			r.rows[id] = v
		})
	}
}
func (r *Registry) Waiting(id, state string, next *time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.rows[id]
	if !ok {
		return
	}
	if v.Running == 0 {
		v.State = state
	}
	v.NextScanAt = next
	r.rows[id] = v
}
func (r *Registry) Snapshot() []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Snapshot, 0, len(r.rows))
	for _, v := range r.rows {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
