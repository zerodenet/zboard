package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
)

func TestSeedValidationRejectsWrongTimeWindowAndCollapsedDistribution(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Exec("CREATE TABLE traffic_records(id INTEGER PRIMARY KEY, record_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	start := now.Add(-6 * 24 * time.Hour)
	if err := db.Exec("INSERT INTO traffic_records VALUES (1,?),(2,?)", start, now.Add(-3*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := verifySeedHistory(db, now, 2); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{start.Add(-time.Hour), start} {
		if err := db.Exec("UPDATE traffic_records SET record_at=? WHERE id=2", at).Error; err != nil {
			t.Fatal(err)
		}
		if err := verifySeedHistory(db, now, 2); err == nil {
			t.Fatalf("accepted invalid history distribution ending at %s", at)
		}
	}
}
