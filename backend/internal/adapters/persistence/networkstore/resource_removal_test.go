package networkstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func removeTestResource(ctx context.Context, service network.ResourceRemoval, kind string) error {
	if kind == "dns_operation" {
		_, err := service.DNS(ctx, 1, 7)
		return err
	}
	_, err := service.Certificate(ctx, 1, 9)
	return err
}
func TestResourceRemovalPreservesUnconfirmedOperationEvidence(t *testing.T) {
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := administrationFixture(t)
			prepareProviderDirectory(t, db)
			var opID uint
			resource := "dns:7"
			if kind == "dns_operation" {
				op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, Status: "failed", OperationType: "sync"}
				if err := db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID = op.ID
			} else {
				op := model.CertificateOperation{ManagedCertificateID: 9, NodeID: 1, Status: "failed", OperationType: "issue"}
				if err := db.Create(&op).Error; err != nil {
					t.Fatal(err)
				}
				opID = op.ID
				resource = "certificate:9"
			}
			service := network.ResourceRemoval{Store: ResourceRemoval{DB: db}}
			run := submit(t, jobstore.New(db), fmt.Sprintf("%s:%d", kind, opID), kind, resource)
			for _, state := range []string{"queued", "running", "unknown", "succeeded"} {
				if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", state).Error; err != nil {
					t.Fatal(err)
				}
				var blocked *network.ResourceRemovalBlocked
				if err := removeTestResource(context.Background(), service, kind); !errors.As(err, &blocked) {
					t.Fatal("unverified resource deleted", state, err)
				}
			}
			if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", "failed").Error; err != nil {
				t.Fatal(err)
			}
			if err := removeTestResource(context.Background(), service, kind); err != nil {
				t.Fatal(err)
			}
			if err := removeTestResource(context.Background(), service, kind); !errors.Is(err, network.ErrResourceNotFound) {
				t.Fatal(err)
			}
			var history jobstore.Record
			if err := db.First(&history, "id = ?", run.ID).Error; err != nil {
				t.Fatal("Run history lost", err)
			}
		})
	}
}

func TestResourceRemovalAuditRollbackPreservesCertificateBindings(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	endpoint := model.ProtocolEndpoint{NodeID: 1, Name: "TLS", RuntimeKey: "00000000-0000-4000-8000-000000000001", Protocol: "trojan", Port: 443, OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.CertificateProtocolEndpoint{ManagedCertificateID: 9, ProtocolEndpointID: endpoint.ID}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("fail_resource_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("fail_resource_audit")
	service := network.ResourceRemoval{Store: ResourceRemoval{DB: db}}
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		if err := removeTestResource(context.Background(), service, kind); err == nil {
			t.Fatal("unaudited removal")
		}
	}
	var count int64
	if err := db.Model(&model.CertificateProtocolEndpoint{}).Where("managed_certificate_id = ?", 9).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("binding was removed", count, err)
	}
	if err := db.Model(&model.ManagedDNSRecord{}).Where("id = ?", 7).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("DNS removed", count, err)
	}
}

func TestResourceRemovalRechecksAuthorityAndCancellation(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	service := network.ResourceRemoval{Store: ResourceRemoval{DB: db}}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		if err := removeTestResource(context.Background(), service, kind); !errors.Is(err, network.ErrResourcePermission) {
			t.Fatal("revoked administrator removed resource", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeTestResource(ctx, service, "dns_operation"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestResourceRemovalScopesPendingRunsAndProtectsInspections(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	service := network.ResourceRemoval{Store: ResourceRemoval{DB: db}}
	submit(t, jobstore.New(db), "other-resource", "dns_operation", "dns:8")
	inspection := submit(t, jobstore.New(db), "inspection", "dns_reconcile", "dns-inspection:7")
	var blocked *network.ResourceRemovalBlocked
	if err := removeTestResource(context.Background(), service, "dns_operation"); !errors.As(err, &blocked) || blocked.Blockers["unverified_jobs"] != 1 {
		t.Fatal("wrong inspection scope", err)
	}
	if err := db.Model(&jobstore.Record{}).Where("id = ?", inspection.ID).Update("state", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := removeTestResource(context.Background(), service, "dns_operation"); err != nil {
		t.Fatal("unrelated resource blocked removal", err)
	}
}
