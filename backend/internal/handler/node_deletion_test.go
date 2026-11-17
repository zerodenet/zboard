package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDeleteOfflineNodeDeletesProviderDNSWithoutSSHAndKeepsCertificateHistory(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			f, node, account := deletionFixture(t)
			record := seedDeletionDNS(t, f, node, account, "offline-owned")
			cert := model.ManagedCertificate{NodeID: node.ID, ProviderAccountID: &account.ID, Name: "offline", Domains: `["offline.example.test"]`, ContactEmail: "admin@example.test", Status: "active", AutoRenew: true}
			if err := f.h.db.Create(&cert).Error; err != nil {
				t.Fatal(err)
			}
			finished := time.Now().UTC()
			operation := model.CertificateOperation{ManagedCertificateID: cert.ID, NodeID: node.ID, OperationType: "issue", Status: "succeeded", Phase: "complete", FinishedAt: &finished}
			if err := f.h.db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			deleted := 0
			mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if denied {
					w.WriteHeader(403)
					w.Write([]byte(`{"success":false}`))
					return
				}
				if r.Method == http.MethodDelete {
					deleted++
					w.Write([]byte(`{"success":true}`))
					return
				}
				remote := cloudflareRecord{ID: record.ProviderRecordID, Type: record.RecordType, Name: record.DomainName, Content: "192.0.2.200", TTL: record.TTL, Proxied: record.Proxied, Comment: fmt.Sprintf("[ZBoard dns=%d node=%d]", record.ID, node.ID)}
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": remote})
			})
			f.h.backgroundJobs()
			f.h.services.Jobs.Detach(network.NodeRetirementHandler)
			response := httptest.NewRecorder()
			f.h.NodeCascadeDeleteHandler(response, announcementRequest(http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", node.ID), f.admin, ""))
			if response.Code != http.StatusAccepted {
				t.Fatal(response.Code, response.Body.String())
			}
			service := f.h.services.NodeRetirement(f.h)
			executor := jobs.Executor{Store: jobstore.New(f.h.db), Worker: "delete-test", Timeout: time.Minute, Handlers: map[string]jobs.Handler{network.NodeRetirementHandler: service.Execute}}
			err := executor.RunOne(context.Background())
			var after model.Node
			if e := f.h.db.First(&after, node.ID).Error; e != nil {
				t.Fatal(e)
			}
			progress, e := service.Repository.Read(t.Context(), node.ID)
			if e != nil {
				t.Fatal(e)
			}
			var dns int64
			f.h.db.Model(&model.ManagedDNSRecord{}).Where("id = ?", record.ID).Count(&dns)
			if denied {
				if err == nil || dns != 1 || after.LifecycleStatus != "deleting" || progress.State != "failed" || deleted != 0 {
					t.Fatal("provider failure was reported as deletion", err, dns, progress)
				}
				return
			}
			if err != nil || deleted != 1 || dns != 0 || after.LifecycleStatus != "archived" || progress.State != "succeeded" {
				t.Fatal("offline DNS deletion did not finish", err, deleted, dns, progress)
			}
			unconfirmed := 0
			for _, step := range progress.Steps {
				if step.Kind == "dns" && step.State != "succeeded" {
					t.Fatal("DNS not deleted", step)
				}
				if step.Kind != "dns" {
					if step.State != "unconfirmed" {
						t.Fatal("missing SSH became proof of native cleanup", step)
					}
					unconfirmed++
				}
			}
			if unconfirmed != 2 {
				t.Fatal("missing explicit host cleanup outcomes", progress)
			}
			var retained model.ManagedCertificate
			if e := f.h.db.First(&retained, cert.ID).Error; e != nil || retained.Status != "deleted" || retained.ProviderAccountID != nil || retained.AutoRenew {
				t.Fatal("managed certificate/provider association remained", retained, e)
			}
			var history int64
			if e := f.h.db.Model(&model.CertificateOperation{}).Where("id = ?", operation.ID).Count(&history).Error; e != nil || history != 1 {
				t.Fatal("deletion erased certificate execution history", history, e)
			}
		})
	}
}

func TestDisableNodeKeepsProviderDNSAndCanBeReenabled(t *testing.T) {
	f, node, _ := deletionFixture(t)
	account := model.ProviderAccount{}
	if err := f.h.db.First(&account).Error; err != nil {
		t.Fatal(err)
	}
	record := seedDeletionDNS(t, f, node, account, "disable-preserves")
	for _, enabled := range []bool{false, true} {
		response := httptest.NewRecorder()
		f.h.NodeUpdateHandler(response, announcementRequest(http.MethodPut, fmt.Sprintf("/api/v1/nodes/%d", node.ID), f.admin, fmt.Sprintf(`{"is_enabled":%t}`, enabled)))
		if response.Code != http.StatusOK {
			t.Fatal(response.Code, response.Body.String())
		}
		var actual model.Node
		if err := f.h.db.First(&actual, node.ID).Error; err != nil || actual.IsEnabled != enabled || actual.ArchivedAt != nil {
			t.Fatal("disable became deletion", actual, err)
		}
		var retained model.ManagedDNSRecord
		if err := f.h.db.First(&retained, record.ID).Error; err != nil {
			t.Fatal("disable deleted provider management", err)
		}
	}
}

func TestNodeDeletionHTTPRejectsRetainingDNSAndReturnsDurableProgress(t *testing.T) {
	f, node, _ := deletionFixture(t)
	f.h.backgroundJobs()
	f.h.services.Jobs.Detach(network.NodeRetirementHandler)
	path := fmt.Sprintf("/api/v1/admin/nodes/%d/deletion", node.ID)
	rejected := httptest.NewRecorder()
	f.h.NodeRetirementHandler(rejected, announcementRequest(http.MethodPost, path, f.admin, `{"mode":"detach"}`))
	if rejected.Code != http.StatusBadRequest {
		t.Fatal("new delete endpoint accepted retaining provider DNS", rejected.Code, rejected.Body.String())
	}
	accepted := httptest.NewRecorder()
	f.h.NodeRetirementHandler(accepted, announcementRequest(http.MethodPost, path, f.admin, `{"mode":"delete"}`))
	if accepted.Code != http.StatusAccepted {
		t.Fatal(accepted.Code, accepted.Body.String())
	}
	progress := httptest.NewRecorder()
	f.h.NodeRetirementHandler(progress, announcementRequest(http.MethodGet, path, f.admin, ""))
	if progress.Code != http.StatusOK {
		t.Fatal(progress.Code, progress.Body.String())
	}
	var body struct {
		Data network.Retirement `json:"data"`
	}
	if err := json.Unmarshal(progress.Body.Bytes(), &body); err != nil || body.Data.Mode != "delete" || body.Data.RunID == "" {
		t.Fatal("missing canonical durable progress", body, err)
	}
}
