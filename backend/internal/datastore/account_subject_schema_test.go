package datastore

import (
	"path/filepath"
	"testing"
)

func TestSQLiteAccountSubjectsUpgradeRetainsFinancialForeignKeys(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "subjects.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, statement := range []string{
		"CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at DATETIME)",
		"CREATE TABLE users(id INTEGER PRIMARY KEY AUTOINCREMENT,email TEXT)",
		"CREATE TABLE wallet_accounts(user_id INTEGER NOT NULL PRIMARY KEY,balance_minor INTEGER NOT NULL CHECK(balance_minor>=0),CONSTRAINT owner FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT)",
		"CREATE INDEX wallet_balance ON wallet_accounts(balance_minor)",
		"INSERT INTO users(id,email) VALUES(1,'before@example.test')",
		"INSERT INTO wallet_accounts VALUES(1,7)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := reconcileAccountSubjectSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAccountSubjectSchema(db); err != nil {
		t.Fatal("repeat", err)
	}
	if err := db.Exec("DELETE FROM users WHERE id=1").Error; err != nil {
		t.Fatal("account still restricted", err)
	}
	var balance int64
	if err := db.Raw("SELECT balance_minor FROM wallet_accounts WHERE user_id=1").Scan(&balance).Error; err != nil || balance != 7 {
		t.Fatal("history lost", balance, err)
	}
	if err := db.Exec("DELETE FROM account_subjects WHERE id=1").Error; err == nil {
		t.Fatal("financial subject not protected")
	}
	if err := db.Exec("INSERT INTO wallet_accounts VALUES(999,0)").Error; err == nil {
		t.Fatal("orphan financial account accepted")
	}
	if !db.Migrator().HasIndex("wallet_accounts", "wallet_balance") {
		t.Fatal("financial index lost")
	}
	if err := db.Exec("UPDATE wallet_accounts SET balance_minor=-1").Error; err == nil {
		t.Fatal("financial check lost")
	}
	var enabled int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil || enabled != 1 {
		t.Fatal("constraints not restored", enabled, err)
	}
}
