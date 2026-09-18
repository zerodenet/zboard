package application

import (
	"context"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

const maintenanceCacheTTL = time.Second

type maintenanceCache struct {
	mu     sync.Mutex
	loaded time.Time
	state  platform.MaintenanceState
	err    error
}

func (s *Services) InvalidateMaintenance() {
	s.maintenance.mu.Lock()
	s.maintenance.loaded = time.Time{}
	s.maintenance.mu.Unlock()
}
func (s *Services) MaintenanceState(ctx context.Context, force bool) (platform.MaintenanceState, error) {
	s.maintenance.mu.Lock()
	defer s.maintenance.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return platform.MaintenanceState{}, err
	}
	if !force && !s.maintenance.loaded.IsZero() && time.Since(s.maintenance.loaded) < maintenanceCacheTTL {
		return s.maintenance.state, s.maintenance.err
	}
	query, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := s.Maintenance.State(query)
	if ctx.Err() != nil {
		return platform.MaintenanceState{}, ctx.Err()
	}
	// Briefly cache failures too: all consumers fail closed without repeatedly
	// exhausting the database connection pool during an outage.
	s.maintenance.state, s.maintenance.err, s.maintenance.loaded = state, err, time.Now()
	return state, err
}
func (s *Services) WorkPaused() bool {
	if !s.isReady() {
		return true
	}
	state, err := s.MaintenanceState(context.Background(), false)
	return err != nil || state.Enabled
}

type maintenanceRepository struct {
	platform.MaintenanceRepository
	invalidate func()
}

func (r maintenanceRepository) Apply(ctx context.Context, actor uint, in platform.MaintenanceUpdate) error {
	err := r.MaintenanceRepository.Apply(ctx, actor, in)
	if err == nil {
		r.invalidate()
	}
	return err
}
func (r maintenanceRepository) Patch(ctx context.Context, actor uint, in platform.MaintenanceSetting) error {
	err := r.MaintenanceRepository.Patch(ctx, actor, in)
	if err == nil {
		r.invalidate()
	}
	return err
}

// Migration admission also changes maintenance state outside HTTP.
type migrationAdmissionRepository struct {
	platform.MigrationRepository
	invalidate func()
}

func (r migrationAdmissionRepository) Admit(ctx context.Context, actor uint, driver, ciphertext string) (platform.MigrationAccepted, error) {
	out, err := r.MigrationRepository.Admit(ctx, actor, driver, ciphertext)
	if err == nil {
		r.invalidate()
	}
	return out, err
}
