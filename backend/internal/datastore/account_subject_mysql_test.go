package datastore_test

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestMySQLAccountSubjectCanonicalForeignKeysResume(t *testing.T) {
	db, root := hourlyMySQLFixture(t)
	var schema string
	if err := root.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated schema", schema)
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	assertCanonical := func() {
		t.Helper()
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND table_name='wallet_accounts' AND column_name='user_id' AND referenced_table_name='account_subjects'").Scan(&count).Error; err != nil || count != 1 {
			t.Fatal("duplicate or missing historical FK", count, err)
		}
		if !db.Migrator().HasConstraint(&model.WalletAccount{}, "User") {
			t.Fatal("canonical FK missing")
		}
	}
	assertCanonical()
	for _, stage := range []string{"temporary_only", "temporary_and_canonical"} {
		t.Run(stage, func(t *testing.T) {
			conn, err := root.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(t.Context(), "SET SESSION foreign_key_checks=0"); err != nil {
				t.Fatal(err)
			}
			defer conn.ExecContext(context.Background(), "SET SESSION foreign_key_checks=1")
			ddl := "ALTER TABLE wallet_accounts ADD CONSTRAINT as_wallet_accounts_user_id FOREIGN KEY(user_id) REFERENCES account_subjects(id) ON DELETE RESTRICT, ALGORITHM=INPLACE, LOCK=NONE"
			if stage == "temporary_only" {
				ddl = "ALTER TABLE wallet_accounts DROP FOREIGN KEY fk_wallet_accounts_user, ADD CONSTRAINT as_wallet_accounts_user_id FOREIGN KEY(user_id) REFERENCES account_subjects(id) ON DELETE RESTRICT, ALGORITHM=INPLACE, LOCK=NONE"
			}
			if _, err := conn.ExecContext(t.Context(), ddl); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), "SET SESSION foreign_key_checks=1"); err != nil {
				t.Fatal(err)
			}
			if err := datastore.RunMigrations(db); err != nil {
				t.Fatal("resume interrupted metadata replacement", err)
			}
			assertCanonical()
		})
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("repeat restart", err)
	}
	assertCanonical()
	// An existing table with lost enforcement is not an interrupted empty-table
	// creation. Do not silently attach unchecked history to the new authority.
	if err := db.Exec("ALTER TABLE wallet_accounts DROP FOREIGN KEY fk_wallet_accounts_user").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("missing historical ownership enforcement silently repaired")
	}
	if db.Migrator().HasConstraint(&model.WalletAccount{}, "User") {
		t.Fatal("preflight mutated broken historical ownership")
	}
}
