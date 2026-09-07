package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestManagedProcessRulesRoundTripWithoutChangingCaseOrOR(t *testing.T) {
	raw := []byte("payload:\n  - PROCESS-NAME,YouTube.exe\n  - PROCESS-NAME,YouTube.exe\n  - PROCESS-PATH,/Applications/Example App/App\n  - DOMAIN-SUFFIX,example.com\n  - IP-CIDR,192.0.2.1/24,no-resolve\n")
	doc, err := parseManagedRuleSource(raw, managedRuleSourceClashClassical)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Rules) != 2 || len(doc.ClientRules) != 2 {
		t.Fatalf("rules = %#v", doc)
	}
	stored := encodeManagedCanonicalSource(doc)
	doc, err = decodeAndNormalizeZeroRuleIR(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encodeManagedRuleClashYAML(doc), []byte("PROCESS-NAME,YouTube.exe")) {
		t.Fatal("process name lost")
	}
	if !bytes.Contains(encodeManagedRuleClashText(doc), []byte("PROCESS-PATH,/Applications/Example App/App")) {
		t.Fatal("process path lost")
	}
	raw, err = encodeManagedRuleSingBox(doc)
	if err != nil {
		t.Fatal(err)
	}
	var sing struct {
		Rules []map[string][]string `json:"rules"`
	}
	if err = json.Unmarshal(raw, &sing); err != nil {
		t.Fatal(err)
	}
	if len(sing.Rules) != 4 {
		t.Fatalf("conditions should form four OR branches: %s", raw)
	}
	for _, rule := range sing.Rules {
		if len(rule) != 1 {
			t.Fatalf("conditions accidentally ANDed: %s", raw)
		}
	}
	if sing.Rules[2]["process_name"][0] != "YouTube.exe" {
		t.Fatalf("changed process name: %s", raw)
	}
}

func TestManagedProcessOnlySourceSkipsZRSAndRejectsZeroBinding(t *testing.T) {
	doc, err := parseManagedRuleSource([]byte("PROCESS-NAME,App.exe\n"), managedRuleSourceClashClassical)
	if err != nil {
		t.Fatal(err)
	}
	source := encodeManagedCanonicalSource(doc)
	h := &handlers{zeroArtifactDir: t.TempDir()}
	if err = h.writeManagedRuleSource("process-only", source); err != nil {
		t.Fatal(err)
	}
	stored, err := h.readManagedRuleSource("process-only")
	if err != nil || !bytes.Equal(source, stored) {
		t.Fatalf("source not preserved: %v", err)
	}
	path, err := h.managedRuleArtifactPath("process-only", sha256.Sum256(source), managedRuleArtifactZRS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("must not publish partial ZRS: %v", err)
	}
	format, err := managedRuleStorageFormat(source)
	if err != nil {
		t.Fatal(err)
	}
	record := model.SubscriptionRuleSet{Name: "App", Tag: "app", Renderer: managedRuleSetRenderer, Format: format, Interval: 3600}
	for _, renderer := range []string{subscriptionRendererClash, subscriptionRendererSingBox, subscriptionRendererZNetSink} {
		_, err := managedRuleCustomizationForRenderer(renderer, "https://panel.example.com", record, "direct")
		if renderer == subscriptionRendererZNetSink {
			if !errors.Is(err, errManagedRuleClientCompatibility) {
				t.Fatalf("Zero accepted client-only rules: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedClientRuleValidationAndEmptyProvider(t *testing.T) {
	for _, value := range []string{"", "App\n.exe", "App,extra", "/usr/bin/App"} {
		if _, err := normalizeManagedProcessValue(managedRuleTypeProcessName, value); err == nil {
			t.Fatalf("accepted invalid process %q", value)
		}
	}
	for _, raw := range []string{
		`{"version":1,"rules":[],"client_rules":[{"type":"domain_exact","value":"example.com"}]}`,
		`{"version":1,"rules":[{"type":"process_name","value":"App"}]}`,
		`{"version":1,"rules":[],"client_rules":[{"type":"process_name","value":"App","action":"reject"}]}`,
	} {
		if _, err := decodeAndNormalizeZeroRuleIR([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid document %s", raw)
		}
	}
	if _, err := parseManagedRuleSource([]byte("payload:\n # - USER-AGENT,MOO*\n"), managedRuleSourceClashClassical); err == nil || !strings.Contains(err.Error(), "没有有效规则") {
		t.Fatalf("empty provider message: %v", err)
	}
}

func TestManagedProcessCreateAndPublicFormats(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	content := `{"version":1,"rules":[],"client_rules":[{"type":"process_name","value":"App.exe"}]}`
	body, _ := json.Marshal(subscriptionRuleSetWriteReq{Name: "App", Tag: "app", Content: &content, SourceFormat: managedRuleSourceZeroRuleIR, SyncInterval: 3600})
	response := httptest.NewRecorder()
	h.AdminSubscriptionRuleSetCreateHandler(response, announcementRequest(http.MethodPost, "/api/v1/admin/subscription-rule-sets", token, string(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var record model.SubscriptionRuleSet
	if err := h.db.Where("tag = ?", "app").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.Format != managedRuleSetFormatClient {
		t.Fatalf("lost compatibility metadata: %#v", record)
	}
	// A metadata-only update must retain the renderer compatibility marker.
	body, _ = json.Marshal(subscriptionRuleSetWriteReq{Name: "App renamed", Tag: "app", SourceURL: "https://example.com/rules.yaml", SourceFormat: managedRuleSourceAuto, SyncInterval: 3600})
	response = httptest.NewRecorder()
	h.AdminSubscriptionRuleSetUpdateHandler(response, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/subscription-rule-sets/%d", record.ID), token, string(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("metadata update: %d %s", response.Code, response.Body.String())
	}
	if err := h.db.First(&record, record.ID).Error; err != nil || record.Format != managedRuleSetFormatClient {
		t.Fatalf("metadata update lost format: %#v %v", record, err)
	}
	for _, renderer := range []string{subscriptionRendererZNetSink, subscriptionRendererClash} {
		response = httptest.NewRecorder()
		h.AdminSubscriptionRuleSetListHandler(response, announcementRequest(http.MethodGet, "/api/v1/admin/subscription-rule-sets?renderer="+renderer, token, ""))
		if response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
		found := strings.Contains(response.Body.String(), "App renamed")
		if found != (renderer == subscriptionRendererClash) {
			t.Fatalf("%s picker compatibility: %s", renderer, response.Body.String())
		}
	}
	for _, format := range []string{managedRuleArtifactClashClassicalYAML, managedRuleArtifactSingBoxSource, managedRuleArtifactZRS} {
		response = httptest.NewRecorder()
		h.PublicManagedRuleSetHandler(response, httptest.NewRequest(http.MethodGet, "/api/v1/rules/app?format="+format, nil))
		if format == managedRuleArtifactZRS {
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "进程") {
				t.Fatalf("ZRS: %d %s", response.Code, response.Body.String())
			}
		} else if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "App.exe") {
			t.Fatalf("%s: %d %s", format, response.Code, response.Body.String())
		}
	}
}

func TestProcessImportCannotBreakExistingZeroTemplate(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	if res := createManagedRuleForTest(t, h, token); res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	var record model.SubscriptionRuleSet
	if err := h.db.Where("tag = ?", "existing-rules").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	template := model.SubscriptionTemplate{Name: "Zero clients", Slug: "zero-clients", Renderer: subscriptionRendererZNetSink, Customization: json.RawMessage(`{}`)}
	if err := h.db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.SubscriptionTemplateRuleSetBinding{SubscriptionTemplateID: template.ID, SubscriptionRuleSetID: record.ID, Action: "direct"}
	if err := h.db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	previous, _ := h.readManagedRuleSource(record.Tag)
	doc, err := parseManagedRuleSource([]byte("PROCESS-NAME,App.exe"), managedRuleSourceClashClassical)
	if err != nil {
		t.Fatal(err)
	}
	err = h.replaceManagedRuleContent(record, encodeManagedCanonicalSource(doc), nil, authClaims{UserID: 1})
	if !errors.Is(err, errManagedRuleClientCompatibility) {
		t.Fatalf("expected compatibility rejection: %v", err)
	}
	current, _ := h.readManagedRuleSource(record.Tag)
	if !bytes.Equal(previous, current) {
		t.Fatal("failed import changed existing source")
	}
}
