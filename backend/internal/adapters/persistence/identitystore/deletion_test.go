package identitystore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func deletionFixture(t *testing.T) (*gorm.DB, Deletion, model.User, time.Time) {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	admin := model.User{Email: "admin@example.test", Password: "hash", Status: "active", IsAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	return db, Deletion{DB: db}, admin, time.Now().UTC().Truncate(time.Second)
}
func deletionUser(t *testing.T, db *gorm.DB, email string, created time.Time) model.User {
	t.Helper()
	u := model.User{Email: email, Password: "hash", Status: "active", CreatedAt: created}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}
func TestAccountHardDeletionRemovesAuthenticationAndRetainsHistory(t *testing.T) {
	db, s, admin, now := deletionFixture(t)
	ctx := context.Background()
	u := deletionUser(t, db, "old@example.test", now.AddDate(0, 0, -90))
	fulfilled := now.AddDate(0, 0, -60)
	order := model.Order{UserID: u.ID, TradeNo: "historic", OrderType: "topup", Status: "paid", Currency: "USD", FulfilledAt: &fulfilled}
	for _, row := range []any{&order, &model.WalletAccount{UserID: u.ID, Currency: "USD"}, &model.UserAPIToken{UserID: u.ID, TokenHash: "hash", TokenPrefix: "token", Scopes: "[]"}, &model.ExternalIdentity{ID: "external", UserID: u.ID, PluginID: "oidc", Publisher: "publisher", Issuer: "issuer", Subject: "old"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	record := model.TrafficRecord{UserID: u.ID, RawBytes: 123, UsedBytes: 123, Meta: "{}", At: now.AddDate(0, 0, -40)}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := s.Inspect(ctx, admin.ID, u.ID, now)
	if err != nil || !preview.Allowed {
		t.Fatal(preview, err)
	}
	if err := s.Remove(ctx, admin.ID, u.ID, now); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Unscoped().Model(&model.User{}).Where("id=?", u.ID).Count(&count)
	if count != 0 {
		t.Fatal("account was soft deleted")
	}
	for _, row := range []any{&model.UserAPIToken{}, &model.ExternalIdentity{}} {
		db.Model(row).Where("user_id=?", u.ID).Count(&count)
		if count != 0 {
			t.Fatal("authentication association survived", row)
		}
	}
	if _, err := (Accounts{DB: db}).CurrentAccount(ctx, u.ID); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal("deleted account authenticated", err)
	}
	var retained model.TrafficRecord
	if err := db.First(&retained, record.ID).Error; err != nil || retained.UserID != u.ID || retained.UsedBytes != 123 {
		t.Fatal("traffic attribution lost", retained, err)
	}
	if err := db.First(&model.Order{}, order.ID).Error; err != nil {
		t.Fatal("financial order lost", err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("restart after deletion", err)
	}
	next := deletionUser(t, db, u.Email, now)
	if next.ID <= u.ID {
		t.Fatal("historical identity reused", next.ID, u.ID)
	}
	if err := db.Delete(&model.AccountSubject{}, u.ID).Error; err == nil {
		t.Fatal("financial subject lost referential protection")
	}
	if err := db.First(&model.AccountSubject{}, u.ID).Error; err != nil {
		t.Fatal("historical subject removed", err)
	}
}
func TestAccountDeletionGuardsRecheckAndAuditRollback(t *testing.T) {
	db, s, admin, now := deletionFixture(t)
	ctx := context.Background()
	u := deletionUser(t, db, "guard@example.test", now.AddDate(0, 0, -50))
	if err := s.Remove(ctx, u.ID, admin.ID, now); !errors.Is(err, identity.ErrPermission) {
		t.Fatal("non-admin allowed", err)
	}
	var blocked *identity.DeletionBlocked
	if err := s.Remove(ctx, admin.ID, admin.ID, now); !errors.As(err, &blocked) {
		t.Fatal("admin deleted", err)
	}
	preview, err := s.Inspect(ctx, admin.ID, u.ID, now)
	if err != nil || !preview.Allowed {
		t.Fatal(preview, err)
	}
	if err := db.Create(&model.WalletAccount{UserID: u.ID, Currency: "USD", BalanceMinor: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, admin.ID, u.ID, now); !errors.As(err, &blocked) {
		t.Fatal("stale preview allowed deletion", err)
	}
	if err := db.Model(&model.WalletAccount{}).Where("user_id=?", u.ID).Update("balance_minor", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_deletion_audit BEFORE INSERT ON audit_logs WHEN NEW.action='account.delete' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, admin.ID, u.ID, now); err == nil {
		t.Fatal("audit failure ignored")
	}
	if err := db.First(&model.User{}, u.ID).Error; err != nil {
		t.Fatal("delete escaped audit transaction", err)
	}
}
func TestAutomaticCleanupDisabledByDefaultAndRestartsGraceAfterExpiry(t *testing.T) {
	db, s, _, now := deletionFixture(t)
	ctx := context.Background()
	unused := deletionUser(t, db, "unused@example.test", now.AddDate(0, 0, -60))
	recent := deletionUser(t, db, "recent@example.test", now.AddDate(0, 0, -2))
	expired := deletionUser(t, db, "expired@example.test", now.AddDate(0, 0, -90))
	subscriber := deletionUser(t, db, "paying@example.test", now.AddDate(0, 0, -90))
	funded := deletionUser(t, db, "funded@example.test", now.AddDate(0, 0, -90))
	for _, sub := range []model.Subscription{{UserID: expired.ID, EndAt: now.AddDate(0, 0, -1), Status: "expired"}, {UserID: subscriber.ID, EndAt: now.AddDate(0, 0, 5), Status: "active"}} {
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.WalletAccount{UserID: funded.ID, Currency: "USD", BalanceMinor: 100}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(ctx, now, 100); err != nil || n != 0 {
		t.Fatal("default cleanup enabled", n, err)
	}
	if err := db.Create(&model.SystemConfig{ConfigKey: identity.CleanupDaysKey, Value: "30", ValueType: "int"}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(ctx, now, 100); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var count int64
	db.Unscoped().Model(&model.User{}).Where("id=?", unused.ID).Count(&count)
	if count != 0 {
		t.Fatal("unused account survived")
	}
	for _, u := range []model.User{recent, expired, subscriber, funded} {
		if err := db.First(&model.User{}, u.ID).Error; err != nil {
			t.Fatal("protected account removed", u.Email, err)
		}
	}
	// Reaching the end wraps the cursor. The next sweep can inspect accounts
	// newly eligible after their last subscription's expiry grace period.
	if _, err := s.Cleanup(ctx, now.AddDate(0, 0, 31), 100); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(ctx, now.AddDate(0, 0, 31), 100); err != nil || n != 2 {
		t.Fatal("grace/cursor", n, err)
	}
	if err := db.Model(&model.SystemConfig{}).Where("config_key=?", identity.CleanupDaysKey).Update("value", "0").Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(ctx, now.AddDate(1, 0, 0), 100); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestAutomaticCleanupScanWindowIsBounded(t *testing.T) {
	db, s, _, now := deletionFixture(t)
	users := make([]model.User, 600)
	for i := range users {
		users[i] = model.User{Email: fmt.Sprintf("protected-%d@example.test", i), Password: "hash", Status: "active", CreatedAt: now}
	}
	if err := db.CreateInBatches(&users, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemConfig{ConfigKey: identity.CleanupDaysKey, Value: "30", ValueType: "int"}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(context.Background(), now, 100); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	var state model.AccountCleanupState
	if err := db.First(&state, 1).Error; err != nil || state.LastUserID != users[498].ID {
		t.Fatal("unbounded scan", state, err)
	}
}
