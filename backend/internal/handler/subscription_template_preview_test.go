package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func previewTemplateHTTP(t *testing.T, h *handlers, token string, request subscriptionTemplatePreviewReq) (*httptest.ResponseRecorder, subscriptionTemplatePreview) {
	t.Helper()
	body, _ := json.Marshal(request)
	w := httptest.NewRecorder()
	h.AdminSubscriptionTemplatePreviewHandler(w, announcementRequest(http.MethodPost, "/api/v1/admin/subscription-templates/preview", token, string(body)))
	var response struct{ Data subscriptionTemplatePreview }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return w, response.Data
}

func TestLiveSubscriptionPreviewUsesRealRendererWithoutNativeValidationOrWrites(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "Preview fixture", SiteURL: "https://site.example", InstalledAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	h.zeroMieruAccess = true
	prior := managedZeroSubscriptionValidator
	calls := 0
	managedZeroSubscriptionValidator = func(context.Context, string, string, []byte) error { calls++; return nil }
	t.Cleanup(func() { managedZeroSubscriptionValidator = prior })
	custom := defaultSubscriptionCustomization(subscriptionRendererZnetSink)
	custom.RuleSets = nil
	custom.PolicyGroups[0].DefaultGroup = ""
	custom.PolicyGroups[0].DefaultTarget = "direct"
	raw, _ := json.Marshal(custom)
	req := subscriptionTemplatePreviewReq{Renderer: subscriptionRendererZnetSink, Customization: raw, Live: true, FullContent: true}
	w, result := previewTemplateHTTP(t, h, token, req)
	if w.Code != 200 || calls != 0 || result.Truncated || result.Sample != "example" || result.DeliveryEncoding != "base64" {
		t.Fatalf("live render: %d %s native=%d", w.Code, w.Body.String(), calls)
	}
	if len(result.Groups) == 0 || result.Groups[0].Default != "direct" || result.Groups[0].Members[0] != "direct" {
		t.Fatalf("effect not derived from final defaults: %#v", result.Groups)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("preview must not be cached")
	}
	var count int64
	if err := h.db.Model(&model.SubscriptionTemplate{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview persisted a template: %d %v", count, err)
	}
	req.Live = false
	w, _ = previewTemplateHTTP(t, h, token, req)
	if w.Code != 200 || calls != 1 {
		t.Fatalf("explicit check skipped native validation: %d calls=%d %s", w.Code, calls, w.Body.String())
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	w, _ = previewTemplateHTTP(t, h, token, req)
	if w.Code != 403 {
		t.Fatalf("revoked admin read preview: %d", w.Code)
	}
}

func TestSubscriptionPreviewEffectsFollowFinalJSONAndYAML(t *testing.T) {
	for _, row := range []struct{ renderer, content string }{
		{subscriptionRendererClash, "proxy-groups:\n  - name: 支付服务\n    type: select\n    proxies: [DIRECT, 节点选择]\n"},
		{subscriptionRendererZnetSink, `{"outbound_groups":[{"tag":"支付服务","type":"selector","outbounds":["节点选择","direct"],"default":"direct"}]}`},
		{subscriptionRendererSingBox, `{"outbounds":[{"type":"direct","tag":"direct"},{"tag":"支付服务","type":"selector","outbounds":["节点选择","direct"],"default":"direct"}]}`},
	} {
		groups := subscriptionPreviewGroups(row.renderer, row.content)
		if len(groups) != 1 || groups[0].Name != "支付服务" || !strings.EqualFold(groups[0].Default, "direct") {
			t.Fatalf("final effect missing: %#v", groups)
		}
	}
	if len(subscriptionPreviewGroups("shadowrocket", "ss://example")) != 0 {
		t.Fatal("link output fabricated a policy group")
	}
}

func TestSubscriptionPreviewUsesAuthorizedRealNodeNamesAndEphemeralCredentials(t *testing.T) {
	f := newOrderFixture(t)
	if err := f.h.db.Create(&model.Installation{ID: 1, SiteName: "Preview fixture", SiteURL: "https://site.example", InstalledAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sub := model.Subscription{UserID: 1, PlanID: f.planRecord.ID, PlanSKUID: f.skuRecord.ID, NodeGroupID: f.group.ID, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 1}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "Preview node", Region: "HK", IsEnabled: true, LastSeenAt: &now}
	if err := f.h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "真实香港节点", Protocol: "trojan", Address: "node.example.test", Port: 443, PublicPort: 443, IsActive: true, ClientConfig: `{"type":"trojan","password":"DO-NOT-EXPOSE","sni":"node.example.test"}`}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Create(&model.NodeGroupEndpoint{NodeGroupID: f.group.ID, ProtocolEndpointID: endpoint.ID}).Error; err != nil {
		t.Fatal(err)
	}
	custom := defaultSubscriptionCustomization(subscriptionRendererClash)
	custom.RuleSets = nil
	custom.PolicyGroups[0].IncludePattern = "香港"
	custom.PolicyGroups = custom.PolicyGroups[:1]
	custom.PolicyGroups[0].IncludeGroups = nil
	custom.PolicyGroups[0].DefaultGroup = ""
	custom.PolicyGroups[0].DefaultTarget = "direct"
	raw, _ := json.Marshal(custom)
	w, result := previewTemplateHTTP(t, f.h, f.token, subscriptionTemplatePreviewReq{Renderer: subscriptionRendererClash, Customization: raw, Live: true, FullContent: true, SubscriptionID: sub.ID})
	if w.Code != 200 {
		t.Fatalf("real-node preview: %d %s", w.Code, w.Body.String())
	}
	if result.Sample != "subscription" || !strings.Contains(result.Content, "真实香港节点") || strings.Contains(result.Content, "DO-NOT-EXPOSE") || !strings.Contains(result.Content, "preview-credential") {
		t.Fatalf("unfaithful or sensitive preview: %s", w.Body.String())
	}
	var count int64
	if err := f.h.db.Model(&model.ProtocolCredential{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview provisioned credentials: %d %v", count, err)
	}
	var stored model.ProtocolEndpoint
	if err := f.h.db.First(&stored, endpoint.ID).Error; err != nil || !strings.Contains(stored.ClientConfig, "DO-NOT-EXPOSE") {
		t.Fatal("preview modified stored node configuration")
	}
	if err := f.h.db.Model(&sub).Update("status", "canceled").Error; err != nil {
		t.Fatal(err)
	}
	w, _ = previewTemplateHTTP(t, f.h, f.token, subscriptionTemplatePreviewReq{Renderer: subscriptionRendererClash, Customization: raw, Live: true, SubscriptionID: sub.ID})
	if w.Code != 400 {
		t.Fatalf("canceled sample silently fell back: %d %s", w.Code, w.Body.String())
	}
}

func TestSubscriptionPreviewCredentialRedactionDoesNotAlterNamesOrRoutingValues(t *testing.T) {
	config := map[string]interface{}{"id": "private-uuid", "password": "private-password", "server_name": "edge.example", "headers": map[string]interface{}{"Host": "edge.example", "X-Private": "private-header"}, "tls": map[string]interface{}{"private_key": "private-key"}}
	redactSubscriptionPreviewConfig(config)
	content, _ := json.Marshal(config)
	if strings.Contains(string(content), "private-") || !strings.Contains(string(content), "edge.example") {
		t.Fatalf("bad credential projection: %s", content)
	}
}

func TestSubscriptionPreviewFullRawPreservesLargeRenderedDocument(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	if err := h.db.Create(&model.Installation{ID: 1, SiteName: "Preview", SiteURL: "https://site.example", InstalledAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	custom := defaultSubscriptionCustomization(subscriptionRendererClash)
	// A legal bounded overlay can expand when YAML indents multiline values.
	var overlay any = strings.Repeat("a\n", 20000)
	for range 8 {
		overlay = map[string]any{"metadata": overlay}
	}
	source, _ := json.Marshal(overlay)
	custom.AdvancedSource = string(source)
	raw, _ := json.Marshal(custom)
	req := subscriptionTemplatePreviewReq{Renderer: subscriptionRendererClash, Customization: raw, Live: true, FullContent: true}
	w, full := previewTemplateHTTP(t, h, token, req)
	if w.Code != 200 || full.Bytes <= maxTemplatePreviewBytes || full.Bytes != len(full.Content) || full.Truncated {
		t.Fatalf("incomplete full Raw: status=%d bytes=%d content=%d truncated=%v %s", w.Code, full.Bytes, len(full.Content), full.Truncated, w.Body.String()[:min(300, w.Body.Len())])
	}
	req.FullContent = false
	w, legacy := previewTemplateHTTP(t, h, token, req)
	if w.Code != 200 || !legacy.Truncated || legacy.Bytes != full.Bytes || len(legacy.Content) > maxTemplatePreviewBytes || !strings.HasPrefix(full.Content, legacy.Content) {
		t.Fatal("legacy preview truncation changed the full document")
	}
}
