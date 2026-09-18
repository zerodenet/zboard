package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runtimeConfigurationRepositoryStub struct {
	nodeID    uint
	now       time.Time
	protocols []string
}

func (s *runtimeConfigurationRepositoryStub) LoadRuntimeConfiguration(_ context.Context, nodeID uint, now time.Time, protocols []string) (RuntimeConfigurationSnapshot, error) {
	s.nodeID, s.now, s.protocols = nodeID, now, protocols
	protocols[0] = "changed"
	return RuntimeConfigurationSnapshot{SiteURL: "https://panel.example.test"}, nil
}

func TestRuntimeConfigurationSourceValidatesAndDelegates(t *testing.T) {
	repository := &runtimeConfigurationRepositoryStub{}
	source := RuntimeConfigurationSource{Repository: repository}
	at := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	protocols := []string{"vless", "vmess"}
	snapshot, err := source.Load(context.Background(), 7, at, protocols)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SiteURL == "" || repository.nodeID != 7 || repository.now.Location() != time.UTC {
		t.Fatalf("delegation mismatch: snapshot=%+v node=%d at=%v", snapshot, repository.nodeID, repository.now)
	}
	if protocols[0] != "vless" {
		t.Fatalf("caller protocol slice was mutated: %v", protocols)
	}
	for _, tc := range []struct {
		name      string
		nodeID    uint
		at        time.Time
		protocols []string
	}{
		{name: "node", at: at, protocols: protocols},
		{name: "time", nodeID: 1, protocols: protocols},
		{name: "protocols", nodeID: 1, at: at},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := source.Load(context.Background(), tc.nodeID, tc.at, tc.protocols)
			if !errors.Is(err, ErrRuntimeConfigurationInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := (RuntimeConfigurationSource{}).Load(context.Background(), 1, at, protocols); !errors.Is(err, ErrRuntimeConfigurationUnavailable) {
		t.Fatalf("unavailable error = %v", err)
	}
}
