package handler

import (
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestRetiredPrincipalRecoverySchemaPreservesEvidence(t *testing.T) {
	assertRetiredPrincipalSchema(t, newOrderFixture(t))
}
func TestMySQLRetiredPrincipalRecoverySchemaPreservesEvidence(t *testing.T) {
	assertRetiredPrincipalSchema(t, newMySQLOrderFixture(t))
}
func assertRetiredPrincipalSchema(t *testing.T, f orderFixture) {
	attachOrderPublishEndpoint(t, f)
	order := f.paid(t, f.create(t, 0).ID)
	var credential model.ProtocolCredential
	if err := f.h.db.Where("subscription_id = ?", order.SubscriptionID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := f.h.db.Model(&credential).Update("retired_principal_hash", digest).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(f.h.db); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.db.First(&credential, credential.ID).Error; err != nil || credential.RetiredPrincipalHash != digest {
		t.Fatalf("repeated schema rewrote hash %+v %v", credential, err)
	}
	if err := f.h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0044_retired_principal_hash.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Migrator().DropIndex(&model.ProtocolCredential{}, "idx_credential_retired_principal"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err != nil {
		t.Fatal("interrupted column/index DDL", err)
	}
	if err := f.h.db.First(&credential, credential.ID).Error; err != nil || credential.RetiredPrincipalHash != digest {
		t.Fatal("interrupted index recovery changed evidence", credential, err)
	}
	if err := f.h.db.Migrator().DropIndex(&model.ProtocolCredential{}, "idx_credential_retired_principal"); err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Exec("ALTER TABLE protocol_credentials DROP COLUMN retired_principal_hash").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(f.h.db); err == nil {
		t.Fatal("lost matching evidence replaced by empty hashes")
	}
	if f.h.db.Migrator().HasColumn(&model.ProtocolCredential{}, "retired_principal_hash") {
		t.Fatal("lost hash column silently recreated")
	}
}
