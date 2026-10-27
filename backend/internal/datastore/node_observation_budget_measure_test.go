package datastore

import (
	"path/filepath"
	"testing"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteObservationBudgetMeasurementNeverInventsZeroForInvalidParts(t *testing.T) {
	for _, variant := range []string{"empty", "valid", "null", "empty-part", "oversized-part", "multibyte-text", "missing-table"} {
		t.Run(variant, func(t *testing.T) {
			db, e := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "budget-measure.db")), &gorm.Config{})
			if e != nil {
				t.Fatal(e)
			}
			pool, _ := db.DB()
			defer pool.Close()
			if variant != "missing-table" {
				if e := db.Exec("CREATE TABLE node_observation_parts (data BLOB NULL)").Error; e != nil {
					t.Fatal(e)
				}
			}
			switch variant {
			case "valid":
				// Bind real BLOB values through database/sql. GORM's Raw expression
				// expands slices directly following '(' into multiple placeholders.
				if _, e := pool.ExecContext(t.Context(), "INSERT INTO node_observation_parts(data) VALUES(?),(?)", []byte("abc"), []byte("12345")); e != nil {
					t.Fatal(e)
				}
			case "null":
				if e := db.Exec("INSERT INTO node_observation_parts(data) VALUES(NULL)").Error; e != nil {
					t.Fatal(e)
				}
			case "empty-part":
				if _, e := pool.ExecContext(t.Context(), "INSERT INTO node_observation_parts(data) VALUES(?)", []byte{}); e != nil {
					t.Fatal(e)
				}
			case "oversized-part":
				if e := db.Exec("INSERT INTO node_observation_parts(data) VALUES(zeroblob(61441))").Error; e != nil {
					t.Fatal(e)
				}
			case "multibyte-text":
				if e := db.Exec("INSERT INTO node_observation_parts(data) VALUES(?)", "界").Error; e != nil {
					t.Fatal(e)
				}
			}
			bytes, e := measureObservationRetainedBytes(db)
			switch variant {
			case "empty":
				if e != nil || bytes != 0 {
					t.Fatal(bytes, e)
				}
			case "valid":
				if e != nil || bytes != 8 {
					t.Fatal(bytes, e)
				}
			case "multibyte-text":
				if e != nil || bytes != 3 {
					t.Fatal("counted Unicode characters instead of bytes", bytes, e)
				}
			default:
				if e == nil {
					t.Fatal("invalid/unknown retained material became a known budget", variant, bytes)
				}
			}
		})
	}
}
