package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestDatabaseSnapshotUsesSingleConnectionAndCommitsCopiedTask(t *testing.T) {
	for _, driver := range []string{datastore.DriverSQLite, datastore.DriverMySQL} {
		t.Run(driver, func(t *testing.T) {
			source := schemaFixture(t, driver)
			targetDriver := datastore.DriverMySQL
			if driver == datastore.DriverMySQL {
				targetDriver = datastore.DriverSQLite
			}
			target := schemaFixture(t, targetDriver)
			for _, db := range []*gorm.DB{source, target} {
				pool, _ := db.DB()
				pool.SetMaxOpenConns(1)
				pool.SetMaxIdleConns(1)
				if err := PrepareDatabaseSchema(db); err != nil {
					t.Fatal(err)
				}
			}
			task := model.Task{Type: "database_migration", Scope: "{}", Content: platform.EncodeMigrationSecret("zboard:v1:encrypted-destination"), Status: 1, Total: 17, IdempotencyKey: "copy-test"}
			if err := source.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			if driver == datastore.DriverSQLite {
				for i, content := range []string{"zboard:v1:legacy-cipher", ""} {
					legacy := model.Task{Type: "database_migration", Scope: "{}", Content: content, Status: 3, IdempotencyKey: []string{"legacy-secret", "legacy-cleared"}[i]}
					if err := source.Create(&legacy).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			item := model.TaskItem{TaskID: task.ID, TargetType: "database", TargetID: targetDriver, Status: 1, Payload: "{}"}
			if err := source.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			if err := source.Exec("UPDATE job_execution_budget SET capacity = 2 WHERE id = 1").Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := CopyDatabaseSnapshot(ctx, source, target, task.ID+100); err == nil || !strings.Contains(err.Error(), "is missing") {
				t.Fatal("missing completion task did not abort copy", err)
			}
			var capacity int
			if err := target.Raw("SELECT capacity FROM job_execution_budget WHERE id = 1").Scan(&capacity).Error; err != nil || capacity != 4 {
				t.Fatal("failed copy lost target defaults", capacity, err)
			}
			var count int64
			if err := target.Model(&model.Task{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("failed copy committed task", count, err)
			}
			if err := CopyDatabaseSnapshot(ctx, source, target, task.ID); err != nil {
				t.Fatal(err)
			}
			var copied model.Task
			if err := target.First(&copied, task.ID).Error; err != nil || copied.Status != 2 || copied.Current != 17 || copied.Content != "{}" {
				t.Fatal("copied task not finalized", copied, err)
			}
			var original model.Task
			if err := source.First(&original, task.ID).Error; err != nil || original.Status != 1 || !strings.Contains(original.Content, "zboard:v1:encrypted-destination") {
				t.Fatal("source current task changed by copy", err)
			}
			// The MySQL global source read lock must have been released after both calls.
			if err := source.WithContext(ctx).Model(&model.Task{}).Where("id = ?", task.ID).Update("current", 1).Error; err != nil {
				t.Fatal("source remained locked", err)
			}
		})
	}
}

func TestCopyDestinationRestoresConstraintsOnFailureAndPanic(t *testing.T) {
	for _, driver := range []string{datastore.DriverSQLite, datastore.DriverMySQL} {
		t.Run(driver, func(t *testing.T) {
			db := schemaFixture(t, driver)
			pool, _ := db.DB()
			pool.SetMaxOpenConns(1)
			pool.SetMaxIdleConns(1)
			if err := db.Exec("CREATE TABLE migration_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				t.Fatal(err)
			}
			read, set := "SELECT @@SESSION.FOREIGN_KEY_CHECKS", "SET FOREIGN_KEY_CHECKS = "
			if driver == datastore.DriverSQLite {
				read, set = "PRAGMA foreign_keys", "PRAGMA foreign_keys = "
			}
			for _, previous := range []string{"1", "0"} {
				if err := db.Exec(set + previous).Error; err != nil {
					t.Fatal(err)
				}
				failure := errors.New("copy failed")
				for _, panicMode := range []bool{false, true} {
					func() {
						defer func() {
							if v := recover(); v != nil && (!panicMode || v != failure) {
								t.Fatalf("unexpected panic: %v", v)
							}
						}()
						err := datastore.WithCopyDestination(context.Background(), db, func(tx *gorm.DB) error {
							var disabled int
							if err := tx.Raw(read).Scan(&disabled).Error; err != nil || disabled != 0 {
								t.Fatal("constraints not disabled on transaction connection", disabled, err)
							}
							if err := tx.Exec("INSERT INTO migration_probe(id) VALUES (1)").Error; err != nil {
								return err
							}
							if panicMode {
								panic(failure)
							}
							return failure
						})
						if panicMode {
							t.Fatal("panic swallowed")
						}
						if !errors.Is(err, failure) {
							t.Fatal(err)
						}
					}()
					var setting string
					if err := db.Raw(read).Scan(&setting).Error; err != nil || setting != previous {
						t.Fatal("constraint setting leaked", setting, previous, err)
					}
					var count int64
					if err := db.Table("migration_probe").Count(&count).Error; err != nil || count != 0 {
						t.Fatal("failed transaction committed", count, err)
					}
				}
			}
		})
	}
}

func TestCopyDestinationCancellationReleasesConnection(t *testing.T) {
	for _, driver := range []string{datastore.DriverSQLite, datastore.DriverMySQL} {
		t.Run(driver, func(t *testing.T) {
			db := schemaFixture(t, driver)
			pool, _ := db.DB()
			pool.SetMaxOpenConns(1)
			pool.SetMaxIdleConns(1)
			if err := db.Exec("CREATE TABLE migration_cancel_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			err := datastore.WithCopyDestination(ctx, db, func(tx *gorm.DB) error {
				if err := tx.Exec("INSERT INTO migration_cancel_probe(id) VALUES (1)").Error; err != nil {
					return err
				}
				cancel()
				return ctx.Err()
			})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was lost", err)
			}
			check, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			var count int64
			if err := db.WithContext(check).Table("migration_cancel_probe").Count(&count).Error; err != nil || count != 0 {
				t.Fatal("canceled copy leaked connection or rows", count, err)
			}
			read := "SELECT @@SESSION.FOREIGN_KEY_CHECKS"
			if driver == datastore.DriverSQLite {
				read = "PRAGMA foreign_keys"
			}
			var enabled int
			if err := db.WithContext(check).Raw(read).Scan(&enabled).Error; err != nil || enabled != 1 {
				t.Fatal("canceled copy leaked disabled constraints", enabled, err)
			}
		})
	}
}
