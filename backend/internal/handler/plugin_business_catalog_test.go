package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type pluginBusinessTestAuthority struct{ grant catalog.Grant }

func (a pluginBusinessTestAuthority) Resolve(context.Context, catalog.Credential, string) (catalog.Grant, error) {
	return a.grant, nil
}

type scopedConfigProjector struct{ ids []uint }

func (*scopedConfigProjector) SupportsPluginSubscriptionFormat(format string) bool {
	return format == "json"
}
func (p *scopedConfigProjector) ProjectPluginSubscription(_ context.Context, summary entitlements.SubscriptionSummary, format string, _ time.Time) (pluginv1.ProjectedSubscription, error) {
	p.ids = append(p.ids, summary.ID)
	return pluginv1.ProjectedSubscription{ID: jsonNumber(summary.ID), DisplayName: summary.PlanName, Format: format, Revision: "revision-1", ContentSHA256: "digest", Content: "bounded content"}, nil
}

func TestPluginConfigurationProjectionRequiresOwnedActiveSubscription(t *testing.T) {
	f := newOrderFixture(t)
	other := model.User{Email: "other-config@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, sub := range []model.Subscription{
		{ID: 301, UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.Add(time.Hour), FlowTotal: 100, Config: "{}"},
		{ID: 302, UserID: other.ID, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.Add(time.Hour), FlowTotal: 100, Config: "{}"},
	} {
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	projector := &scopedConfigProjector{}
	authority := pluginBusinessTestAuthority{grant: catalog.Grant{Principal: catalog.Principal{Kind: "plugin_session", Subject: "plugin:reader", AccountID: 1, PluginID: "example.reader", Generation: 1}}}
	registry := catalog.New(authority, meteringCatalogAdmission{})
	if err := f.h.services.RegisterPluginBusinessCapabilities(registry, projector); err != nil {
		t.Fatal(err)
	}
	invoke := func(body string) (any, error) {
		return registry.Invoke(context.Background(), catalog.Credential{}, "subscriptions.owned.config", json.RawMessage(body))
	}
	if _, err := invoke(`{"id":302,"format":"json"}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("foreign projection: %v", err)
	}
	if len(projector.ids) != 0 {
		t.Fatalf("foreign subscription reached projector: %+v", projector.ids)
	}
	result, err := invoke(`{"id":301,"format":"json"}`)
	if err != nil {
		t.Fatal(err)
	}
	projected := result.(pluginv1.ProjectedSubscription)
	if projected.Content != "bounded content" || len(projector.ids) != 1 || projector.ids[0] != 301 {
		t.Fatalf("owned projection: %+v %+v", projected, projector.ids)
	}
	result, err = invoke(`{"id":301,"format":"json","known_revision":"revision-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	projected = result.(pluginv1.ProjectedSubscription)
	if projected.Content != "" || !projected.NotModified {
		t.Fatalf("revision projection: %+v", projected)
	}
}

func TestNativePluginBusinessCallsRequireBoundPrincipalAndExactCapability(t *testing.T) {
	f := newOrderFixture(t)
	host := application.NewPluginHostServices(f.h.services, nil)
	owner, err := host.PluginPrincipal(context.Background(), "example.alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	otherPlugin, err := host.PluginPrincipal(context.Background(), "example.beta", 1)
	if err != nil {
		t.Fatal(err)
	}
	call := func(capability, operation, principal string, fields map[string]any) (json.RawMessage, *pluginv1.HostCallError) {
		input := map[string]any{"principal_id": principal}
		for key, value := range fields {
			input[key] = value
		}
		raw, _ := json.Marshal(input)
		return host.CallPluginHost(context.Background(), "example.alpha", capability, operation, raw)
	}
	account, callErr := call(pluginv1.AccountSelfReadCapability, "account.self.get", owner, nil)
	if callErr != nil || !strings.Contains(string(account), "reader@example.test") {
		t.Fatalf("self read: %s %+v", account, callErr)
	}
	if _, callErr := call(pluginv1.AccountSelfReadCapability, "account.admin.get", owner, map[string]any{"user_id": 1}); callErr == nil || callErr.Code != "forbidden" {
		t.Fatalf("wrong capability: %+v", callErr)
	}
	if _, callErr := call(pluginv1.AccountSelfReadCapability, "account.self.get", otherPlugin, nil); callErr == nil || callErr.Code != "unauthorized" {
		t.Fatalf("cross-plugin principal: %+v", callErr)
	}
	if _, callErr := call(pluginv1.SubscriptionReadCapability, "subscriptions.owned.list", owner, map[string]any{"user_id": 2}); callErr == nil || callErr.Code != "forbidden" {
		t.Fatalf("cross-user subscription: %+v", callErr)
	}
	if _, callErr := call(pluginv1.SubscriptionQuotaWriteCapability, "subscriptions.quota.adjust", owner, map[string]any{"subscription_id": 1, "delta_mb": 10, "reason": "quota", "idempotency_key": "native"}); callErr == nil || callErr.Code != "invalid_request" {
		t.Fatalf("nonexistent target: %+v", callErr)
	}
}

func TestPluginMessageCatalogFiltersAudienceAndAcknowledgesRevision(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	authority := pluginBusinessTestAuthority{grant: catalog.Grant{Principal: catalog.Principal{Kind: "plugin_session", Subject: "plugin:reader", AccountID: 1, PluginID: "example.reader", Generation: 1}}}
	registry := catalog.New(authority, meteringCatalogAdmission{})
	if err := h.services.RegisterPluginBusinessCapabilities(registry, h); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	userMessage := model.Announcement{Title: "Visible", Content: "Account content", Severity: "info", Audience: "user", Status: "published", StartsAt: &now, Revision: 1}
	adminMessage := model.Announcement{Title: "Hidden", Content: "Admin content", Severity: "info", Audience: "admin", Status: "published", StartsAt: &now, Revision: 1}
	for _, message := range []*model.Announcement{&userMessage, &adminMessage} {
		if err := h.db.Create(message).Error; err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(name, body string) (any, error) {
		return registry.Invoke(context.Background(), catalog.Credential{}, name, json.RawMessage(body))
	}
	page, err := invoke("messages.owned.list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	pageJSON, _ := json.Marshal(page)
	if !strings.Contains(string(pageJSON), "Visible") || strings.Contains(string(pageJSON), "Hidden") || strings.Contains(string(pageJSON), "Account content") {
		t.Fatalf("unsafe message page: %s", pageJSON)
	}
	if _, err := invoke("messages.owned.get", `{"id":`+jsonNumber(adminMessage.ID)+`}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("admin message leaked: %v", err)
	}
	detail, err := invoke("messages.owned.get", `{"id":`+jsonNumber(userMessage.ID)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	detailJSON, _ := json.Marshal(detail)
	if !strings.Contains(string(detailJSON), "Account content") {
		t.Fatalf("message body missing: %s", detailJSON)
	}
	if _, err := invoke("messages.owned.ack", `{"id":`+jsonNumber(userMessage.ID)+`,"revision":2}`); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err := invoke("messages.owned.ack", `{"id":`+jsonNumber(userMessage.ID)+`,"revision":1}`); err != nil {
		t.Fatal(err)
	}
	page, err = invoke("messages.owned.list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	pageJSON, _ = json.Marshal(page)
	if !strings.Contains(string(pageJSON), `"read":true`) {
		t.Fatalf("read receipt missing: %s", pageJSON)
	}
}

func jsonNumber(value uint) string { return strconv.FormatUint(uint64(value), 10) }

func TestPluginBusinessCatalogUsesDomainOwnershipAndSeparateWriteGrant(t *testing.T) {
	f := newOrderFixture(t)
	other := model.User{Email: "other@example.test", Password: "unused", Status: "active"}
	if err := f.h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, sub := range []model.Subscription{
		{ID: 201, UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.Add(24 * time.Hour), FlowTotal: 1000, Config: "{}"},
		{ID: 202, UserID: other.ID, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now, EndAt: now.Add(24 * time.Hour), FlowTotal: 1000, Config: "{}"},
	} {
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	authority := pluginBusinessTestAuthority{grant: catalog.Grant{Principal: catalog.Principal{Kind: "plugin_session", Subject: "plugin:test", AccountID: 1, PluginID: "example.reader", Generation: 1}}}
	registry := catalog.New(authority, meteringCatalogAdmission{})
	if err := f.h.services.RegisterPluginBusinessCapabilities(registry, f.h); err != nil {
		t.Fatal(err)
	}
	invoke := func(name, body string) (any, error) {
		return registry.Invoke(context.Background(), catalog.Credential{}, name, json.RawMessage(body))
	}
	self, err := invoke("account.self.get", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	selfJSON, _ := json.Marshal(self)
	if !strings.Contains(string(selfJSON), "reader@example.test") || strings.Contains(string(selfJSON), "password") {
		t.Fatalf("unsafe self account: %s", selfJSON)
	}
	if _, err := invoke("account.self.get", `{"user_id":2}`); !errors.Is(err, catalog.ErrInput) {
		t.Fatalf("self target substitution: %v", err)
	}
	page, err := invoke("subscriptions.owned.list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	pageJSON, _ := json.Marshal(page)
	if !strings.Contains(string(pageJSON), `"id":201`) || strings.Contains(string(pageJSON), `"id":202`) {
		t.Fatalf("cross-user list: %s", pageJSON)
	}
	if _, err := invoke("subscriptions.owned.get", `{"id":202}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("cross-user detail: %v", err)
	}
	if _, err := invoke("subscriptions.owned.list", `{"user_id":2}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("cross-user filter: %v", err)
	}
	if _, err := invoke("subscriptions.quota.adjust", `{"subscription_id":201,"delta_mb":10,"reason":"test quota","idempotency_key":"one"}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("read grant wrote quota: %v", err)
	}
	if _, err := invoke("account.admin.get", `{"user_id":2}`); !errors.Is(err, catalog.ErrDenied) {
		t.Fatalf("read grant accessed admin profile: %v", err)
	}
	authority.grant.Administrative = true
	registry = catalog.New(authority, meteringCatalogAdmission{})
	if err := f.h.services.RegisterPluginBusinessCapabilities(registry, f.h); err != nil {
		t.Fatal(err)
	}
	first, err := invoke("subscriptions.quota.adjust", `{"subscription_id":201,"delta_mb":10,"reason":"test quota","idempotency_key":"one"}`)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("missing quota receipt")
	}
	replay, err := invoke("subscriptions.quota.adjust", `{"subscription_id":201,"delta_mb":10,"reason":"test quota","idempotency_key":"one"}`)
	if err != nil {
		t.Fatalf("quota replay: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	replayJSON, _ := json.Marshal(replay)
	if string(firstJSON) != string(replayJSON) {
		t.Fatalf("quota replay diverged: %s %s", firstJSON, replayJSON)
	}
	if _, err := invoke("subscriptions.quota.adjust", `{"subscription_id":201,"delta_mb":11,"reason":"test quota","idempotency_key":"one"}`); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("changed quota request reused key: %v", err)
	}
	var count int64
	if err := f.h.db.Model(&model.Task{}).Where("type = ?", "quota").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("quota tasks: %d %v", count, err)
	}
	if err := f.h.db.Model(&model.AuditLog{}).Where("action = ? AND detail LIKE ?", "task.create", "%origin=plugin:example.reader%").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("quota audit: %d %v", count, err)
	}
	profile, err := invoke("account.admin.get", `{"user_id":2}`)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, _ := json.Marshal(profile)
	if !strings.Contains(string(profileJSON), "other@example.test") || strings.Contains(string(profileJSON), "password") {
		t.Fatalf("unsafe admin account: %s", profileJSON)
	}
	extended, err := invoke("subscriptions.term.extend", `{"subscription_id":201,"days":3,"reason":"support extension","idempotency_key":"term-one"}`)
	if err != nil {
		t.Fatal(err)
	}
	extendJSON, _ := json.Marshal(extended)
	if !strings.Contains(string(extendJSON), `"status":"active"`) {
		t.Fatalf("term receipt: %s", extendJSON)
	}
	replayed, err := invoke("subscriptions.term.extend", `{"subscription_id":201,"days":3,"reason":"support extension","idempotency_key":"term-one"}`)
	if err != nil {
		t.Fatal(err)
	}
	extendReplayJSON, _ := json.Marshal(replayed)
	if string(extendReplayJSON) != string(extendJSON) {
		t.Fatalf("term replay diverged: %s %s", extendJSON, extendReplayJSON)
	}
	if _, err := invoke("subscriptions.term.extend", `{"subscription_id":201,"days":4,"reason":"support extension","idempotency_key":"term-one"}`); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("term conflict: %v", err)
	}
	if _, err := invoke("subscriptions.status.cancel", `{"subscription_id":201,"reason":"support cancellation","idempotency_key":"status-one"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke("subscriptions.term.extend", `{"subscription_id":201,"days":1,"reason":"too late","idempotency_key":"term-two"}`); !errors.Is(err, catalog.ErrInput) {
		t.Fatalf("canceled subscription extended: %v", err)
	}
}
