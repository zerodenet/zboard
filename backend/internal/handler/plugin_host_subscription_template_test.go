package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestPluginProjectionUsesSavedClientTemplateAndTracksChanges(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	if err := h.db.Save(&model.Installation{
		ID: 1, SiteName: "Panel", SiteURL: "https://panel.example.test", InstalledAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	customization := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	customization.PolicyGroups[0].IncludeGroups = append(customization.PolicyGroups[0].IncludeGroups, "special")
	customization.PolicyGroups = append(customization.PolicyGroups, subscriptionPolicyGroup{
		ID: "special", Name: "专线优选", Type: "select",
	})
	raw, err := json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	template := model.SubscriptionTemplate{
		Name: "ZNet Sink", Slug: subscriptionRendererZnetSink, Renderer: subscriptionRendererZnetSink,
		Customization: raw, IsActive: true, Revision: 1,
	}
	if err := h.db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	data := sampleSubscriptionTemplateData()
	first, deliveryFormat, err := h.renderPluginSubscriptionTemplate(context.Background(), subscriptionRendererZnetSink, data)
	if err != nil {
		t.Fatal(err)
	}
	if deliveryFormat != "zero" {
		t.Fatalf("unexpected delivery format: %q", deliveryFormat)
	}
	decoded, err := base64.StdEncoding.DecodeString(first)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OutboundGroups []struct {
			Tag string `json:"tag"`
		} `json:"outbound_groups"`
	}
	if err := json.Unmarshal(decoded, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.OutboundGroups) != 3 || document.OutboundGroups[2].Tag != "专线优选" {
		t.Fatalf("plugin projection lost the saved policy group: %+v", document.OutboundGroups)
	}
	customization.PolicyGroups[2].Name = "新的专线优选"
	raw, err = json.Marshal(customization)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&template).Update("customization", raw).Error; err != nil {
		t.Fatal(err)
	}
	updated, _, err := h.renderPluginSubscriptionTemplate(context.Background(), subscriptionRendererZnetSink, data)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256([]byte(first)) == sha256.Sum256([]byte(updated)) {
		t.Fatal("template change did not change the projected content revision")
	}
	if err := h.db.Model(&template).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.renderPluginSubscriptionTemplate(context.Background(), subscriptionRendererZnetSink, data); err == nil {
		t.Fatal("disabled client template silently fell back to renderer defaults")
	}
}
