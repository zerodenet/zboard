package application

import (
	"context"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

const installationCacheTTL = time.Second

type installationCache struct {
	mu     sync.Mutex
	loaded time.Time
	state  platform.InstallationStatus
	err    error
}

func (s *Services) InvalidateInstallation() {
	s.installation.mu.Lock()
	s.installation.loaded = time.Time{}
	s.installation.mu.Unlock()
}

// InstallationState coalesces the installation guard shared by every API
// request. Failures are briefly cached too, so an unavailable database cannot
// turn request concurrency into a connection-pool stampede.
func (s *Services) InstallationState(ctx context.Context, force bool) (platform.InstallationStatus, error) {
	s.installation.mu.Lock()
	defer s.installation.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return platform.InstallationStatus{}, err
	}
	if !force && !s.installation.loaded.IsZero() && time.Since(s.installation.loaded) < installationCacheTTL {
		return s.installation.state, s.installation.err
	}
	query, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := s.Installation.Status(query)
	if ctx.Err() != nil {
		return platform.InstallationStatus{}, ctx.Err()
	}
	s.installation.state, s.installation.err, s.installation.loaded = state, err, time.Now()
	return state, err
}

type installationRepository struct {
	platform.InstallationRepository
	invalidate func()
}

func (r installationRepository) Create(ctx context.Context, in platform.InstallationInput, preferences platform.SetupPreferences, hash string) (platform.InstallationResult, error) {
	result, err := r.InstallationRepository.Create(ctx, in, preferences, hash)
	if err == nil && r.invalidate != nil {
		r.invalidate()
	}
	return result, err
}
