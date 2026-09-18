package networkstore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestOperationRetentionKeepsUnresolvedAndContradictoryEvidence(t *testing.T) {
	for _, kind := range []network.OperationKind{network.CertificateOperation, network.DNSOperation} {
		t.Run(string(kind), func(t *testing.T) {
			db, _ := administrationFixture(t)
			seedLegacyResources(t, db)
			ctx := context.Background()
			old := time.Now().UTC().AddDate(-1, 0, 0)
			var unknownRun string
			for _, state := range []string{"none", "queued", "running", "unknown", "failed", "contradictory", "active-domain"} {
				status := "failed"
				if state == "active-domain" {
					status = "running"
				}
				var id uint
				resource := "dns:7"
				if kind == network.CertificateOperation {
					op := model.CertificateOperation{ManagedCertificateID: 9, NodeID: 1, OperationType: "issue", Status: status, Phase: "persisting", FinishedAt: &old}
					if err := db.Create(&op).Error; err != nil {
						t.Fatal(err)
					}
					id = op.ID
					resource = "certificate:9"
				} else {
					op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, OperationType: "sync", Status: status, Phase: "persisting", FinishedAt: &old}
					if err := db.Create(&op).Error; err != nil {
						t.Fatal(err)
					}
					id = op.ID
				}
				if state == "none" || state == "active-domain" {
					continue
				}
				payload, _ := json.Marshal(map[string]any{"revision": "1", "operation_id": id})
				run, err := jobstore.New(db).Submit(ctx, jobs.Submission{Owner: "system", Key: fmt.Sprintf("%s:%d", kind, id), Handler: string(kind), Resource: resource, Payload: string(payload)})
				if err != nil {
					t.Fatal(err)
				}
				actual := state
				if state == "contradictory" {
					actual = "succeeded"
				}
				if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", actual).Error; err != nil {
					t.Fatal(err)
				}
				if state == "unknown" {
					unknownRun = run.ID
				}
			}
			service := network.OperationHistory{Store: OperationHistory{DB: db}}
			count, err := service.Prune(ctx, kind, time.Now().UTC())
			if err != nil || count != 2 {
				t.Fatal("expected only unreferenced and matching terminal rows removed", count, err)
			}
			count, err = service.Prune(ctx, kind, time.Now().UTC())
			if err != nil || count != 0 {
				t.Fatal("protected evidence was pruned", count, err)
			}
			if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
				t.Fatal(err)
			}
			if err := (jobs.ReviewService{Repository: JobReviews{DB: db}}).Resolve(ctx, jobs.Reviewer{AccountID: 1}, jobs.Review{RunID: unknownRun, Outcome: jobs.Failed, Reason: "verified failed operation"}); err != nil {
				t.Fatal(err)
			}
			count, err = service.Prune(ctx, kind, time.Now().UTC())
			if err != nil || count != 1 {
				t.Fatal("resolved evidence was not released", count, err)
			}
		})
	}
}

func TestOperationRetentionHonorsCancellation(t *testing.T) {
	db, _ := administrationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (network.OperationHistory{Store: OperationHistory{DB: db}}).Prune(ctx, network.DNSOperation, time.Now().UTC()); err == nil {
		t.Fatal("canceled cleanup accepted")
	}
}
