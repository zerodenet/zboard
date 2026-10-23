package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestConsumptionRefundSchemaRecovery(t *testing.T) {
	assertConsumptionRefundSchemaRecovery(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundSchemaRecovery(t *testing.T) {
	assertConsumptionRefundSchemaRecovery(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundSchemaRecovery(t *testing.T, f orderFixture) {
	const version = "0052_consumption_refunds.up.sql"
	// Resume empty DDL without a marker; keep unrelated order data intact.
	order := f.create(t, 0)
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Migrator().DropTable(&model.ConsumptionRefund{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal("empty interrupted migration did not recover", err)
	}
	if err := f.h.db.First(&order, order.ID).Error; err != nil || order.Status != "pending" {
		t.Fatal("migration changed historical order", err)
	}
	for range 2 {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal("repeated migration failed", err)
		}
	}
	if err := f.h.db.Migrator().DropIndex(&model.ConsumptionRefund{}, "idx_consumption_refunds_request_key"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil || !f.h.db.Migrator().HasIndex(&model.ConsumptionRefund{}, "idx_consumption_refunds_request_key") {
		t.Fatal("idempotency index did not recover", err)
	}
	if err := f.h.db.Migrator().DropTable(&model.ConsumptionRefund{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("recorded missing financial table was replaced by an empty one")
	}
	if f.h.db.Migrator().HasTable(&model.ConsumptionRefund{}) {
		t.Fatal("missing financial evidence was reconstructed")
	}
}

func TestConsumptionRefundSchemaPreservesApprovedEvidence(t *testing.T) {
	assertConsumptionRefundEvidenceRecovery(t, newOrderFixture(t))
}
func TestMySQLConsumptionRefundSchemaPreservesApprovedEvidence(t *testing.T) {
	assertConsumptionRefundEvidenceRecovery(t, newMySQLOrderFixture(t))
}
func assertConsumptionRefundEvidenceRecovery(t *testing.T, f orderFixture) {
	order := refundPaidOrder(t, f, 0)
	refundCloseOrder(t, f, order)
	in := refundRequest(t, f, "refund-evidence-migration", 300)
	refunded, err := f.h.services.ConsumptionRefunds.Approve(t.Context(), 1, order.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	read, err := f.h.services.ConsumptionRefunds.Read(t.Context(), 1, refunded.ID)
	if err != nil || read.EntryID != refunded.EntryID || read.AmountMinor != 300 {
		t.Fatal("migration changed approved refund", read, err)
	}
	if err := f.h.db.Migrator().DropTable(&model.ConsumptionRefund{}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0052_consumption_refunds.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil || f.h.db.Migrator().HasTable(&model.ConsumptionRefund{}) {
		t.Fatal("surviving wallet/audit proof allowed empty approval reconstruction", err)
	}
	var entry model.WalletEntry
	if err := f.h.db.First(&entry, refunded.EntryID).Error; err != nil || entry.BalanceDelta != 300 {
		t.Fatal("failed migration changed financial evidence", err)
	}
}
