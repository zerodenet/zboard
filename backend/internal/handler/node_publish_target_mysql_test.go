package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"strings"
	"testing"
	"time"
)

func TestMySQLPublicationCannotReplaceOrConfirmADifferentTarget(t *testing.T) {
	h, _ := newMySQLBusinessHandlers(t)
	node := model.Node{Name: "immutable-target", LifecycleStatus: "active", Config: "{}"}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "target endpoint", RuntimeKey: "immutable-target-endpoint", Protocol: "mieru", Address: "node.example.test", Port: 443, PublicPort: 443, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]", IsActive: true}
	if err := h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	s := h.services.ConfigurationPublicationState()
	ctx := t.Context()
	sha, other := strings.Repeat("a", 64), strings.Repeat("b", 64)
	started, err := s.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: node.ID, TriggerEndpointID: endpoint.ID})
	if err != nil {
		t.Fatal(err)
	}
	completion := network.ConfigurationPublicationCompletion{ConfigSHA: sha, LastHealthyAt: time.Now().UTC(), MieruAccess: true, MieruFallbackCount: 1, SuppressMieruFallback: true, ManagedPrincipalAccess: true}
	if _, err := s.Complete(ctx, started.Deployment, completion); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("missing target accepted: %v", err)
	}
	desired, err := s.SetDesired(ctx, started.Deployment, sha)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDesired(ctx, desired, other); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("target replaced: %v", err)
	}
	completion.ConfigSHA = other
	if _, err := s.Complete(ctx, desired, completion); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("wrong result accepted: %v", err)
	}
	completion.ConfigSHA = sha
	stale := desired
	stale.DesiredConfigSHA256 = other
	if _, err := s.Complete(ctx, stale, completion); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatalf("wrong caller accepted: %v", err)
	}
	var current model.Node
	var ep model.ProtocolEndpoint
	var deployment model.ProtocolDeployment
	if err := h.db.First(&current, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&ep, endpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.First(&deployment, desired.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.LastSyncAt != nil || ep.MieruPrincipalReady || deployment.Status != "running" || deployment.AppliedConfigSHA256 != "" || deployment.DesiredConfigSHA256 != sha {
		t.Fatal("mismatched completion committed readiness or application")
	}
	if _, err := s.Complete(ctx, desired, completion); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(ctx, desired, completion); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatal("completed deployment accepted another completion")
	}
}
