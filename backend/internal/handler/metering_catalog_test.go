package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"testing"
)

type meteringCatalogAuthority struct{ revoked bool }

func (a *meteringCatalogAuthority) Resolve(context.Context, catalog.Credential, string) (catalog.Grant, error) {
	if a.revoked {
		return catalog.Grant{}, catalog.ErrDenied
	}
	return catalog.Grant{Principal: catalog.Principal{Kind: "integration", Subject: "integration:1", AccountID: 1, CredentialID: 1}}, nil
}

type meteringCatalogAdmission struct{}

func (meteringCatalogAdmission) Admit(context.Context, catalog.Credential, catalog.Grant, catalog.Descriptor) error {
	return nil
}

func TestMeteringCatalogUsesScopedCapability(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	auth := &meteringCatalogAuthority{}
	registry := catalog.New(auth, meteringCatalogAdmission{})
	if err := f.h.services.RegisterMeteringCapabilities(registry, nil, nil); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"from":"2026-09-01T00:00:00Z","to":"2026-09-02T00:00:00Z","bucket":"hour","include_totals":true}`)
	out, err := registry.Invoke(context.Background(), catalog.Credential{}, "metering.usage.query", raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Items []struct {
			UserID uint `json:"user_id"`
		}
		Statistics struct {
			Aggregates struct {
				UsedBytes int64 `json:"used_bytes"`
			}
		}
	}
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 || result.Statistics.Aggregates.UsedBytes != 210 {
		t.Fatalf("wrong result: %s", encoded)
	}
	for _, item := range result.Items {
		if item.UserID != 1 {
			t.Fatal("foreign account")
		}
	}
	for _, bad := range []string{`{"administrative":true}`, `{"from":"2026-09-01T00:00:00Z","to":"2026-09-02T00:00:00Z","bucket":"hour","user_id":2}`} {
		if _, err = registry.Invoke(context.Background(), catalog.Credential{}, "metering.usage.query", json.RawMessage(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	auth.revoked = true
	if _, err = registry.Invoke(context.Background(), catalog.Credential{}, "metering.usage.query", raw); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("revoked: %v", err)
	}
}
