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

func TestResourceDeletionHTTPReturnsUnknownJobBlockers(t *testing.T) {
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		t.Run(kind, func(t *testing.T) {
			f, node, account := deletionFixture(t)
			var opID, id uint
			var resource, path string
			if kind == "dns_operation" {
				record := seedDeletionDNS(t, f, node, account, "removal")
				op := model.ProviderOperation{ProviderAccountID: account.ID, ResourceType: "dns_record", ResourceID: record.ID, Status: "failed", OperationType: "sync"}
				if err := f.h.db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID, id = op.ID, record.ID
				resource = fmt.Sprintf("dns:%d", id)
				path = fmt.Sprintf("/api/v1/admin/dns-records/%d", id)
			} else {
				cert := model.ManagedCertificate{NodeID: node.ID, Name: "local", Domains: `["example.test"]`, Status: "active"}
				if err := f.h.db.Create(&cert).Error; err != nil {
					t.Fatal(err)
				}
				op := model.CertificateOperation{ManagedCertificateID: cert.ID, NodeID: node.ID, Status: "failed", OperationType: "issue"}
				if err := f.h.db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID, id = op.ID, cert.ID
				resource = fmt.Sprintf("certificate:%d", id)
				path = fmt.Sprintf("/api/v1/admin/certificates/%d", id)
			}
			run, err := jobstore.New(f.h.db).Submit(context.Background(), jobs.Submission{Owner: "system", Key: fmt.Sprintf("%s:%d", kind, opID), Handler: kind, Resource: resource, Payload: `{}`})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", "unknown").Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			w := httptest.NewRecorder()
			request := announcementRequest(http.MethodDelete, path, f.admin, "")
			if kind == "dns_operation" {
				f.h.ManagedDNSDeleteHandler(w, request)
			} else {
				f.h.ManagedCertificateDeleteHandler(w, request)
			}
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"unverified_jobs":1`) {
				t.Fatalf("unknown job ignored: %d %s", w.Code, w.Body.String())
			}
			if calls != 0 {
				t.Fatal("unexpected external cleanup")
			}
		})
	}
}
