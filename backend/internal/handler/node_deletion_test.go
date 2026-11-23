package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestDeleteOfflineNodeDeletesProviderDNSWithoutSSHAndKeepsCertificateHistory(t *testing.T) {
	for _, scenario := range []string{"owned", "ownership_mismatch", "shared", "permission_denied"} {
		t.Run(scenario, func(t *testing.T) {
			f, node, account := deletionFixture(t)
			record := seedDeletionDNS(t, f, node, account, "offline-first")
			next := seedDeletionDNS(t, f, node, account, "offline-next")
			var shared model.ManagedDNSRecord
			if scenario == "shared" {
				survivor := model.Node{Name: "other-node", Address: "192.0.2.2", Config: "{}"}
				if err := f.h.db.Create(&survivor).Error; err != nil {
					t.Fatal(err)
				}
				shared = record
				shared.ID = 0
				shared.NodeID = survivor.ID
				if err := f.h.db.Create(&shared).Error; err != nil {
					t.Fatal(err)
				}
			}
			cert := model.ManagedCertificate{NodeID: node.ID, ProviderAccountID: &account.ID, Name: "offline", Domains: `["offline.example.test"]`, ContactEmail: "admin@example.test", Status: "active", AutoRenew: true}
			if err := f.h.db.Create(&cert).Error; err != nil {
				t.Fatal(err)
			}
			finished := time.Now().UTC()
			operation := model.CertificateOperation{ManagedCertificateID: cert.ID, NodeID: node.ID, OperationType: "issue", Status: "succeeded", Phase: "complete", FinishedAt: &finished}
			if err := f.h.db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			var deleted atomic.Int32
			mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if scenario == "permission_denied" && path.Base(r.URL.Path) == record.ProviderRecordID {
					w.WriteHeader(403)
					w.Write([]byte(`{"success":false}`))
					return
				}
				if scenario == "shared" && path.Base(r.URL.Path) == record.ProviderRecordID {
					t.Error("shared DNS should be skipped before reaching the provider")
				}
				if r.Method == http.MethodDelete {
					if scenario == "ownership_mismatch" && path.Base(r.URL.Path) == record.ProviderRecordID {
						t.Error("provider DELETE reached a DNS record owned by another node")
					}
					deleted.Add(1)
					w.Write([]byte(`{"success":true}`))
					return
				}
				target := record
				if path.Base(r.URL.Path) == next.ProviderRecordID {
					target = next
				}
				marker := fmt.Sprintf("[ZBoard dns=%d node=%d]", target.ID, node.ID)
				if scenario == "ownership_mismatch" && target.ID == record.ID {
					marker = fmt.Sprintf("[ZBoard dns=%d node=%d]", target.ID, node.ID+1)
				}
				remote := cloudflareRecord{ID: target.ProviderRecordID, Type: target.RecordType, Name: target.DomainName, Content: "192.0.2.200", TTL: target.TTL, Proxied: target.Proxied, Comment: marker}
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
			f.h.db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", node.ID).Count(&dns)
			if scenario == "permission_denied" {
				if err == nil || dns != 2 || after.LifecycleStatus != "deleting" || progress.State != "failed" || deleted.Load() != 0 {
					t.Fatal("provider failure was reported as deletion", err, dns, progress)
				}
				return
			}
			wantDeleted := int32(2)
			if scenario == "ownership_mismatch" || scenario == "shared" {
				wantDeleted = 1
			}
			if err != nil || deleted.Load() != wantDeleted || dns != 0 || after.LifecycleStatus != "archived" || progress.State != "succeeded" {
				t.Fatal("offline DNS deletion did not finish", err, deleted.Load(), dns, progress)
			}
			unconfirmed := 0
			for _, step := range progress.Steps {
				if step.Kind == "dns" {
					want := "succeeded"
					if (scenario == "ownership_mismatch" || scenario == "shared") && step.ID == record.ID {
						want = "retained"
					}
					if step.State != want {
						t.Fatal("unexpected DNS outcome", step)
					}
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
			if shared.ID != 0 {
				var surviving model.ManagedDNSRecord
				if err := f.h.db.First(&surviving, shared.ID).Error; err != nil || surviving.NodeID != shared.NodeID {
					t.Fatal("surviving node lost its DNS management", surviving, err)
				}
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

func TestNodeDeletionHTTPRejectsLegacyDetachAndReturnsDurableProgress(t *testing.T) {
	f, node, _ := deletionFixture(t)
	f.h.backgroundJobs()
	f.h.services.Jobs.Detach(network.NodeRetirementHandler)
	path := fmt.Sprintf("/api/v1/admin/nodes/%d/deletion", node.ID)
	rejected := httptest.NewRecorder()
	f.h.NodeRetirementHandler(rejected, announcementRequest(http.MethodPost, path, f.admin, `{"mode":"detach"}`))
	if rejected.Code != http.StatusBadRequest {
		t.Fatal("delete endpoint accepted legacy detach", rejected.Code, rejected.Body.String())
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

func TestNodeDeletionResumesKeepingRemainingDNSAfterProviderFailure(t *testing.T) {
	f, node, account := deletionFixture(t)
	owned := seedDeletionDNS(t, f, node, account, "owned")
	denied := seedDeletionDNS(t, f, node, account, "denied")
	pending := seedDeletionDNS(t, f, node, account, "pending")
	var providerCalls atomic.Int32
	mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if path.Base(r.URL.Path) != owned.ProviderRecordID {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false}`))
			return
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		remote := cloudflareRecord{ID: owned.ProviderRecordID, Type: owned.RecordType, Name: owned.DomainName, Comment: fmt.Sprintf("[ZBoard dns=%d node=%d]", owned.ID, node.ID)}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": remote})
	})
	f.h.backgroundJobs()
	f.h.services.Jobs.Detach(network.NodeRetirementHandler)
	service := f.h.services.NodeRetirement(f.h)
	executor := jobs.Executor{Store: jobstore.New(f.h.db), Worker: "keep-dns-test", Timeout: time.Minute, Handlers: map[string]jobs.Handler{network.NodeRetirementHandler: service.Execute}}
	endpoint := fmt.Sprintf("/api/v1/admin/nodes/%d/deletion", node.ID)
	submit := func(input string, wantStatus int) {
		t.Helper()
		res := httptest.NewRecorder()
		f.h.NodeRetirementHandler(res, announcementRequest(http.MethodPost, endpoint, f.admin, input))
		if res.Code != wantStatus {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	keepDNS := `{"mode":"delete","keep_remaining_dns":true}`
	submit(keepDNS, http.StatusConflict)
	submit(`{"mode":"delete"}`, http.StatusAccepted)
	submit(keepDNS, http.StatusConflict)
	if err := executor.RunOne(t.Context()); err == nil {
		t.Fatal("provider failure was treated as cleanup")
	}
	first, err := service.Repository.Read(t.Context(), node.ID)
	if err != nil || first.State != "failed" || providerCalls.Load() != 3 {
		t.Fatal("unexpected first attempt", first, providerCalls.Load(), err)
	}
	submit(keepDNS, http.StatusAccepted)
	if err := executor.RunOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	done, err := service.Repository.Read(t.Context(), node.ID)
	if err != nil || done.State != "succeeded" || done.Attempt != first.Attempt+1 || done.RunID == first.RunID || providerCalls.Load() != 3 {
		t.Fatal("fallback retried external DNS or failed to complete", done, providerCalls.Load(), err)
	}
	for _, step := range done.Steps {
		want := "unconfirmed"
		if step.Kind == "dns" {
			want = map[uint]string{owned.ID: "succeeded", denied.ID: "retained", pending.ID: "retained"}[step.ID]
		}
		if step.State != want {
			t.Fatal("lost persisted outcome", step)
		}
	}
	var remainingDNS int64
	if err := f.h.db.Model(&model.ManagedDNSRecord{}).Where("node_id = ?", node.ID).Count(&remainingDNS).Error; err != nil || remainingDNS != 0 {
		t.Fatal("panel DNS management remained", remainingDNS, err)
	}
	var after model.Node
	if err := f.h.db.First(&after, node.ID).Error; err != nil || after.LifecycleStatus != "archived" {
		t.Fatal("node deletion remained incomplete", after.LifecycleStatus, err)
	}
}
