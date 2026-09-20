package handler

import (
	"context"
	"encoding/json"
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
	if err := json.Unmarshal(principalRaw, &principal); err != nil || principal.ID != "1" || principal.Display != "reader@example.test" {
		t.Fatalf("principal = %+v, %v", principal, err)
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
