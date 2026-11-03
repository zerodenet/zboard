package datastore

import (
	"path/filepath"
	"testing"
)

func TestUserAdminNoteUpgradeResumesWithoutLosingNotes(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, sql := range []string{
		"CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at DATETIME)",
		"CREATE TABLE users(id INTEGER PRIMARY KEY,email TEXT)",
		"INSERT INTO users(id,email) VALUES(1,'existing@example.test')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := reconcileUserAdminNoteSchema(db); err != nil {
		t.Fatal(err)
	}
	var note string
	if err := db.Raw("SELECT admin_note FROM users WHERE id=1").Scan(&note).Error; err != nil || note != "" {
		t.Fatal(note, err)
	}
	if err := db.Exec("UPDATE users SET admin_note='运营备注' WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", userAdminNoteVersion).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reconcileUserAdminNoteSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Raw("SELECT admin_note FROM users WHERE id=1").Scan(&note).Error; err != nil || note != "运营备注" {
		t.Fatal(note, err)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version=?", userAdminNoteVersion).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
