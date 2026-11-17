package platform

import (
	"sync"
	"time"
)

type ResourceUsage struct {
	Used      uint64 `json:"used"`
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
}
type SystemSample struct {
	At         time.Time      `json:"at"`
	CPU        *float64       `json:"cpu_percent"`
	CPUCount   int            `json:"cpu_count"`
	Load1      *float64       `json:"load_1"`
	Memory     *ResourceUsage `json:"memory"`
	Disk       *ResourceUsage `json:"disk"`
	ProcessCPU *float64       `json:"process_cpu_cores"`
	ProcessRSS *uint64        `json:"process_rss"`
	Heap       uint64         `json:"heap_bytes"`
	Goroutines int            `json:"goroutines"`
	RX         *float64       `json:"rx_bytes_per_second"`
	TX         *float64       `json:"tx_bytes_per_second"`
	Issues     []string       `json:"issues"`
}
type SystemPoint struct {
	At     time.Time `json:"at"`
	CPU    *float64  `json:"cpu_percent"`
	Memory *float64  `json:"memory_percent"`
	Disk   *float64  `json:"disk_percent"`
	RX     *float64  `json:"rx_bytes_per_second"`
	TX     *float64  `json:"tx_bytes_per_second"`
}
type SystemSnapshot struct {
	Current           *SystemSample       `json:"current"`
	Points            []SystemPoint       `json:"points"`
	RangeMinutes      int                 `json:"range_minutes"`
	SampleSeconds     int                 `json:"sample_seconds"`
	ResolutionSeconds int                 `json:"resolution_seconds"`
	Scope             string              `json:"scope"`
	History           SystemHistoryStatus `json:"history"`
}
type SystemReader interface{ Read() SystemSample }

// RuntimeMonitor has one sampler per application instance. Only 1440 minute
// aggregates are cached; durable minute storage is provided through a port.
type RuntimeMonitor struct {
	mu              sync.Mutex
	store           SystemHistoryStore
	minutes         map[int64]SystemMinute
	dirty           map[int64]uint64
	revision        uint64
	historyLoaded   bool
	historyError    string
	lastSavedAt     *time.Time
	lastMaintenance time.Time
	wake            chan struct{}
	reader          SystemReader
	current         *SystemSample
	points          []SystemPoint
	bucket          time.Time
	sums            [5]float64
	counts          [5]int
	stop            chan struct{}
	done            chan struct{}
	closed          bool
}

func NewRuntimeMonitor(reader SystemReader, history ...SystemHistoryStore) *RuntimeMonitor {
	m := &RuntimeMonitor{reader: reader, minutes: map[int64]SystemMinute{}, dirty: map[int64]uint64{}, wake: make(chan struct{}, 1)}
	if len(history) > 0 {
		m.store = history[0]
	}
	return m
}
func (m *RuntimeMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stop != nil || m.closed {
		return
	}
	m.stop, m.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(m.done)
		m.restore(time.Now().UTC())
		m.collect()
		persistenceDone := make(chan struct{})
		go m.persistLoop(persistenceDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				<-persistenceDone
				m.flush(time.Now().UTC(), true)
				return
			case <-ticker.C:
				m.collect()
			}
		}
	}()
}
func (m *RuntimeMonitor) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		if m.stop != nil {
			close(m.stop)
		}
	}
	done := m.done
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}
func percent(u *ResourceUsage) *float64 {
	if u == nil || u.Total == 0 {
		return nil
	}
	v := float64(u.Used) * 100 / float64(u.Total)
	return &v
}
func (m *RuntimeMonitor) collect() {
	sample := m.reader.Read()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = &sample
	m.expire(sample.At)
	bucket := sample.At.UTC().Truncate(time.Minute)
	previousBucket := m.bucket
	if !bucket.Equal(m.bucket) || len(m.points) == 0 {
		m.bucket = bucket
		m.sums = [5]float64{}
		m.counts = [5]int{}
		if previous, ok := m.minutes[bucket.Unix()]; ok {
			m.sums = previous.Sums
			m.counts = previous.Counts
		}
		hasBucket := len(m.points) > 0 && m.points[len(m.points)-1].At.Equal(bucket)
		if len(m.points) == 1440 && !hasBucket {
			delete(m.minutes, m.points[0].At.Unix())
			delete(m.dirty, m.points[0].At.Unix())
			copy(m.points, m.points[1:])
			m.points = m.points[:1439]
		}
		if m.points == nil {
			m.points = make([]SystemPoint, 0, 1440)
		}
		if !hasBucket {
			m.points = append(m.points, SystemPoint{At: bucket})
		}
	}
	values := [5]*float64{sample.CPU, percent(sample.Memory), percent(sample.Disk), sample.RX, sample.TX}
	var avg [5]*float64
	for i, v := range values {
		if v != nil {
			m.sums[i] += *v
			m.counts[i]++
		}
		if m.counts[i] > 0 {
			a := m.sums[i] / float64(m.counts[i])
			avg[i] = &a
		}
	}
	m.revision++
	m.minutes[bucket.Unix()] = SystemMinute{At: bucket, Sums: m.sums, Counts: m.counts}
	m.dirty[bucket.Unix()] = m.revision
	if m.store != nil && !previousBucket.IsZero() && !previousBucket.Equal(bucket) {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	m.points[len(m.points)-1] = SystemPoint{At: bucket, CPU: avg[0], Memory: avg[1], Disk: avg[2], RX: avg[3], TX: avg[4]}
}

// expire is called with the mutex held. Count and age limits are independent:
// after a sampling gap, expired buckets must not wait for 1440 new samples.
func (m *RuntimeMonitor) expire(now time.Time) {
	cutoff := now.Add(-24 * time.Hour)
	first := 0
	for first < len(m.points) && m.points[first].At.Before(cutoff) {
		first++
	}
	if first > 0 {
		for _, point := range m.points[:first] {
			delete(m.minutes, point.At.Unix())
			delete(m.dirty, point.At.Unix())
		}
		remaining := copy(m.points, m.points[first:])
		clear(m.points[remaining:])
		m.points = m.points[:remaining]
	}
	if m.current != nil && m.current.At.Before(cutoff) {
		m.current = nil
	}
}
func (m *RuntimeMonitor) Snapshot(minutes int, now time.Time) SystemSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire(now)
	out := SystemSnapshot{Current: m.current, Points: []SystemPoint{}, RangeMinutes: minutes, SampleSeconds: 5, ResolutionSeconds: 60, Scope: "instance", History: m.historyStatus()}
	cutoff := now.Add(-time.Duration(minutes) * time.Minute)
	for _, p := range m.points {
		if !p.At.Before(cutoff) && !p.At.After(now) {
			out.Points = append(out.Points, p)
		}
	}
	return out
}
