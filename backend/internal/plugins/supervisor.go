package plugins

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type restartAttempt struct {
	attempts int
	next     time.Time
}

var ErrInvalidSession = errors.New("plugin session expired or revoked")

// Renewal never waits for the lifecycle mutex or a plugin RPC.
func (m *Manager) renewLease() {
	if m.lost.Load() {
		return
	}
	if err := m.renew(); err != nil {
		m.leaseErrors.Add(1)
		if errors.Is(err, ErrUnavailable) || time.Now().UnixNano() >= m.leaseUntil.Load() {
			m.lost.Store(true)
		}
	}
}
func (m *Manager) stopProcesses() {
	m.cancelPluginTasks()
	for id, p := range m.processes {
		p.close()
		delete(m.processes, id)
	}
	m.sessions = map[string]Session{}
}

// A separate supervisor can wait for process cleanup without delaying renewal.
func (m *Manager) supervise(ctx context.Context) {
	defer close(m.supervisorDone)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.superviseOnce(ctx)
		}
	}
}
func (m *Manager) superviseOnce(ctx context.Context) {
	if !m.mu.TryLock() {
		return
	}
	defer m.syncTaskRegistrations(ctx)
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if m.lost.Load() {
		m.stopProcesses()
		now := time.Now().UTC()
		until := now.Add(time.Minute)
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		res := m.db.WithContext(attempt).Model(&model.PluginHostLease{}).Where("id = 1 AND expires_at <= ?", now).Updates(map[string]any{"owner": m.owner, "epoch": gorm.Expr("epoch + 1"), "expires_at": until})
		if res.Error != nil || res.RowsAffected != 1 {
			return
		}
		var lease model.PluginHostLease
		if err := m.db.WithContext(attempt).Where("id = 1 AND owner = ?", m.owner).First(&lease).Error; err != nil {
			return
		}
		m.epoch.Store(lease.Epoch)
		m.leaseUntil.Store(until.UnixNano())
		m.lost.Store(false)
		if err := m.recoverWithContext(ctx); err != nil {
			m.lost.Store(true)
			m.stopProcesses()
		}
		return
	}
	for token, s := range m.sessions {
		if time.Now().After(s.ExpiresAt) {
			delete(m.sessions, token)
		}
	}
	for id, p := range m.processes {
		if p.client.Exited() {
			p.close()
			delete(m.processes, id)
			m.invalidate(id)
			m.restarts[id] = restartAttempt{next: time.Now().Add(10 * time.Second)}
			_ = m.updateInstallation(id, map[string]any{"state": "failed", "last_error": "plugin process exited"})
		}
	}
	for id, retry := range m.restarts {
		if retry.attempts >= 3 || time.Now().Before(retry.next) {
			continue
		}
		v, err := m.load(id)
		if err != nil {
			continue
		}
		if !v.Enabled || v.State != "failed" {
			delete(m.restarts, id)
			continue
		}
		retry.attempts++
		retry.next = time.Now().Add(time.Duration(1<<retry.attempts) * 15 * time.Second)
		m.restarts[id] = retry
		pack, err := m.packageFor(v)
		if err == nil {
			err = m.commitCandidate(ctx, v.PluginInstallation, pack, "process-recovery", nil)
		}
		if err == nil {
			delete(m.restarts, id)
		} else {
			_ = m.updateInstallation(id, map[string]any{"state": "failed", "last_error": "plugin restart failed; check configuration or retry manually"})
		}
		break // At most one bounded startup per supervision round.
	}

}

type HostStatus struct {
	State           string     `json:"state"`
	Epoch           uint64     `json:"epoch"`
	LeaseUntil      *time.Time `json:"lease_until"`
	RenewalFailures uint64     `json:"renewal_failures"`
	IntervalSeconds int        `json:"interval_seconds"`
}

func (m *Manager) Status() HostStatus {
	state := "active"
	if m.lost.Load() {
		state = "recovering"
	}
	status := HostStatus{State: state, Epoch: m.epoch.Load(), RenewalFailures: m.leaseErrors.Load(), IntervalSeconds: 10}
	if n := m.leaseUntil.Load(); n != 0 {
		until := time.Unix(0, n).UTC()
		status.LeaseUntil = &until
	}
	return status
}
