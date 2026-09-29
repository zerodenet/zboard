package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMySQLHourlyRetirementAndAuditFailureRemainAtomic(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Updates(map[string]any{"lifecycle": "fixed", "ends_on_quota_exhaustion": true, "flow_total": 30}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("ALTER TABLE audit_logs ADD CONSTRAINT reject_hourly_cleanup CHECK (action <> 'subscription.cleanup')").Error; err != nil {
		t.Fatal(err)
	}
	expire := func() error {
		_, err := h.services.CredentialExpiry().ExpireDue(context.Background(), time.Now().UTC(), 10)
		return err
	}
	if err := expire(); err == nil {
		t.Fatal("cleanup ignored audit failure")
	}
	assert := func(want int64) {
		t.Helper()
		for _, table := range []string{"traffic_records", "traffic_usage_hourly"} {
			var used int64
			if err := h.db.Table(table).Where("subscription_id = ?", credential.SubscriptionID).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != want {
				t.Fatalf("%s used=%d want=%d: %v", table, used, want, err)
			}
		}
	}
	assert(30)
	if err := h.db.First(&model.Subscription{}, credential.SubscriptionID).Error; err != nil {
		t.Fatal("subscription lost on failed cleanup", err)
	}
	if err := h.db.Exec("ALTER TABLE audit_logs DROP CHECK reject_hourly_cleanup").Error; err != nil {
		t.Fatal(err)
	}
	if err := expire(); err != nil {
		t.Fatal(err)
	}
	assert(0)
	var count int64
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("subscription retained: %d %v", count, err)
	}
	var order model.Order
	if err := h.db.Where("subscription_ended_at IS NOT NULL AND status = 'paid'").First(&order).Error; err != nil || order.SubscriptionFinalFlowUsed != 30 {
		t.Fatalf("history lost: %+v %v", order, err)
	}
}

func TestMySQLTrafficHourlyDatabaseTransferRebuildsLedgerProjection(t *testing.T) {
	source, credential := accountingBenchmarkFixture(t)
	if err := source.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 1)); err != nil {
		t.Fatal(err)
	}
	target, _ := newMySQLPublishHandlers(t)
	copy := func(abort error) error {
		return datastore.WithMigrationSource(context.Background(), source.db, func(snapshot *gorm.DB) error {
			return datastore.WithCopyDestination(context.Background(), target.db, func(tx *gorm.DB) error {
				tables, err := datastore.MigrationTables(tx)
				if err != nil {
					return err
				}
				if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
					return err
				}
				if err := datastore.CopyApplicationData(snapshot, tx); err != nil {
					return err
				}
				return abort
			})
		})
	}
	assert := func(want int64) {
		t.Helper()
		for _, table := range []string{"traffic_records", "traffic_usage_hourly"} {
			var used int64
			if err := target.db.Table(table).Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != want {
				t.Fatalf("%s used=%d want=%d: %v", table, used, want, err)
			}
		}
	}
	abort := errors.New("cancel transfer")
	if err := copy(abort); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	assert(0)
	if err := copy(nil); err != nil {
		t.Fatal(err)
	}
	assert(30)
}
