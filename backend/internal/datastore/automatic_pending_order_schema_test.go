package datastore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteAutomaticPendingMigrationCopyAndMissingAuthority(t *testing.T) {
	assertAutomaticPendingMigration(t, false)
}
func TestMySQLAutomaticPendingMigrationCopyAndMissingAuthority(t *testing.T) {
	assertAutomaticPendingMigration(t, true)
}
func assertAutomaticPendingMigration(t *testing.T, mysql bool) {
	source := diagnosticSchemaDB(t, mysql)
	at := time.Now().UTC().Truncate(time.Millisecond)
	user := model.User{Email: "pending-auto-schema@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "pending-auto-schema", Code: "pending-auto-schema", IsEnabled: true}
	for _, row := range []any{&user, &group} {
		if err := source.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "pending-auto-schema", Slug: "pending-auto-schema", NodeGroupID: group.ID, IsActive: true}
	if err := source.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Name: "pending-auto-schema", Code: "pending-auto-schema", Currency: "CNY", BillingUnit: "month", BillingValue: 1, RenewalEffect: "extend_only", IsActive: true}
	if err := source.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, StartAt: at, EndAt: at.AddDate(0, 1, 0), Status: "active", Config: "{}", EntitlementEpoch: 17}
	if err := source.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	// Explicit structural history fixture: epoch17 requires all17 closed source
	// epochs. These zero-usage/reset identities are not paid purchase evidence.
	closed := make([]model.ClosedEntitlement, 17)
	for epoch := range closed {
		start := at.AddDate(0, epoch-17, 0)
		end := start.AddDate(0, 1, 0)
		closed[epoch] = model.ClosedEntitlement{SubscriptionID: sub.ID, Epoch: uint64(epoch), UserID: user.ID, StartAt: start, EndAt: end, ClosedAt: end, UpdatedAt: end}
	}
	if err := source.CreateInBatches(&closed, 17).Error; err != nil {
		t.Fatal(err)
	}

	// Structural preservation fixture only: these unverified pending orders do
	// not claim paid receipts, valid debit approval or entitlement value evidence.
	root := model.Order{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, SubscriptionID: sub.ID, TradeNo: "pending-schema-root", Status: "pending", Currency: "CNY"}
	if err := source.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	order := model.Order{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TargetSubscriptionID: &sub.ID, OrderType: "renewal", TradeNo: "pending-schema-stage", Status: "pending", Currency: "CNY"}
	if err := source.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	row := model.SubscriptionAutomaticRenewal{SubscriptionID: sub.ID, UserID: user.ID, Enabled: false, PlanSKUID: sku.ID, RootOrderID: root.ID, AgreementDigest: strings.Repeat("a", 64), Revision: 8, State: "disabled", NextAttemptAt: at, PendingOrderID: &order.ID, PendingConsentRevision: 7, PendingEndAt: &sub.EndAt, PendingEpoch: 17, PendingAttempt: 3}
	if err := source.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.ValidateAutomaticPendingBinaryRollback(source); err == nil {
		t.Fatal("pending link treated as old-binary compatible")
	}
	for range 2 {
		if err := datastore.RunMigrations(source); err != nil {
			t.Fatal(err)
		}
	}
	var saved model.SubscriptionAutomaticRenewal
	if err := source.First(&saved, "subscription_id = ?", sub.ID).Error; err != nil || saved.Enabled || saved.PendingOrderID == nil || *saved.PendingOrderID != order.ID || saved.PendingConsentRevision != 7 || saved.Revision != 8 || saved.PendingAttempt != 3 || saved.PendingEpoch != 17 || saved.LastOrderID != nil {
		t.Fatalf("migration changed revoked queued consent: %+v %v", saved, err)
	}
	target := diagnosticSchemaDB(t, mysql)
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(source, tx)
	}); err != nil {
		t.Fatal(err)
	}
	var copied model.SubscriptionAutomaticRenewal
	if err := target.First(&copied, "subscription_id = ?", sub.ID).Error; err != nil || copied.PendingOrderID == nil || *copied.PendingOrderID != order.ID || copied.PendingConsentRevision != 7 || copied.PendingAttempt != 3 || copied.PendingEpoch != 17 || copied.PendingEndAt == nil || !copied.PendingEndAt.Equal(sub.EndAt) || copied.Enabled || copied.LastOrderID != nil {
		t.Fatalf("copy lost queued cancellation identity: %+v %v", copied, err)
	}
	if err := source.Migrator().DropColumn("subscription_automatic_renewals", "pending_epoch"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(source); err == nil || !strings.Contains(err.Error(), "authority column missing") {
		t.Fatalf("recorded missing pending authority defaulted: %v", err)
	}
	if source.Migrator().HasColumn("subscription_automatic_renewals", "pending_epoch") {
		t.Fatal("lost epoch invented as0")
	}
	if err := datastore.CopyApplicationData(source, target); err == nil {
		t.Fatal("copy adopted missing recorded pending authority")
	}
	var retained int64
	if err := source.Table("subscription_automatic_renewals").Where("subscription_id = ? AND pending_order_id = ?", sub.ID, order.ID).Count(&retained).Error; err != nil || retained != 1 {
		t.Fatal("refusal erased pending link", retained, err)
	}
}

func TestSQLiteAutomaticPendingInterruptedEmptyDDLAndPartialIndexes(t *testing.T) {
	assertAutomaticPendingEmptyDDL(t, false)
}
func TestMySQLAutomaticPendingInterruptedEmptyDDLAndPartialIndexes(t *testing.T) {
	assertAutomaticPendingEmptyDDL(t, true)
}
func assertAutomaticPendingEmptyDDL(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0066_automatic_renewal_pending_orders.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn("subscription_automatic_renewals", "pending_epoch"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("empty interrupted DDL did not resume", err)
	}
	for _, name := range []string{"pending_order_id", "pending_consent_revision", "pending_end_at", "pending_epoch", "pending_attempt"} {
		if !db.Migrator().HasColumn("subscription_automatic_renewals", name) {
			t.Fatal("missing resumed column", name)
		}
	}
	if mysql {
		return
	}
	for _, spec := range []struct {
		name, columns string
		unique        bool
	}{{"idx_auto_pending_order", "pending_order_id", true}, {"idx_auto_pending_due", "pending_order_id,next_attempt_at,user_id", false}} {
		if err := db.Exec("DROP INDEX " + spec.name).Error; err != nil {
			t.Fatal(err)
		}
		prefix := "CREATE INDEX "
		if spec.unique {
			prefix = "CREATE UNIQUE INDEX "
		}
		if err := db.Exec(prefix + spec.name + " ON subscription_automatic_renewals(" + spec.columns + ") WHERE enabled = 1").Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "index must cover all rows") {
			t.Fatalf("partial pending index accepted: %v", err)
		}
		if err := db.Exec("DROP INDEX " + spec.name).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(prefix + spec.name + " ON subscription_automatic_renewals(" + spec.columns + ")").Error; err != nil {
			t.Fatal(err)
		}
	}
}
