package platform

import (
	"context"
	"sort"
	"time"
)

// SystemMinute retains aggregation weights, so a graceful restart within the
// same minute resumes the average without duplicating or dropping its samples.
type SystemMinute struct {
	At     time.Time
	Sums   [5]float64
	Counts [5]int
}

func (m SystemMinute) Point() SystemPoint {
	var values [5]*float64
	for i, n := range m.Counts {
		if n > 0 {
			v := m.Sums[i] / float64(n)
			values[i] = &v
		}
	}
	return SystemPoint{At: m.At, CPU: values[0], Memory: values[1], Disk: values[2], RX: values[3], TX: values[4]}
}

type SystemHistoryStore interface {
	Load(context.Context, time.Time) ([]SystemMinute, error)
	Save(context.Context, []SystemMinute, time.Time) error
}
type SystemHistoryStatus struct {
	Persistent     bool       `json:"persistent"`
	State          string     `json:"state"`
	LastSavedAt    *time.Time `json:"last_saved_at"`
	PendingMinutes int        `json:"pending_minutes"`
	Error          string     `json:"error,omitempty"`
}

func (m *RuntimeMonitor) historyStatus() SystemHistoryStatus {
	state := "memory"
	if m.store != nil {
		state = "loading"
		if m.historyLoaded {
			state = "ready"
		}
		if m.historyError != "" {
			state = "error"
		}
	}
	pending := 0
	if m.store != nil {
		pending = len(m.dirty)
	}
	return SystemHistoryStatus{Persistent: m.store != nil, State: state, LastSavedAt: m.lastSavedAt, PendingMinutes: pending, Error: m.historyError}
}
func (m *RuntimeMonitor) restore(now time.Time) bool {
	if m.store == nil {
		return true
	}
	m.mu.Lock()
	loaded := m.historyLoaded
	m.mu.Unlock()
	if loaded {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := m.store.Load(ctx, now)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.historyError = "监控历史恢复失败，正在自动重试。"
		return false
	}
	for _, row := range rows {
		if row.At.Before(now.Add(-24*time.Hour)) || row.At.After(now) || !row.At.Equal(row.At.Truncate(time.Minute)) {
			continue
		}
		key := row.At.Unix()
		if local, ok := m.minutes[key]; ok {
			for i := range row.Sums {
				row.Sums[i] += local.Sums[i]
				row.Counts[i] += local.Counts[i]
			}
		}
		m.minutes[key] = row
		at := row.At
		if m.lastSavedAt == nil || at.After(*m.lastSavedAt) {
			m.lastSavedAt = &at
		}
	}
	keys := make([]int64, 0, len(m.minutes))
	for key := range m.minutes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if len(keys) > 1440 {
		for _, key := range keys[:len(keys)-1440] {
			delete(m.minutes, key)
			delete(m.dirty, key)
		}
		keys = keys[len(keys)-1440:]
	}
	m.points = make([]SystemPoint, 0, 1440)
	for _, key := range keys {
		row := m.minutes[key]
		m.points = append(m.points, row.Point())
		if row.At.Equal(m.bucket) {
			m.sums = row.Sums
			m.counts = row.Counts
		}
	}
	m.expire(now)
	m.historyLoaded = true
	m.historyError = ""
	return true
}

// flush is called by one persistence worker, independently from the sampler.
// Normal writes contain completed minutes only; graceful shutdown also saves
// the current partial minute. A failed write keeps a bounded dirty set.
func (m *RuntimeMonitor) flush(now time.Time, includeCurrent bool) {
	if m.store == nil || !m.restore(now) {
		return
	}
	m.mu.Lock()
	m.expire(now)
	rows := make([]SystemMinute, 0, len(m.dirty))
	versions := make(map[int64]uint64, len(m.dirty))
	for key, revision := range m.dirty {
		row := m.minutes[key]
		if includeCurrent || row.At.Before(now.Truncate(time.Minute)) {
			rows = append(rows, row)
			versions[key] = revision
		}
	}
	skipMaintenance := len(rows) == 0 && m.lastMaintenance.Truncate(time.Minute).Equal(now.Truncate(time.Minute))
	m.mu.Unlock()
	if skipMaintenance {
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := m.store.Save(ctx, rows, now)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.historyError = "监控历史保存失败，正在自动重试；当前显示缓存数据。"
		return
	}
	for key, revision := range versions {
		if m.dirty[key] == revision {
			delete(m.dirty, key)
		}
	}
	m.lastMaintenance = now
	if len(rows) > 0 {
		at := rows[len(rows)-1].At
		if m.lastSavedAt == nil || at.After(*m.lastSavedAt) {
			m.lastSavedAt = &at
		}
	}
	m.historyError = ""
}
func (m *RuntimeMonitor) persistLoop(done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
			m.flush(time.Now().UTC(), false)
		case <-ticker.C:
			m.flush(time.Now().UTC(), false)
		}
	}
}
