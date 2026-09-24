package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"golang.org/x/crypto/bcrypt"
)

func TestPluginHostAccountAndMessageProjectionsStayPrincipalScoped(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{
		"password": string(passwordHash), "account_name": "Reader",
	}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	announcement := model.Announcement{
		Title: "Service update", Content: "Private details", Severity: "info", Audience: "user",
		Status: "published", StartsAt: &now, Revision: 1,
	}
	if err := h.db.Create(&announcement).Error; err != nil {
		t.Fatal(err)
	}

	principalRaw, callErr := h.CallPluginHost(context.Background(), "example.plugin", plugins.AccountAssertionCapability, "account.assert", mustJSON(t, pluginv1.AccountAssertionRequest{
		Account: "reader@example.test", Password: "correct-password",
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var principal pluginv1.Principal
	if err := json.Unmarshal(principalRaw, &principal); err != nil || !strings.HasPrefix(principal.ID, "p1.") || principal.Display != "Reader" {
		t.Fatalf("principal = %+v, %v", principal, err)
	}
	pagePrincipal, err := h.PluginPrincipal(context.Background(), "example.plugin", 1)
	if err != nil || pagePrincipal != principal.ID {
		t.Fatalf("page principal = %q, want asserted principal %q: %v", pagePrincipal, principal.ID, err)
	}
	_, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.AccountAssertionCapability, "account.assert", mustJSON(t, pluginv1.AccountAssertionRequest{
		Account: "reader@example.test", Password: "wrong",
	}))
	if callErr == nil || callErr.Code != "invalid_credentials" {
		t.Fatalf("wrong password error = %+v", callErr)
	}

	pageRaw, callErr := h.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.list", mustJSON(t, pluginv1.MessageListRequest{
		PrincipalID: principal.ID, Limit: 20,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var page pluginv1.MessagePage
	if err := json.Unmarshal(pageRaw, &page); err != nil || len(page.Items) != 1 || page.Items[0].Body != "" {
		t.Fatalf("message page = %+v, %v", page, err)
	}
	messageRaw, callErr := h.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.get", mustJSON(t, pluginv1.MessageGetRequest{
		PrincipalID: principal.ID, MessageID: page.Items[0].ID,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var message pluginv1.ProjectedMessage
	if err := json.Unmarshal(messageRaw, &message); err != nil || message.Body != "Private details" {
		t.Fatalf("message = %+v, %v", message, err)
	}
	_, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.mark-read", mustJSON(t, pluginv1.MessageMarkReadRequest{
		PrincipalID: principal.ID, MessageID: message.ID, Revision: message.Revision,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var receipt model.AnnouncementRead
	if err := h.db.Where("announcement_id = ? AND user_id = ?", announcement.ID, 1).First(&receipt).Error; err != nil || receipt.Revision != 1 {
		t.Fatalf("read receipt = %+v, %v", receipt, err)
	}
	_, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.get", mustJSON(t, pluginv1.MessageGetRequest{
		PrincipalID: "999", MessageID: message.ID,
	}))
	if callErr == nil || callErr.Code != "unauthorized" {
		t.Fatalf("foreign principal error = %+v", callErr)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPluginHostSubscriptionUsageIsReadOnlyAndPrincipalScoped(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	principalID, err := h.PluginPrincipal(context.Background(), "example.plugin", 1)
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesRaw, callErr := h.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.capabilities", mustJSON(t, struct{}{}))
	var capabilities pluginv1.SubscriptionCapabilities
	if callErr != nil || json.Unmarshal(capabilitiesRaw, &capabilities) != nil || !capabilities.Usage {
		t.Fatalf("subscription capabilities = %+v, error = %+v", capabilities, callErr)
	}
	other := model.User{Email: "other@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2027, time.January, 2, 3, 4, 5, 0, time.UTC)
	subscription := model.Subscription{UserID: 1, Status: subStatusActive, EndAt: expires, FlowTotal: 1000, FlowUsed: 375}
	if err := h.db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	request := pluginv1.SubscriptionUsageRequest{PrincipalID: principalID, SubscriptionID: strconv.FormatUint(uint64(subscription.ID), 10)}
	raw, callErr := h.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.usage", mustJSON(t, request))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var usage pluginv1.SubscriptionUsage
	if err := json.Unmarshal(raw, &usage); err != nil || usage.UsedBytes != 375 || usage.TotalBytes != 1000 || usage.ExpireAtUnixMs != uint64(expires.UnixMilli()) {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	if err := h.db.Model(&subscription).Update("flow_used", 500).Error; err != nil {
		t.Fatal(err)
	}
	raw, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.usage", mustJSON(t, request))
	if callErr != nil || json.Unmarshal(raw, &usage) != nil || usage.UsedBytes != 500 {
		t.Fatalf("updated usage = %+v, error = %+v", usage, callErr)
	}
	// Exhausted subscriptions remain readable even when no configuration may be projected.
	if err := h.db.Model(&subscription).Update("flow_used", 1200).Error; err != nil {
		t.Fatal(err)
	}
	raw, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.usage", mustJSON(t, request))
	if callErr != nil || json.Unmarshal(raw, &usage) != nil || usage.UsedBytes != 1200 || usage.TotalBytes != 1000 {
		t.Fatalf("exhausted usage = %+v, error = %+v", usage, callErr)
	}
	request.PrincipalID, err = h.PluginPrincipal(context.Background(), "example.plugin", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, callErr = h.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.usage", mustJSON(t, request))
	if callErr == nil || callErr.Code != "not_found" {
		t.Fatalf("foreign subscription error = %+v", callErr)
	}
}

func TestPluginHostRejectsUnverifiedAndCrossPluginPrincipals(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	other := model.User{Email: "other@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	foreignToken, err := h.PluginPrincipal(context.Background(), "other.plugin", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, principalID := range []string{strconv.FormatUint(uint64(other.ID), 10), foreignToken, foreignToken + "x"} {
		calls := []struct {
			capability string
			operation  string
			payload    any
		}{
			{plugins.SubscriptionProjectionCapability, "subscriptions.list", pluginv1.SubscriptionListRequest{PrincipalID: principalID, Format: "znet-sink"}},
			{plugins.SubscriptionProjectionCapability, "subscriptions.get", pluginv1.SubscriptionContentRequest{PrincipalID: principalID, SubscriptionID: "1", Format: "znet-sink"}},
			{plugins.SubscriptionProjectionCapability, "subscriptions.usage", pluginv1.SubscriptionUsageRequest{PrincipalID: principalID, SubscriptionID: "1"}},
			{plugins.MessageProjectionCapability, "messages.list", pluginv1.MessageListRequest{PrincipalID: principalID, Limit: 1}},
			{plugins.MessageProjectionCapability, "messages.get", pluginv1.MessageGetRequest{PrincipalID: principalID, MessageID: "1"}},
			{plugins.MessageProjectionCapability, "messages.mark-read", pluginv1.MessageMarkReadRequest{PrincipalID: principalID, MessageID: "1", Revision: 1}},
		}
		for _, call := range calls {
			_, callErr := h.CallPluginHost(context.Background(), "example.plugin", call.capability, call.operation, mustJSON(t, call.payload))
			if callErr == nil || callErr.Code != "unauthorized" {
				t.Fatalf("%s with principal %q: want unauthorized, got %+v", call.operation, principalID, callErr)
			}
		}
	}
}
