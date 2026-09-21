package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/application"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"golang.org/x/crypto/bcrypt"
)

type recordingPluginSubscriptionProjector struct{ ids []uint }

func (*recordingPluginSubscriptionProjector) SupportsPluginSubscriptionFormat(value string) bool {
	return value == "znet-sink"
}

func (p *recordingPluginSubscriptionProjector) ProjectPluginSubscription(_ context.Context, summary entitlements.SubscriptionSummary, format string, _ time.Time) (pluginv1.ProjectedSubscription, error) {
	p.ids = append(p.ids, summary.ID)
	return pluginv1.ProjectedSubscription{ID: strconv.FormatUint(uint64(summary.ID), 10), DisplayName: summary.PlanName, Format: format, Revision: "revision", ContentSHA256: "digest", Content: "content"}, nil
}

func TestPluginHostAccountAndMessageProjectionsStayPrincipalScoped(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	host := application.NewPluginHostServices(h.services, h)
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

	principalRaw, callErr := host.CallPluginHost(context.Background(), "example.plugin", plugins.AccountAssertionCapability, "account.assert", mustJSON(t, pluginv1.AccountAssertionRequest{
		Account: "reader@example.test", Password: "correct-password",
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var principal pluginv1.Principal
	if err := json.Unmarshal(principalRaw, &principal); err != nil || !strings.HasPrefix(principal.ID, "p1.") || principal.ID == "1" || principal.Display != "reader@example.test" {
		t.Fatalf("principal = %+v, %v", principal, err)
	}
	if strings.Contains(string(principalRaw), "correct-password") || strings.Contains(string(principalRaw), "token") || strings.Contains(string(principalRaw), "hash") {
		t.Fatalf("principal exposed credentials: %s", principalRaw)
	}
	pagePrincipal, err := host.PluginPrincipal(context.Background(), "example.plugin", 1)
	if err != nil || pagePrincipal != principal.ID {
		t.Fatalf("page action principal=%q, asserted principal=%q, err=%v", pagePrincipal, principal.ID, err)
	}
	_, callErr = host.CallPluginHost(context.Background(), "example.plugin", plugins.AccountAssertionCapability, "account.assert", mustJSON(t, pluginv1.AccountAssertionRequest{
		Account: "reader@example.test", Password: "wrong",
	}))
	if callErr == nil || callErr.Code != "invalid_credentials" {
		t.Fatalf("wrong password error = %+v", callErr)
	}

	pageRaw, callErr := host.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.list", mustJSON(t, pluginv1.MessageListRequest{
		PrincipalID: principal.ID, Limit: 20,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var page pluginv1.MessagePage
	if err := json.Unmarshal(pageRaw, &page); err != nil || len(page.Items) != 1 || page.Items[0].Body != "" {
		t.Fatalf("message page = %+v, %v", page, err)
	}
	messageRaw, callErr := host.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.get", mustJSON(t, pluginv1.MessageGetRequest{
		PrincipalID: principal.ID, MessageID: page.Items[0].ID,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var message pluginv1.ProjectedMessage
	if err := json.Unmarshal(messageRaw, &message); err != nil || message.Body != "Private details" {
		t.Fatalf("message = %+v, %v", message, err)
	}
	_, callErr = host.CallPluginHost(context.Background(), "example.plugin", plugins.MessageProjectionCapability, "messages.mark-read", mustJSON(t, pluginv1.MessageMarkReadRequest{
		PrincipalID: principal.ID, MessageID: message.ID, Revision: message.Revision,
	}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var receipt model.AnnouncementRead
	if err := h.db.Where("announcement_id = ? AND user_id = ?", announcement.ID, 1).First(&receipt).Error; err != nil || receipt.Revision != 1 {
		t.Fatalf("read receipt = %+v, %v", receipt, err)
	}
	_, callErr = host.CallPluginHost(context.Background(), "other.plugin", plugins.MessageProjectionCapability, "messages.get", mustJSON(t, pluginv1.MessageGetRequest{
		PrincipalID: principal.ID, MessageID: message.ID,
	}))
	if callErr == nil || callErr.Code != "unauthorized" {
		t.Fatalf("cross-plugin principal error = %+v", callErr)
	}
}

func TestPluginHostSubscriptionProjectionCannotCrossPrincipalOwnership(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("password", string(passwordHash)).Error; err != nil {
		t.Fatal(err)
	}
	other := model.User{Email: "other@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := []model.Subscription{
		{UserID: 1, Status: subStatusActive, StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 1},
		{UserID: other.ID, Status: subStatusActive, StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 1},
	}
	if err := h.db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	projector := &recordingPluginSubscriptionProjector{}
	host := application.NewPluginHostServices(h.services, projector)
	principalRaw, callErr := host.CallPluginHost(context.Background(), "example.plugin", plugins.AccountAssertionCapability, "account.assert", mustJSON(t, pluginv1.AccountAssertionRequest{Account: "reader@example.test", Password: "correct-password"}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var principal pluginv1.Principal
	if err := json.Unmarshal(principalRaw, &principal); err != nil {
		t.Fatal(err)
	}

	listRaw, callErr := host.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.list", mustJSON(t, pluginv1.SubscriptionListRequest{PrincipalID: principal.ID, Format: "znet-sink"}))
	if callErr != nil {
		t.Fatal(callErr)
	}
	var list []pluginv1.ProjectedSubscription
	if err := json.Unmarshal(listRaw, &list); err != nil || len(list) != 1 || list[0].ID != strconv.FormatUint(uint64(records[0].ID), 10) {
		t.Fatalf("owned subscriptions = %+v, %v", list, err)
	}
	if len(projector.ids) != 1 || projector.ids[0] != records[0].ID {
		t.Fatalf("projector saw subscriptions outside principal scope: %v", projector.ids)
	}

	_, callErr = host.CallPluginHost(context.Background(), "example.plugin", plugins.SubscriptionProjectionCapability, "subscriptions.get", mustJSON(t, pluginv1.SubscriptionContentRequest{
		PrincipalID: principal.ID, SubscriptionID: strconv.FormatUint(uint64(records[1].ID), 10), Format: "znet-sink",
	}))
	if callErr == nil || callErr.Code != "not_found" {
		t.Fatalf("foreign subscription error = %+v", callErr)
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
