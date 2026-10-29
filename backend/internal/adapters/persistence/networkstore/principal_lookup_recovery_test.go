package networkstore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
)

// One isolated schema, two independent DB pools. No handler/worker is started.
func TestPrincipalLookupInvalidationRecoveryMySQLAndSQLite(t *testing.T) {
	a, b := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, a)
	cipher, err := security.NewCredentialCipher(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "principal-recovery-secret-fixture"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{1, 2} {
		sub := model.Subscription{ID: id, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "canceled", Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)}
		if err := a.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		credential := model.ProtocolCredential{ID: id, SubscriptionID: id, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: []string{"", "repair-secret", "repair-empty"}[id], PrincipalKey: []string{"", "repair-key-secret", "repair-key-empty"}[id], Status: "revoked", ExpiresAt: now, PrincipalLookupVersion: 1}
		if id == 1 {
			credential.Secret = encrypted
			credential.SecretPrincipalDigest = entitlements.RetiredPrincipalHash(1, secret)
		}
		if err := a.Create(&credential).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := principalRepairCanonical(t, b)
	checkSource := func() {
		t.Helper()
		if !reflect.DeepEqual(snapshot, principalRepairCanonical(t, b)) {
			t.Fatal("canonical credential source changed during derived repair")
		}
	}
	checkLookup := func() {
		t.Helper()
		found, err := (credentialstore.PrincipalIndex{DB: b, Cipher: cipher}).Lookup(context.Background(), 1, secret)
		if err != nil || found.ID != 1 {
			t.Fatal("secret lookup was not rebuilt from encrypted canonical source", err)
		}
		var stored model.ProtocolCredential
		if err := b.First(&stored, 1).Error; err != nil {
			t.Fatal(err)
		}
		if stored.RetiredPrincipalHash != "" || stored.PrincipalKey == secret || stored.SecretPrincipalDigest != entitlements.RetiredPrincipalHash(1, secret) {
			t.Fatal("lookup bypassed rebuilt secret digest")
		}
		checkSource()
	}
	checkComplete := func() {
		t.Helper()
		var rows []model.ProtocolCredential
		if err := b.Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].PrincipalLookupVersion != 1 || rows[1].PrincipalLookupVersion != 1 || rows[1].SecretPrincipalDigest != "" {
			t.Fatal("complete coverage or legitimate empty secret digest changed")
		}
	}
	t.Run("complete_v1_is_not_invalidated", func(t *testing.T) {
		updates, adds := 0, 0
		stop := principalRepairCallbacks(t, a, func(tx *gorm.DB) {
			if principalRepairInvalidation(tx) {
				updates++
			}
		}, func(tx *gorm.DB) {
			if tx.Error == nil && principalRepairAdd(tx) {
				adds++
			}
		})
		before := principalRepairReceipt(t, b)
		for range 2 {
			if err := datastore.RunMigrations(a); err != nil {
				t.Fatal(err)
			}
		}
		stop()
		if updates != 0 || adds != 0 {
			t.Fatalf("complete structure issued %d invalidations/%d ADDs", updates, adds)
		}
		checkComplete()
		checkSource()
		if !reflect.DeepEqual(before, principalRepairReceipt(t, b)) {
			t.Fatal("existing0046 receipt changed")
		}
	})
	t.Run("update_failure_precedes_every_add", func(t *testing.T) {
		principalRepairDrop(t, a, "secret_principal_digest")
		principalRepairUnrecord(t, a)
		sentinel := errors.New("test principal invalidation rejected")
		updates, adds := 0, 0
		stop := principalRepairCallbacks(t, a, func(tx *gorm.DB) {
			if principalRepairInvalidation(tx) {
				updates++
				tx.AddError(sentinel)
			}
		}, func(tx *gorm.DB) {
			if tx.Error == nil && principalRepairAdd(tx) {
				adds++
			}
		})
		err := datastore.RunMigrations(a)
		stop()
		if !errors.Is(err, sentinel) {
			t.Fatal("invalidation failure was not propagated", err)
		}
		if updates != 1 || adds != 0 {
			t.Fatalf("rejected invalidation followed DDL: UPDATE attempts=%d committed ADDs=%d", updates, adds)
		}
		if b.Migrator().HasColumn("protocol_credentials", "secret_principal_digest") {
			t.Fatal("rejected invalidation still added digest")
		}
		principalRepairVersions(t, b, 1)
		if principalRepairReceipt(t, b).Present {
			t.Fatal("failed repair recorded0046 completion")
		}
		checkSource()
		if err := datastore.RunMigrations(b); err != nil {
			t.Fatal(err)
		}
		principalRepairVersions(t, a, 0)
		if !principalRepairReceipt(t, a).Present {
			t.Fatal("independent-pool repair did not record completion")
		}
		checkLookup()
	})
	t.Run("committed_add_failure_cannot_leave_old_coverage", func(t *testing.T) {
		principalRepairDrop(t, a, "principal_digest", "secret_principal_digest")
		principalRepairUnrecord(t, a)
		sentinel := errors.New("test failure after committed principal ADD")
		adds := 0
		stop := principalRepairCallbacks(t, a, func(*gorm.DB) {}, func(tx *gorm.DB) {
			if tx.Error == nil && principalRepairAdd(tx) {
				adds++
				tx.AddError(sentinel)
			}
		})
		err := datastore.RunMigrations(a)
		stop()
		if !errors.Is(err, sentinel) || adds != 1 {
			t.Fatal("did not fail after exactly one actual committed ADD", err, adds)
		}
		if !b.Migrator().HasColumn("protocol_credentials", "principal_digest") {
			t.Fatal("actual ADD did not persist across pools")
		}
		principalRepairVersions(t, b, 0)
		if principalRepairReceipt(t, b).Present {
			t.Fatal("post-DDL failure recorded completion")
		}
		checkSource()
		if err := datastore.RunMigrations(b); err != nil {
			t.Fatal(err)
		}
		principalRepairVersions(t, a, 0)
		checkLookup()
	})
	t.Run("missing_version_defaults_to_unknown", func(t *testing.T) {
		principalRepairDrop(t, a, "principal_lookup_version")
		updates, adds := 0, 0
		stop := principalRepairCallbacks(t, a, func(tx *gorm.DB) {
			if principalRepairInvalidation(tx) {
				updates++
			}
		}, func(tx *gorm.DB) {
			if tx.Error == nil && principalRepairAdd(tx) {
				adds++
			}
		})
		before := principalRepairReceipt(t, b)
		if err := datastore.RunMigrations(a); err != nil {
			stop()
			t.Fatal(err)
		}
		stop()
		if updates != 0 || adds != 1 {
			t.Fatalf("missing-version repair invalidations=%d ADDs=%d", updates, adds)
		}
		principalRepairVersions(t, b, 0)
		checkSource()
		if !reflect.DeepEqual(before, principalRepairReceipt(t, b)) {
			t.Fatal("recorded recovery rewrote0046 receipt")
		}
		checkLookup()
	})
	t.Run("three_missing_columns_batch_and_index_repair", func(t *testing.T) {
		principalRepairDrop(t, a, "principal_digest", "secret_principal_digest", "principal_lookup_version")
		updates, adds := 0, 0
		stop := principalRepairCallbacks(t, a, func(tx *gorm.DB) {
			if principalRepairInvalidation(tx) {
				updates++
			}
		}, func(tx *gorm.DB) {
			if tx.Error == nil && principalRepairAdd(tx) {
				adds++
			}
		})
		if err := datastore.RunMigrations(a); err != nil {
			stop()
			t.Fatal(err)
		}
		stop()
		want := 1
		if datastore.IsSQLite(a) {
			want = 3
		}
		if updates != 0 || adds != want {
			t.Fatalf("three-column repair invalidations=%d ADDs=%d, want0/%d", updates, adds, want)
		}
		principalRepairVersions(t, b, 0)
		checkSource()
		if err := b.Migrator().DropIndex("protocol_credentials", "idx_credential_lookup_backfill"); err != nil {
			t.Fatal(err)
		}
		if err := b.Exec("CREATE INDEX idx_credential_lookup_backfill ON protocol_credentials(node_id)").Error; err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(a); err != nil {
			t.Fatal(err)
		}
		indexes, err := b.Migrator().GetIndexes("protocol_credentials")
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]string{"idx_credential_principal_digest": "node_id,principal_digest", "idx_credential_secret_digest": "node_id,secret_principal_digest", "idx_credential_lookup_backfill": "node_id,principal_lookup_version,id"}
		for _, index := range indexes {
			if want, ok := expected[index.Name()]; ok {
				if strings.Join(index.Columns(), ",") != want {
					t.Fatal("derived index shape changed", index.Name())
				}
				delete(expected, index.Name())
			}
		}
		if len(expected) != 0 {
			t.Fatal("derived indexes missing")
		}
		checkLookup()
		checkComplete()
	})
}

func principalRepairCanonical(t *testing.T, db *gorm.DB) []model.ProtocolCredential {
	t.Helper()
	var rows []model.ProtocolCredential
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].PrincipalDigest = ""
		rows[i].SecretPrincipalDigest = ""
		rows[i].PrincipalLookupVersion = 0
	}
	return rows
}
func principalRepairVersions(t *testing.T, db *gorm.DB, want uint8) {
	t.Helper()
	var rows []struct{ PrincipalLookupVersion uint8 }
	if err := db.Table("protocol_credentials").Select("principal_lookup_version").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal("credential source row count changed")
	}
	for _, row := range rows {
		if row.PrincipalLookupVersion != want {
			t.Fatalf("persisted coverage=%d, want%d", row.PrincipalLookupVersion, want)
		}
	}
}
func principalRepairDrop(t *testing.T, db *gorm.DB, columns ...string) {
	t.Helper()
	for _, name := range []string{"idx_credential_principal_digest", "idx_credential_secret_digest", "idx_credential_lookup_backfill"} {
		if db.Migrator().HasIndex("protocol_credentials", name) {
			if err := db.Migrator().DropIndex("protocol_credentials", name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, column := range columns {
		if err := db.Exec("ALTER TABLE protocol_credentials DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
}

type principalRepairReceiptState struct {
	Present   bool
	AppliedAt time.Time
}

func principalRepairReceipt(t *testing.T, db *gorm.DB) principalRepairReceiptState {
	t.Helper()
	var row struct{ AppliedAt time.Time }
	result := db.Table("schema_migrations").Where("version = ?", "0046_principal_lookup_index.up.sql").Limit(1).Find(&row)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	return principalRepairReceiptState{Present: result.RowsAffected > 0, AppliedAt: row.AppliedAt}
}
func principalRepairUnrecord(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0046_principal_lookup_index.up.sql").Error; err != nil {
		t.Fatal(err)
	}
}
func principalRepairInvalidation(tx *gorm.DB) bool {
	return strings.HasPrefix(strings.ToLower(tx.Statement.SQL.String()), "update protocol_credentials set principal_lookup_version = 0 where")
}
func principalRepairAdd(tx *gorm.DB) bool {
	sql := strings.ToLower(tx.Statement.SQL.String())
	return strings.HasPrefix(sql, "alter table ") && strings.Contains(sql, "protocol_credentials") && strings.Contains(sql, "add column")
}
func principalRepairCallbacks(t *testing.T, db *gorm.DB, before, after func(*gorm.DB)) func() {
	t.Helper()
	const start = "test:principal_repair_before"
	const end = "test:principal_repair_after"
	if err := db.Callback().Raw().Before("gorm:raw").Register(start, before); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().After("gorm:raw").Register(end, after); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if err := db.Callback().Raw().Remove(start); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Raw().Remove(end); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(stop)
	return stop
}
