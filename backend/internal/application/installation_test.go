package application

import (
	"context"
	"sync"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

type installationRepositoryStub struct {
	mu        sync.Mutex
	installed bool
	reads     int
}

func (s *installationRepositoryStub) Status(context.Context) (platform.InstallationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return platform.InstallationStatus{Installed: s.installed}, nil
}

func (s *installationRepositoryStub) Create(context.Context, platform.InstallationInput, platform.SetupPreferences, string) (platform.InstallationResult, error) {
	s.mu.Lock()
	s.installed = true
	s.mu.Unlock()
	return platform.InstallationResult{}, nil
}

func TestInstallationStateCoalescesReadsAndInvalidatesAfterCreate(t *testing.T) {
	repository := &installationRepositoryStub{}
	services := &Services{}
	services.Installation = platform.Installation{Repository: installationRepository{InstallationRepository: repository, invalidate: services.InvalidateInstallation}}
	for i := 0; i < 8; i++ {
		state, err := services.InstallationState(context.Background(), false)
		if err != nil || state.Installed {
			t.Fatalf("initial state = %+v, %v", state, err)
		}
	}
	repository.mu.Lock()
	reads := repository.reads
	repository.mu.Unlock()
	if reads != 1 {
		t.Fatalf("installation state queried %d times, want one coalesced read", reads)
	}
	if _, err := services.Installation.Create(context.Background(), platform.InstallationInput{SiteName: "ZBoard", SiteURL: "https://example.test", AdminEmail: "admin@example.test", AdminPassword: "long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	state, err := services.InstallationState(context.Background(), false)
	if err != nil || !state.Installed {
		t.Fatalf("post-install state = %+v, %v", state, err)
	}
	repository.mu.Lock()
	reads = repository.reads
	repository.mu.Unlock()
	if reads != 2 {
		t.Fatalf("successful install did not invalidate cache: reads=%d", reads)
	}
}
