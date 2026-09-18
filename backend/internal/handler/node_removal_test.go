package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeDeletionHTTPRetainsChildExecutionEvidence(t *testing.T) {
	f, node, account := deletionFixture(t)
	record := seedDeletionDNS(t, f, node, account, "retained")
	op := model.ProviderOperation{ProviderAccountID: account.ID, ResourceType: "dns_record", ResourceID: record.ID, Status: "failed", OperationType: "sync"}
	if err := f.h.db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}
	run, err := jobstore.New(f.h.db).Submit(context.Background(), jobs.Submission{Owner: "system", Key: fmt.Sprintf("dns_operation:%d", op.ID), Handler: "dns_operation", Resource: fmt.Sprintf("dns:%d", record.ID), Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", "unknown").Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	w := httptest.NewRecorder()
	f.h.NodeCascadeDeleteHandler(w, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", node.ID), f.admin, ""))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"unverified_jobs":1`) {
		t.Fatalf("missing blocking outcome: %d %s", w.Code, w.Body.String())
	}
	if calls != 0 {
		t.Fatal("local account deletion called remote provider")
	}
	var retained model.ProviderOperation
	if err := f.h.db.First(&retained, op.ID).Error; err != nil {
		t.Fatal("operation evidence was removed", err)
	}
}
