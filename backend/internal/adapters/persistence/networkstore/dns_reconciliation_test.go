package networkstore

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type dnsObservationFunc func(context.Context, network.DNSReconciliationScope) (network.DNSObservation, error)

func (f dnsObservationFunc) ObserveDNS(c context.Context, s network.DNSReconciliationScope) (network.DNSObservation, error) {
	return f(c, s)
}

func TestDNSReconciliationRequiresCurrentEvidenceAndAtomicCompletion(t *testing.T) {
	for _, scenario := range []string{"matched", "mismatch", "read-error", "revision-changed", "admin-revoked", "account-disabled", "missing-identity", "audit-failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, _ := administrationFixture(t)
			seedLegacyResources(t, db)
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error)
			check(db.Model(&model.ProviderAccount{}).Where("id = ?", 1).Updates(map[string]any{"provider_key": "cloudflare", "status": "active"}).Error)
			check(db.Model(&model.ManagedDNSRecord{}).Where("id = ?", 7).Updates(map[string]any{"provider_zone_id": "zone", "provider_record_id": "remote", "desired_hash": "desired", "domain_name": "example.test", "record_type": "A"}).Error)
			op := model.ProviderOperation{ProviderAccountID: 1, ResourceType: "dns_record", ResourceID: 7, OperationType: "sync", Status: "running", Phase: "applying_record"}
			check(db.Create(&op).Error)
			_, err := (Recovery{DB: db}).RecoverLegacyNative(context.Background())
			check(err)
			var run jobstore.Record
			check(db.Where("handler = ?", "dns_operation").First(&run).Error)
			if scenario == "missing-identity" {
				check(db.Model(&model.ManagedDNSRecord{}).Where("id = ?", 7).Update("provider_record_id", "").Error)
			}
			if scenario == "audit-failure" {
				check(db.Callback().Create().Before("gorm:create").Register("fail_reconciliation_audit", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_logs" {
						tx.AddError(errors.New("audit unavailable"))
					}
				}))
				defer db.Callback().Create().Remove("fail_reconciliation_audit")
			}
			reads := 0
			observer := dnsObservationFunc(func(_ context.Context, s network.DNSReconciliationScope) (network.DNSObservation, error) {
				reads++
				observation := network.DNSObservation{ZoneID: s.ZoneID, RemoteID: s.RemoteID, Domain: s.Domain, RecordType: s.RecordType, Hash: s.DesiredHash}
				switch scenario {
				case "mismatch":
					observation.Hash = "other"
				case "read-error":
					return observation, errors.New("provider unavailable")
				case "revision-changed":
					check(db.Model(&model.ManagedDNSRecord{}).Where("id = ?", 7).Update("revision", gorm.Expr("revision + 1")).Error)
				case "admin-revoked":
					check(db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error)
				case "account-disabled":
					check(db.Model(&model.ProviderAccount{}).Where("id = ?", 1).Update("status", "disabled").Error)
				}
				return observation, nil
			})
			service := network.DNSReconciliation{Store: DNSReconciliation{DB: db}, Observer: observer}
			err = service.Reconcile(context.Background(), 1, run.ID)
			var actual jobstore.Record
			check(db.First(&actual, "id = ?", run.ID).Error)
			var operation model.ProviderOperation
			check(db.First(&operation, op.ID).Error)
			var record model.ManagedDNSRecord
			check(db.First(&record, 7).Error)
			if scenario == "matched" {
				check(err)
				if actual.State != string(jobs.Succeeded) || operation.Phase != "reconciled" || record.ObservedHash != "desired" {
					t.Fatal(actual.State, operation.Phase, record.ObservedHash)
				}
				if service.Reconcile(context.Background(), 1, run.ID) == nil || reads != 1 {
					t.Fatal("completed source inspected again")
				}
			} else {
				if err == nil || actual.State != string(jobs.Unknown) || operation.Status != "running" || record.ObservedHash == "desired" {
					t.Fatal("unverified or partial completion", err, actual.State, operation.Status, record.ObservedHash)
				}
				if scenario == "missing-identity" && reads != 0 {
					t.Fatal("unowned remote lookup")
				}
			}
		})
	}
}
