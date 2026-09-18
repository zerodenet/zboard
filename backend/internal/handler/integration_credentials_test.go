package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestIntegrationCredentialsScopeExpiryAndRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	ctx := context.Background()
	service := f.h.services.Integrations()
	issued, err := service.Issue(ctx, 1, identity.IntegrationIssue{Name: "usage client", Scopes: []string{"metering.usage.query"}, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	var stored model.UserAPIToken
	if err = f.h.db.First(&stored, issued.Credential.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenHash == issued.Token || stored.TokenHash != identity.IntegrationTokenHash(issued.Token) {
		t.Fatal("credential storage not hashed")
	}
	authority := f.h.services.IntegrationAuthority()
	credential := catalog.Credential{Kind: "integration", Proof: issued.Token}
	grant, err := authority.Resolve(ctx, credential, "metering.usage.query")
	if err != nil || grant.Principal.AccountID != 1 || grant.Principal.Kind != "integration" || grant.Principal.CredentialID != issued.Credential.ID || grant.Principal.Subject == "" || grant.Administrative {
		t.Fatalf("grant: %+v %v", grant, err)
	}
	admission := f.h.services.IntegrationAdmission()
	descriptor := catalog.Descriptor{Name: "metering.usage.query", RateLimitPerMinute: 2}
	if err := admission.Admit(ctx, credential, grant, descriptor); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if err := admission.Admit(ctx, credential, grant, descriptor); err != nil {
		t.Fatalf("second admission: %v", err)
	}
	if err := admission.Admit(ctx, credential, grant, descriptor); !errors.Is(err, catalog.ErrRateLimited) {
		t.Fatalf("rate admission: %v", err)
	}
	previousWindow := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)
	if err = f.h.db.Model(&model.UserAPIToken{}).Where("id = ?", issued.Credential.ID).Updates(map[string]any{
		"invocation_window_started_at": previousWindow,
		"invocation_window_count":      2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err = admission.Admit(ctx, credential, grant, descriptor); err != nil {
		t.Fatalf("next window admission: %v", err)
	}
	if _, err = authority.Resolve(ctx, credential, "jobs.submit"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("scope bypass: %v", err)
	}
	registry := catalog.New(authority, admission)
	if err = f.h.services.RegisterMeteringCapabilities(registry, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Invoke(ctx, credential, "metering.usage.query", json.RawMessage(`{"from":"2026-09-01T00:00:00Z","to":"2026-09-02T00:00:00Z","bucket":"hour"}`)); err != nil {
		t.Fatal(err)
	}
	if err = service.Revoke(ctx, 99, issued.Credential.ID); !errors.Is(err, identity.ErrPermission) {
		t.Fatalf("foreign revoke: %v", err)
	}
	if err = service.Revoke(ctx, 1, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(ctx, credential, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("revoked: %v", err)
	}
	if err = admission.Admit(ctx, credential, grant, descriptor); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("revoked grant admission: %v", err)
	}
	second, err := service.Issue(ctx, 99, identity.IntegrationIssue{Name: "admin own data", Scopes: []string{"metering.usage.query"}, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	secondCredential := catalog.Credential{Kind: "integration", Proof: second.Token}
	grant, err = authority.Resolve(ctx, secondCredential, "metering.usage.query")
	if err != nil || grant.Administrative {
		t.Fatalf("admin inheritance: %+v %v", grant, err)
	}
	if err = f.h.db.Model(&model.UserAPIToken{}).Where("id = ?", second.Credential.ID).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(ctx, secondCredential, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("expired: %v", err)
	}
	if _, err = service.Issue(ctx, 1, identity.IntegrationIssue{Name: "wildcard", Scopes: []string{"*"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}); !errors.Is(err, identity.ErrIntegrationInput) {
		t.Fatalf("wildcard issued: %v", err)
	}
}
