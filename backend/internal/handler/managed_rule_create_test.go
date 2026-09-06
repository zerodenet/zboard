package handler

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func newManagedRuleCreateFixture(t *testing.T) (*handlers, string) {
	t.Helper()
	h, _ := newAnnouncementTestHandlers(t)
	h.zeroArtifactDir = t.TempDir()
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	return h, token
}

func createManagedRuleForTest(t *testing.T, h *handlers, token string) *httptest.ResponseRecorder {
	t.Helper()
	content := `{"version":1,"rules":[{"type":"domain_suffix","value":"example.com"}]}`
	body, err := json.Marshal(subscriptionRuleSetWriteReq{
		Name: "Existing rules", Tag: "existing-rules", Content: &content,
		SourceFormat: managedRuleSourceZeroRuleIR, SyncInterval: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.AdminSubscriptionRuleSetCreateHandler(response, announcementRequest(http.MethodPost,
		"/api/v1/admin/subscription-rule-sets", token, string(body)))
	return response
}

func TestDuplicateManagedRuleCreatePreservesExistingSourceAndArtifacts(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	if response := createManagedRuleForTest(t, h, token); response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	source, err := h.readManagedRuleSource("existing-rules")
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := h.managedRuleArtifactPath("existing-rules", sha256.Sum256(source), managedRuleArtifactClashClassicalYAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeManagedRuleFileAtomic(artifact, []byte("existing cached artifact")); err != nil {
		t.Fatal(err)
	}
	if response := createManagedRuleForTest(t, h, token); response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate: %d %s", response.Code, response.Body.String())
	}
	actual, err := h.readManagedRuleSource("existing-rules")
	if err != nil || string(actual) != string(source) {
		t.Fatalf("duplicate creation changed existing source: %q, %v", actual, err)
	}
	actual, err = os.ReadFile(artifact)
	if err != nil || string(actual) != "existing cached artifact" {
		t.Fatalf("duplicate creation removed cached artifact: %q, %v", actual, err)
	}
	var record model.SubscriptionRuleSet
	if err := h.db.Where("tag = ?", "existing-rules").First(&record).Error; err != nil || record.Revision != 1 {
		t.Fatalf("existing database record changed: %+v, %v", record, err)
	}
}

func TestManagedRuleCreateRollbackRemovesItsOwnFiles(t *testing.T) {
	h, token := newManagedRuleCreateFixture(t)
	if err := h.db.Exec(`CREATE TRIGGER reject_rule_audit BEFORE INSERT ON audit_logs
BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if response := createManagedRuleForTest(t, h, token); response.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure: %d %s", response.Code, response.Body.String())
	}
	dir, err := h.managedRuleSetDir("existing-rules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("failed creation left files behind: %v", err)
	}
	var count int64
	if err := h.db.Model(&model.SubscriptionRuleSet{}).Where("tag = ?", "existing-rules").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed creation left database row behind: count=%d, %v", count, err)
	}
}
