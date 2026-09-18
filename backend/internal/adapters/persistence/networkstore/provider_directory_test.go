package networkstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func prepareProviderDirectory(t *testing.T, db *gorm.DB) {
	t.Helper()
	seedLegacyResources(t, db)
	for _, result := range []*gorm.DB{
		db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true),
		db.Model(&model.ManagedCertificate{}).Where("id = ?", 9).Updates(map[string]any{"status": "active", "provider_account_id": 1, "auto_renew": true}),
		db.Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", 1).Update("status", "active"),
	} {
		if result.Error != nil {
			t.Fatal(result.Error)
		}
	}
}
func TestProviderDeletionRetainsUnresolvedAndContradictoryEvidence(t *testing.T) {
	for _, kind := range []string{"dns_operation", "certificate_operation"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := administrationFixture(t)
			prepareProviderDirectory(t, db)
			ctx := context.Background()
			service := network.ProviderDirectory{Store: ProviderAccounts{DB: db}}
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
			run := submit(t, jobstore.New(db), fmt.Sprintf("%s:%d", kind, opID), kind, resource)
			for _, state := range []string{"queued", "running", "unknown", "succeeded"} {
				if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", state).Error; err != nil {
					t.Fatal(err)
				}
				_, err := service.Delete(ctx, 1, 1)
				var blocked *network.ProviderDeletionBlocked
				if !errors.As(err, &blocked) {
					t.Fatal("unresolved or contradictory evidence deleted", state, err)
				}
				var dnsCount, certCount int64
				db.Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", 1).Count(&dnsCount)
				db.Model(&model.ManagedCertificate{}).Where("provider_account_id = ?", 1).Count(&certCount)
				if dnsCount != 2 || certCount != 1 {
					t.Fatal("blocked deletion partially changed associations")
				}
			}
			if err := db.Model(&jobstore.Record{}).Where("id = ?", run.ID).Update("state", "failed").Error; err != nil {
				t.Fatal(err)
			}
			result, err := service.Delete(ctx, 1, 1)
			if err != nil || !result.Deleted || !result.ExternalAccountRetained {
				t.Fatal(result, err)
			}
			var cert model.ManagedCertificate
			if err := db.First(&cert, 9).Error; err != nil {
				t.Fatal(err)
			}
			if cert.ProviderAccountID != nil || cert.AutoRenew {
				t.Fatal("certificate remained associated")
			}
			var persisted jobstore.Record
			if err := db.First(&persisted, "id = ?", run.ID).Error; err != nil {
				t.Fatal("execution history deleted", err)
			}
		})
	}
}

func TestProviderDirectoryCountsReferencesAndRedactsSecrets(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	service := network.ProviderDirectory{Store: ProviderAccounts{DB: db}}
	accounts, err := service.List(context.Background(), 1)
	if err != nil || len(accounts) != 1 || accounts[0].UsageCount != 3 {
		t.Fatal(accounts, err)
	}
	data, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	var values []map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if _, exists := values[0]["credential_ciphertext"]; exists {
		t.Fatal("credential included in list")
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(context.Background(), 1); !errors.Is(err, network.ErrProviderPermission) {
		t.Fatal("revoked administrator listed accounts", err)
	}
	if _, err := service.Delete(context.Background(), 1, 1); !errors.Is(err, network.ErrProviderPermission) {
		t.Fatal("revoked administrator deleted account", err)
	}
}

func TestProviderDeletionAuditFailureRestoresAllAssociations(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	if err := db.Callback().Create().Before("gorm:create").Register("fail_delete_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("fail_delete_audit")
	service := network.ProviderDirectory{Store: ProviderAccounts{DB: db}}
	if _, err := service.Delete(context.Background(), 1, 1); err == nil {
		t.Fatal("unaudited deletion succeeded")
	}
	var cert model.ManagedCertificate
	if err := db.First(&cert, 9).Error; err != nil {
		t.Fatal(err)
	}
	if cert.ProviderAccountID == nil || !cert.AutoRenew {
		t.Fatal("certificate detach was not rolled back")
	}
	var count int64
	if err := db.Model(&model.ManagedDNSRecord{}).Where("provider_account_id = ?", 1).Count(&count).Error; err != nil || count != 2 {
		t.Fatal(count, err)
	}
}

func TestProviderDeletionProtectsInspectionAndOrphanedOperationEvidence(t *testing.T) {
	db, _ := administrationFixture(t)
	prepareProviderDirectory(t, db)
	service := network.ProviderDirectory{Store: ProviderAccounts{DB: db}}
	inspection := submit(t, jobstore.New(db), "inspection", "dns_reconcile", "dns-inspection:7")
	if err := db.Model(&jobstore.Record{}).Where("id = ?", inspection.ID).Update("state", "unknown").Error; err != nil {
		t.Fatal(err)
	}
	var blocked *network.ProviderDeletionBlocked
	if _, err := service.Delete(context.Background(), 1, 1); !errors.As(err, &blocked) || blocked.Blockers["unverified_jobs"] != 1 {
		t.Fatal("inspection evidence removed", err)
	}
	if err := db.Model(&jobstore.Record{}).Where("id = ?", inspection.ID).Update("state", "canceled").Error; err != nil {
		t.Fatal(err)
	}
	op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7777, Status: "failed", OperationType: "sync"}
	if err := db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}
	submit(t, jobstore.New(db), fmt.Sprintf("dns_operation:%d", op.ID), "dns_operation", "dns:7777")
	if _, err := service.Delete(context.Background(), 1, 1); !errors.As(err, &blocked) || blocked.Blockers["unverified_jobs"] != 1 {
		t.Fatal("orphaned operation evidence removed", err)
	}
}

func TestProviderDeletionObservesConcurrentJobAdmission(t *testing.T) {
	db, other := administrationFixture(t)
	prepareProviderDirectory(t, db)
	locked, release := make(chan struct{}), make(chan struct{})
	admitted := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		admitted <- jobstore.New(db).WithLedgerLock(ctx, func(tx *gorm.DB) error {
			close(locked)
			<-release
			op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, Status: "running", OperationType: "sync"}
			if err := tx.Create(&op).Error; err != nil {
				return err
			}
			_, err := jobstore.New(tx).Submit(ctx, jobs.Submission{Owner: "system", Key: fmt.Sprintf("dns_operation:%d", op.ID), Handler: "dns_operation", Resource: "dns:7", Payload: `{}`})
			return err
		})
	}()
	select {
	case <-locked:
	case err := <-admitted:
		t.Fatalf("admission lock failed: %v", err)
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	deleted := make(chan error, 1)
	go func() {
		_, err := (network.ProviderDirectory{Store: ProviderAccounts{DB: other}}).Delete(ctx, 1, 1)
		deleted <- err
	}()
	close(release)
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	var blocked *network.ProviderDeletionBlocked
	if err := <-deleted; !errors.As(err, &blocked) {
		t.Fatal("concurrently accepted work lost", err)
	}
	var count int64
	if err := other.Model(&model.ProviderAccount{}).Where("id = ?", 1).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
