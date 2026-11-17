package platformstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/security"
	"gorm.io/gorm"
)

func TestMigrationCopierPrincipalLookupCoverage(t *testing.T) {
	principalCopyCoverageMatrix(t, false, false)
}
func TestMySQLMigrationCopierPrincipalLookupCoverage(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) { principalCopyCoverageMatrix(t, !targetMySQL, targetMySQL) })
	}
}
func principalCopyCoverageMatrix(t *testing.T, sourceMySQL, targetMySQL bool) {
	t.Helper()
	for _, tc := range []struct {
		name    string
		missing []string
	}{
		{name: "complete_v1_and_legal_empty_digest"},
		{name: "missing_secret_digest", missing: []string{"secret_principal_digest"}},
		{name: "missing_version", missing: []string{"principal_lookup_version"}},
		{name: "missing_both_digests", missing: []string{"principal_digest", "secret_principal_digest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMigrationCopyFixture(t, sourceMySQL, targetMySQL)
			cipher, secret := principalCopySeed(t, f.source)
			canonical := principalCopyCanonical(t, f.source)
			principalCopyLoseDerived(t, f.source, tc.missing)
			columns, sourceRows := principalCopySourceState(t, f.source)
			if err := f.copier(false).Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err != nil {
				t.Fatal("owned copier rejected source instead of preserving rebuildable coverage", err)
			}
			target := f.destination(t)
			want := uint8(1)
			if len(tc.missing) > 0 {
				want = 0
			}
			principalCopyVersions(t, target, want)
			principalCopyAssertSource(t, f, columns, sourceRows, canonical)
			if !reflect.DeepEqual(canonical, principalCopyCanonical(t, target)) {
				t.Fatal("copy changed canonical credential model")
			}
			var complete migrationPreparation
			if err := target.First(&complete, 1).Error; err != nil || complete.State != "completed" {
				t.Fatal("owned copy did not complete normally", err)
			}
			principalCopyLookup(t, target, cipher, secret)
			if !reflect.DeepEqual(canonical, principalCopyCanonical(t, target)) {
				t.Fatal("lookup reconstruction changed canonical credential model")
			}
			principalCopyAssertSource(t, f, columns, sourceRows, canonical)
		})
	}
}

func TestMigrationCopierPrincipalLookupCompletionRollback(t *testing.T) {
	principalCopyCompletionRollback(t, false, false)
}
func TestMySQLMigrationCopierPrincipalLookupCompletionRollback(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) { principalCopyCompletionRollback(t, !targetMySQL, targetMySQL) })
	}
}
func principalCopyCompletionRollback(t *testing.T, sourceMySQL, targetMySQL bool) {
	t.Helper()
	f := newMigrationCopyFixture(t, sourceMySQL, targetMySQL)
	cipher, secret := principalCopySeed(t, f.source)
	canonical := principalCopyCanonical(t, f.source)
	principalCopyLoseDerived(t, f.source, []string{"secret_principal_digest"})
	columns, sourceRows := principalCopySourceState(t, f.source)
	sentinel := errors.New("test principal-copy completion rejected")
	copied := false
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
		if err := prepareMigrationCopyTestSchema(db); err != nil {
			return err
		}
		if err := db.Callback().Create().After("gorm:create").Register("test:principal_copy_insert", func(tx *gorm.DB) {
			if tx.Statement.Table == "protocol_credentials" && tx.Error == nil {
				copied = true
			}
		}); err != nil {
			return err
		}
		return db.Callback().Update().Before("gorm:update").Register("test:principal_copy_completion", func(tx *gorm.DB) {
			values, ok := tx.Statement.Dest.(map[string]any)
			if tx.Statement.Table != migrationPreparationTable || !ok || values["state"] != "completed" {
				return
			}
			if !copied {
				tx.AddError(errors.New("test completion reached before actual credential INSERT"))
				return
			}
			var coverage struct{ Total, Nonzero int64 }
			if err := tx.Session(&gorm.Session{NewDB: true}).Table("protocol_credentials").Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN principal_lookup_version <> 0 THEN 1 ELSE 0 END), 0) AS nonzero").Scan(&coverage).Error; err != nil {
				tx.AddError(err)
				return
			}
			if coverage.Total != 2 || coverage.Nonzero != 0 {
				tx.AddError(errors.New("test completion cannot see exactly2 zero-coverage rows inside copy transaction"))
				return
			}
			tx.AddError(sentinel)
		})
	}}
	err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID)
	if !errors.Is(err, sentinel) || !copied {
		t.Fatal("completion did not fail after actual zero-coverage INSERT", err)
	}
	target := f.destination(t)
	receipt, err := loadMigrationPreparation(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPreparationManifest(target, receipt, false); err != nil {
		t.Fatal("completion failure changed prepared target", err)
	}
	var count int64
	if err := target.Table("protocol_credentials").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("completion failure committed copied credential rows", err)
	}
	principalCopyAssertSource(t, f, columns, sourceRows, canonical)
	if err := f.copier(false).Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err != nil {
		t.Fatal("owned copy could not retry after rollback", err)
	}
	principalCopyVersions(t, target, 0)
	principalCopyLookup(t, target, cipher, secret)
	if !reflect.DeepEqual(canonical, principalCopyCanonical(t, target)) {
		t.Fatal("retried copy changed canonical credential model")
	}
	principalCopyAssertSource(t, f, columns, sourceRows, canonical)
}

func principalCopySeed(t *testing.T, db *gorm.DB) (*security.CredentialCipher, string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	cipher, err := security.NewCredentialCipher(strings.Repeat("02", 32))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "copy-projection-secret-fixture"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Node{ID: 1, Name: "principal-copy", Config: "{}"},
		&model.NodeGroup{ID: 1, Name: "principal-copy", Code: "principal-copy"},
		&model.Plan{ID: 1, Name: "principal-copy", Slug: "principal-copy", NodeGroupID: 1},
		&model.PlanSKU{ID: 1, PlanID: 1, Name: "principal-copy", Code: "principal-copy", BillingUnit: "month", BillingValue: 1, Currency: "CNY"},
		&model.ProtocolEndpoint{ID: 1, NodeID: 1, Name: "principal-copy", RuntimeKey: "principal-copy", Protocol: "shadowsocks", Address: "test", Port: 443, PublicPort: 443, OptionalConfig: "{}", Tags: "[]"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uint{1, 2} {
		sub := model.Subscription{ID: id, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "canceled", Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(-time.Minute)}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		credential := model.ProtocolCredential{ID: id, UserID: 1, SubscriptionID: id, NodeID: 1, ProtocolEndpointID: 1, CredentialID: fmt.Sprintf("copy-credential-%d", id), PrincipalKey: fmt.Sprintf("copy-stable-key-%d", id), Status: "revoked", ExpiresAt: now, CreatedAt: now, UpdatedAt: now, PrincipalLookupVersion: 1}
		if id == 1 {
			credential.Secret = encrypted
			credential.SecretPrincipalDigest = entitlements.RetiredPrincipalHash(1, secret)
		}
		if err := db.Create(&credential).Error; err != nil {
			t.Fatal(err)
		}
	}
	return cipher, secret
}
func principalCopyLoseDerived(t *testing.T, db *gorm.DB, columns []string) {
	t.Helper()
	for _, column := range columns {
		for _, index := range []struct{ column, name string }{{"principal_digest", "idx_credential_principal_digest"}, {"secret_principal_digest", "idx_credential_secret_digest"}, {"principal_lookup_version", "idx_credential_lookup_backfill"}} {
			if index.column == column && db.Migrator().HasIndex("protocol_credentials", index.name) {
				if err := db.Migrator().DropIndex("protocol_credentials", index.name); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := db.Exec("ALTER TABLE protocol_credentials DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
}
func principalCopyCanonical(t *testing.T, db *gorm.DB) []model.ProtocolCredential {
	t.Helper()
	var rows []model.ProtocolCredential
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].PrincipalDigest = ""
		rows[i].SecretPrincipalDigest = ""
		rows[i].PrincipalLookupVersion = 0
		rows[i].CreatedAt = rows[i].CreatedAt.UTC()
		rows[i].UpdatedAt = rows[i].UpdatedAt.UTC()
		rows[i].ExpiresAt = rows[i].ExpiresAt.UTC()
	}
	return rows
}
func principalCopySourceState(t *testing.T, db *gorm.DB) ([]string, []map[string]any) {
	t.Helper()
	rows, err := db.Table("protocol_credentials").Rows()
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	closeErr := rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	var values []map[string]any
	if err := db.Table("protocol_credentials").Order("id").Find(&values).Error; err != nil {
		t.Fatal(err)
	}
	return columns, values
}
func principalCopyAssertSource(t *testing.T, f migrationCopyFixture, columns []string, values []map[string]any, canonical []model.ProtocolCredential) {
	t.Helper()
	actualColumns, actualValues := principalCopySourceState(t, f.source)
	if !reflect.DeepEqual(columns, actualColumns) || !reflect.DeepEqual(values, actualValues) || !reflect.DeepEqual(canonical, principalCopyCanonical(t, f.source)) {
		t.Fatal("copy changed source credential columns/data or canonical model")
	}
	assertMigrationCopySourcePreserved(t, f)
}
func principalCopyVersions(t *testing.T, db *gorm.DB, want uint8) {
	t.Helper()
	var rows []struct{ Version uint8 }
	if err := db.Table("protocol_credentials").Select("principal_lookup_version AS version").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal("credential row count changed")
	}
	for _, row := range rows {
		if row.Version != want {
			t.Fatalf("copied lookup coverage=%d, want%d", row.Version, want)
		}
	}
}
func principalCopyLookup(t *testing.T, db *gorm.DB, cipher *security.CredentialCipher, secret string) {
	t.Helper()
	row, err := (credentialstore.PrincipalIndex{DB: db, Cipher: cipher}).Lookup(context.Background(), 1, secret)
	if err != nil || row.ID != 1 {
		t.Fatal("copied secret lookup did not rebuild", err)
	}
	if row.RetiredPrincipalHash != "" || row.PrincipalKey == secret || row.SecretPrincipalDigest != entitlements.RetiredPrincipalHash(1, secret) {
		t.Fatal("lookup bypassed the rebuilt secret digest")
	}
	var empty model.ProtocolCredential
	if err := db.First(&empty, 2).Error; err != nil {
		t.Fatal(err)
	}
	if empty.Secret != "" || empty.RetiredPrincipalHash != "" || empty.SecretPrincipalDigest != "" || empty.PrincipalLookupVersion != 1 {
		t.Fatal("legitimate empty secret coverage was changed")
	}
}
