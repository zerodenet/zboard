package plugins

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestPluginCapabilityAuthorityFencesSessionAndAdmission(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) {
		m.Capabilities = append(m.Capabilities, MeteringReadCapability, CommerceOrdersReadCapability)
	})
	m, db, _ := testManager(t, keys)
	if err := db.Create(&model.User{ID: 1, Email: "plugin-reader@example.test", Password: "unused", Status: "active", IsAdmin: true}).Error; err != nil {
		t.Fatal(err)
	}
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSession(v.ID, "home", "account", 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	auth := SessionAuthority{Manager: m, UserID: 1, Admin: true}
	proof := catalog.Credential{Kind: "plugin_session", Proof: session.Token}
	grant, err := auth.Resolve(context.Background(), proof, "metering.usage.query")
	if err != nil || grant.Principal.AccountID != 1 || grant.Principal.Kind != "plugin_session" || grant.Principal.PluginID != v.ID || grant.Principal.Generation != v.Generation || grant.Principal.Subject == "" || grant.Administrative {
		t.Fatalf("grant: %+v %v", grant, err)
	}
	if orders, ordersErr := auth.Resolve(context.Background(), proof, "commerce.orders.list"); ordersErr != nil || orders.Principal.Subject != grant.Principal.Subject {
		t.Fatalf("order grant: %+v %v", orders, ordersErr)
	}
	if _, paymentErr := auth.Resolve(context.Background(), proof, "commerce.payments.record"); !errors.Is(paymentErr, catalog.ErrDenied) {
		t.Fatalf("browser payment escalation: %v", paymentErr)
	}
	descriptor := catalog.Descriptor{Name: "metering.usage.query", RateLimitPerMinute: 1}
	if err := auth.Admit(context.Background(), proof, grant, descriptor); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if err = db.Model(&model.User{}).Where("id = 1").Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.Admit(context.Background(), proof, grant, descriptor); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("disabled account admission: %v", err)
	}
	if err = db.Model(&model.User{}).Where("id = 1").Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := auth.Admit(context.Background(), proof, grant, descriptor); !errors.Is(err, catalog.ErrRateLimited) {
		t.Fatalf("rate admission: %v", err)
	}
	other := SessionAuthority{Manager: m, UserID: 2}
	if _, err = other.Resolve(context.Background(), proof, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("session substitution: %v", err)
	}
	if _, err = auth.Resolve(context.Background(), proof, "jobs.submit"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("operation escalation: %v", err)
	}
	public, err := m.CreateSession(v.ID, "home", "public", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Resolve(context.Background(), catalog.Credential{Kind: "plugin_session", Proof: public.Token}, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("public session: %v", err)
	}
	if err = db.Model(&model.User{}).Where("id = 1").Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Resolve(context.Background(), proof, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("disabled account: %v", err)
	}
	if err = db.Model(&model.User{}).Where("id = 1").Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	v, err = m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Resolve(context.Background(), proof, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("disabled plugin: %v", err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Resolve(context.Background(), proof, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("old generation: %v", err)
	}
	fresh, err := m.CreateSession(v.ID, "home", "account", 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Resolve(context.Background(), catalog.Credential{Kind: "plugin_session", Proof: fresh.Token}, "metering.usage.query"); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("withdrawn admission: %v", err)
	}
}
