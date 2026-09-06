package datastore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSQLiteReadViewKeepsSnapshotWithoutBlockingCommittedWrites(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "read view#database.db"))
	if err != nil {
		t.Fatal(err)
	}
	writer, _ := db.DB()
	defer writer.Close()
	if err := db.Exec("CREATE TABLE balances (amount INTEGER NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO balances VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
	view, closeView, err := OpenReadView(db)
	if err != nil {
		t.Fatal(err)
	}
	defer closeView()
	reader, _ := view.DB()
	if reader == writer || reader.Stats().MaxOpenConnections != 2 || writer.Stats().MaxOpenConnections != 1 {
		t.Fatal("reporting and accounting pools are not isolated and bounded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = view.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before, during int
		if err := tx.Raw("SELECT amount FROM balances").Scan(&before).Error; err != nil {
			return err
		}
		// This commits while the reporting transaction retains the old snapshot.
		// An immediate/read-write reporting transaction would block this write.
		if err := db.WithContext(ctx).Exec("UPDATE balances SET amount = 2").Error; err != nil {
			return err
		}
		if err := tx.Raw("SELECT amount FROM balances").Scan(&during).Error; err != nil {
			return err
		}
		if before != 1 || during != 1 {
			t.Errorf("inconsistent reporting snapshot: before=%d during=%d", before, during)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err := view.Raw("SELECT amount FROM balances").Scan(&after).Error; err != nil || after != 2 {
		t.Fatalf("new read did not see committed state: value=%d error=%v", after, err)
	}
	if err := view.Exec("UPDATE balances SET amount = 3").Error; err == nil {
		t.Fatal("reporting connection allowed a write")
	}
	if err := closeView(); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE balances SET amount = 4").Error; err != nil {
		t.Fatalf("closing reporting also closed accounting: %v", err)
	}
}

func TestSQLiteMemoryReadViewSharesExistingDatabase(t *testing.T) {
	db, err := OpenWithDriver(DriverSQLite, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	view, closeView, err := OpenReadView(db)
	if err != nil || view != db {
		t.Fatalf("memory database was replaced: same=%t error=%v", view == db, err)
	}
	if err := closeView(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(); err != nil {
		t.Fatalf("fallback close must not close the owner pool: %v", err)
	}
}
