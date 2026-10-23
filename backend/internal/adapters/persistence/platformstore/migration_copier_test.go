package platformstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Mirror the composition root's schema-only boundary without importing
// application (which would create an adapter -> application -> adapter cycle).
func prepareMigrationCopyTestSchema(db *gorm.DB) error {
	for _, step := range []func(*gorm.DB) error{
		datastore.RunMigrations,
		datastore.ReconcileCommerceSchema,
		datastore.ReconcileSubscriptionAccessSchema,
		datastore.ReconcileZeroEventSchema,
		datastore.ReconcileTrafficReadSchema,
		datastore.ReconcileOperationsSchema,
		datastore.ReconcileFairUseTelemetrySchema,
	} {
		if err := step(db); err != nil {
			return err
		}
	}
	tables, err := datastore.MigrationTables(db)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("test prepared database is missing %s", table)
		}
	}
	return nil
}

type migrationCopyFixture struct {
	source *gorm.DB
	target platform.MigrationTarget
	task   model.Task
	claim  jobs.Claim
}

func migrationCopyDatabase(t testing.TB, mysql bool) (*gorm.DB, platform.MigrationTarget) {
	t.Helper()
	target := platform.MigrationTarget{TargetDriver: "sqlite", TargetDataSource: filepath.Join(t.TempDir(), "migration.db")}
	if mysql {
		dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("ZBOARD_TEST_MYSQL_DSN is not configured; real MySQL not verified")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		if err != nil {
			t.Fatal("invalid isolated MySQL test configuration")
		}
		config.DBName = ""
		admin, err := sql.Open("mysql", config.FormatDSN())
		if err != nil {
			t.Fatal("cannot open isolated MySQL test server")
		}
		t.Cleanup(func() { _ = admin.Close() })
		name := "zboard_copy_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`") })
		config.DBName, config.ParseTime, config.Loc = name, true, time.UTC
		target = platform.MigrationTarget{TargetDriver: "mysql", TargetDataSource: config.FormatDSN()}
	}
	db, err := OpenMigrationTarget(context.Background(), target.TargetDriver, target.TargetDataSource)
	if err != nil {
		t.Fatal("open isolated database failed")
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	return db, target
}
func migrationCopyClaim(t testing.TB, source *gorm.DB) (model.Task, jobs.Claim) {
	t.Helper()
	tables, err := datastore.MigrationTables(source)
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{IdempotencyKey: "database_migration:" + uuid.NewString(), Type: "database_migration", Scope: "{}", Content: platform.EncodeMigrationSecret(""), Status: 1, Total: int64(len(tables)), MaxAttempts: 1}
	if err := source.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&model.TaskItem{TaskID: task.ID, TargetType: "database", TargetID: "test", Status: 1, Payload: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	store := jobstore.New(source)
	_, err = store.Submit(context.Background(), jobs.Submission{Owner: "system", Key: fmt.Sprintf("database_migration:%d", task.ID), Handler: "database_migration", Resource: jobs.MaintenanceResource, Payload: "{}", Timeout: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.Claim(context.Background(), "migration-copy-test", []string{"database_migration"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return task, claim
}
func newMigrationCopyFixture(t testing.TB, sourceMySQL, targetMySQL bool) migrationCopyFixture {
	t.Helper()
	source, _ := migrationCopyDatabase(t, sourceMySQL)
	if err := prepareMigrationCopyTestSchema(source); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&model.User{Email: "copy-source@example.test", Password: "source-evidence", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	targetDB, target := migrationCopyDatabase(t, targetMySQL)
	pool, _ := targetDB.DB()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	task, claim := migrationCopyClaim(t, source)
	return migrationCopyFixture{source, target, task, claim}
}
func (f migrationCopyFixture) destination(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := OpenMigrationTarget(context.Background(), f.target.TargetDriver, f.target.TargetDataSource)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	return db
}
func (f migrationCopyFixture) copier(fault bool) MigrationCopier {
	return MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
		if err := prepareMigrationCopyTestSchema(db); err != nil {
			return err
		}
		if fault {
			return db.Callback().Create().Before("gorm:create").Register("test:copy-database-failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "users" {
					tx.AddError(tx.Exec("INSERT INTO zboard_test_missing_copy_table(id) VALUES(1)").Error)
				}
			})
		}
		return nil
	}}
}
func assertMigrationCopySourcePreserved(t testing.TB, f migrationCopyFixture) {
	t.Helper()
	var users []model.User
	if err := f.source.Find(&users).Error; err != nil || len(users) != 1 || users[0].Email != "copy-source@example.test" || users[0].Password != "source-evidence" {
		t.Fatal("source business data changed", len(users), err)
	}
}
func failMigrationCopy(t testing.TB, f migrationCopyFixture) migrationPreparation {
	t.Helper()
	copier := f.copier(true)
	err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID)
	if err == nil || !strings.Contains(err.Error(), "write target table users") {
		t.Fatalf("expected actual SQL failure during copy: %v", err)
	}
	destination := f.destination(t)
	receipt, err := loadMigrationPreparation(destination, false)
	if err != nil {
		t.Fatal("prepared evidence lost after copy rollback", err)
	}
	if err := verifyPreparationManifest(destination, receipt, false); err != nil {
		t.Fatal("copy rollback changed owned seed", err)
	}
	var users int64
	if err := destination.Model(&model.User{}).Count(&users).Error; err != nil || users != 0 {
		t.Fatal("failed copy committed user", users, err)
	}
	assertMigrationCopySourcePreserved(t, f)
	return receipt
}

func TestMigrationCopierRecoversOwnedPreparedTargetAfterSQLFailure(t *testing.T) {
	assertMigrationCopyRetry(t, newMigrationCopyFixture(t, false, false), false)
}
func TestMySQLMigrationCopierRecoversOwnedPreparedTargetAfterSQLFailure(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) {
			assertMigrationCopyRetry(t, newMigrationCopyFixture(t, !targetMySQL, targetMySQL), false)
		})
	}
}
func TestMigrationCopierNewTaskResumesSameSourcePreparation(t *testing.T) {
	assertMigrationCopyRetry(t, newMigrationCopyFixture(t, false, false), true)
}
func TestMySQLMigrationCopierNewTaskResumesSameSourcePreparation(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) {
			assertMigrationCopyRetry(t, newMigrationCopyFixture(t, !targetMySQL, targetMySQL), true)
		})
	}
}
func assertMigrationCopyRetry(t *testing.T, f migrationCopyFixture, newTask bool) {
	receipt := failMigrationCopy(t, f)
	if newTask {
		if err := jobstore.New(f.source).Finish(context.Background(), f.claim, jobs.Failed); err != nil {
			t.Fatal(err)
		}
		if err := f.source.Model(&f.task).Update("status", 3).Error; err != nil {
			t.Fatal(err)
		}
		f.task, f.claim = migrationCopyClaim(t, f.source)
	}
	if err := (MigrationTargets{DB: f.source}).Check(context.Background(), f.target); err != nil {
		t.Fatal("owned prepared target blocked admission", err)
	}
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(*gorm.DB) error { t.Fatal("retry ran unowned schema initialization again"); return nil }}
	if err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err != nil {
		t.Fatal("retry failed", err)
	}
	destination := f.destination(t)
	var after migrationPreparation
	if err := destination.First(&after, 1).Error; err != nil || after.State != "completed" || after.Token != receipt.Token {
		t.Fatal("completion receipt not committed", after.State, err)
	}
	var user model.User
	if err := destination.First(&user, 1).Error; err != nil || user.Email != "copy-source@example.test" || user.Password != "source-evidence" {
		t.Fatal("copied source data differs", user.Email, err)
	}
	var task model.Task
	if err := destination.First(&task, f.task.ID).Error; err != nil || task.Status != 2 || task.Current != task.Total {
		t.Fatal("copied task not finalized", task.Status, err)
	}
	var run jobstore.Record
	if err := destination.First(&run, "id = ?", f.claim.Run.ID).Error; err != nil || run.State != string(jobs.Succeeded) {
		t.Fatal("copied run not finalized", run.State, err)
	}
	if err := (MigrationTargets{DB: f.source}).Check(context.Background(), f.target); !errors.Is(err, platform.ErrMigrationTargetOccupied) {
		t.Fatal("completed target was offered for destructive replay", err)
	}
	assertMigrationCopySourcePreserved(t, f)
}

func TestMigrationCopierRejectsUnownedAndChangedTargets(t *testing.T) {
	for _, change := range []string{"unowned_seed", "unknown_data", "business_insert", "seed_change", "schema_change", "wrong_source", "wrong_target", "source_task_changed", "preparing", "missing_receipt", "between_check_and_copy"} {
		t.Run(change, func(t *testing.T) { assertMigrationCopyRejection(t, newMigrationCopyFixture(t, false, false), change) })
	}
}
func TestMySQLMigrationCopierRejectsUnownedAndChangedTargets(t *testing.T) {
	for _, change := range []string{"unowned_seed", "unknown_data", "sqlite_prefix_data", "business_insert", "seed_change", "schema_change", "wrong_source", "wrong_target", "source_task_changed", "preparing", "missing_receipt", "between_check_and_copy"} {
		t.Run(change, func(t *testing.T) { assertMigrationCopyRejection(t, newMigrationCopyFixture(t, false, true), change) })
	}
}
func assertMigrationCopyRejection(t *testing.T, f migrationCopyFixture, change string) {
	destination := f.destination(t)
	var receipt migrationPreparation
	switch change {
	case "unowned_seed":
		if err := prepareMigrationCopyTestSchema(destination); err != nil {
			t.Fatal(err)
		}
	case "unknown_data":
		if err := destination.Exec("CREATE TABLE custom_real_data(id int PRIMARY KEY, value varchar(32))").Error; err != nil {
			t.Fatal(err)
		}
		if err := destination.Exec("INSERT INTO custom_real_data(id,value)VALUES(1,'preserve')").Error; err != nil {
			t.Fatal(err)
		}
	case "sqlite_prefix_data":
		if err := destination.Exec("CREATE TABLE sqlite_real_data(id int PRIMARY KEY, value varchar(32))").Error; err != nil {
			t.Fatal(err)
		}
		if err := destination.Exec("INSERT INTO sqlite_real_data(id,value)VALUES(1,'preserve')").Error; err != nil {
			t.Fatal(err)
		}
	default:
		receipt = failMigrationCopy(t, f)
		switch change {
		case "business_insert", "between_check_and_copy":
			if err := destination.Create(&model.User{Email: "real-target@example.test", Password: "preserve", Status: "active"}).Error; err != nil {
				t.Fatal(err)
			}
		case "seed_change":
			if err := destination.Model(&model.SystemConfig{}).Where("config_key = ?", "subscription_renewal_grace_days").Update("value", "14").Error; err != nil {
				t.Fatal(err)
			}
		case "schema_change":
			if err := destination.Exec("CREATE TABLE extra_empty_table(id int PRIMARY KEY)").Error; err != nil {
				t.Fatal(err)
			}
		case "wrong_source":
			other, _ := migrationCopyDatabase(t, false)
			if err := prepareMigrationCopyTestSchema(other); err != nil {
				t.Fatal(err)
			}
			f.source = other
		case "wrong_target":
			other, target := migrationCopyDatabase(t, f.target.TargetDriver == "mysql")
			if err := other.Migrator().CreateTable(&migrationPreparation{}); err != nil {
				t.Fatal(err)
			}
			if err := other.Create(&receipt).Error; err != nil {
				t.Fatal(err)
			}
			f.target, destination = target, other
		case "source_task_changed":
			if err := f.source.Model(&f.task).Update("idempotency_key", "database_migration:"+uuid.NewString()).Error; err != nil {
				t.Fatal(err)
			}
		case "preparing":
			if err := destination.Model(&migrationPreparation{}).Where("id = 1").Update("state", "preparing").Error; err != nil {
				t.Fatal(err)
			}
		case "missing_receipt":
			if err := destination.Migrator().DropTable(&migrationPreparation{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if change == "between_check_and_copy" {
		err := copyPreparedMigrationSnapshot(context.Background(), f.source, destination, f.task.ID, f.claim.Run.ID, receipt)
		if !errors.Is(err, platform.ErrMigrationTargetOccupied) {
			t.Fatal("transaction guard failed before clear", err)
		}
	} else if err := (MigrationTargets{DB: f.source}).Check(context.Background(), f.target); !errors.Is(err, platform.ErrMigrationTargetOccupied) {
		t.Fatal("unsafe target passed admission", err)
	}
	if change == "business_insert" || change == "between_check_and_copy" {
		var user model.User
		if err := destination.Where("email = ?", "real-target@example.test").First(&user).Error; err != nil || user.Password != "preserve" {
			t.Fatal("real target data was overwritten", err)
		}
	}
	if change == "unknown_data" {
		var value string
		if err := destination.Raw("SELECT value FROM custom_real_data WHERE id=1").Scan(&value).Error; err != nil || value != "preserve" {
			t.Fatal("unknown target data was overwritten", err)
		}
	}
	if change == "sqlite_prefix_data" {
		var value string
		if err := destination.Raw("SELECT value FROM sqlite_real_data WHERE id=1").Scan(&value).Error; err != nil || value != "preserve" {
			t.Fatal("MySQL SQLite-like table was treated as internal", err)
		}
	}
	if change != "wrong_source" {
		assertMigrationCopySourcePreserved(t, f)
	}
}

func TestMigrationCopierInterruptedPreparationStaysUncertain(t *testing.T) {
	assertMigrationPreparationInterrupted(t, newMigrationCopyFixture(t, false, false))
}
func TestMySQLMigrationCopierInterruptedPreparationStaysUncertain(t *testing.T) {
	assertMigrationPreparationInterrupted(t, newMigrationCopyFixture(t, false, true))
}
func assertMigrationPreparationInterrupted(t *testing.T, f migrationCopyFixture) {
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
		if err := prepareMigrationCopyTestSchema(db); err != nil {
			return err
		}
		return errors.New("simulated process loss after schema DDL")
	}}
	if err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err == nil {
		t.Fatal("uncertain initialization acknowledged")
	}
	destination := f.destination(t)
	var receipt migrationPreparation
	if err := destination.First(&receipt, 1).Error; err != nil || receipt.State != "preparing" || receipt.SeedFingerprint != "" {
		t.Fatal("uncertain preparation evidence missing", receipt.State, err)
	}
	if err := f.copier(false).Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); !errors.Is(err, platform.ErrMigrationTargetOccupied) {
		t.Fatal("unknown DDL interval adopted without proof", err)
	}
	var seed model.SystemConfig
	if err := destination.Where("config_key = ?", "subscription_renewal_grace_days").First(&seed).Error; err != nil || seed.Value != "7" {
		t.Fatal("uncertain target seed was cleared", err)
	}
	assertMigrationCopySourcePreserved(t, f)
}

func TestMigrationCopierCompletionFailureRollsBackCopiedRowsAndReceipt(t *testing.T) {
	assertMigrationCompletionRollback(t, newMigrationCopyFixture(t, false, false))
}
func TestMySQLMigrationCopierCompletionFailureRollsBackCopiedRowsAndReceipt(t *testing.T) {
	assertMigrationCompletionRollback(t, newMigrationCopyFixture(t, false, true))
}
func assertMigrationCompletionRollback(t *testing.T, f migrationCopyFixture) {
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
		if err := prepareMigrationCopyTestSchema(db); err != nil {
			return err
		}
		return db.Callback().Update().Before("gorm:update").Register("test:copy-completion-failure", func(tx *gorm.DB) {
			values, ok := tx.Statement.Dest.(map[string]any)
			if tx.Statement.Table == migrationPreparationTable && ok && values["state"] == "completed" {
				tx.AddError(tx.Exec("INSERT INTO zboard_test_missing_completion_table(id) VALUES(1)").Error)
			}
		})
	}}
	if err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err == nil {
		t.Fatal("failed completion was acknowledged")
	}
	destination := f.destination(t)
	receipt, err := loadMigrationPreparation(destination, false)
	if err != nil {
		t.Fatal("completion rollback lost prepared evidence", err)
	}
	if err := verifyPreparationManifest(destination, receipt, false); err != nil {
		t.Fatal("completion failure left copied rows behind", err)
	}
	if err := f.copier(false).Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err != nil {
		t.Fatal("completion failure could not retry", err)
	}
	assertMigrationCopySourcePreserved(t, f)
}

func TestMigrationCopierDoesNotClaimBusinessWritesAsInitialization(t *testing.T) {
	for _, change := range []string{"custom_config", "changed_menu", "changed_default", "changed_budget", "changed_ledger"} {
		t.Run(change, func(t *testing.T) {
			assertMigrationDDLWindowProtection(t, newMigrationCopyFixture(t, false, false), change)
		})
	}
}
func TestMySQLMigrationCopierDoesNotClaimBusinessWritesAsInitialization(t *testing.T) {
	for _, change := range []string{"custom_config", "changed_menu", "changed_default", "changed_budget", "changed_email", "changed_subscription"} {
		t.Run(change, func(t *testing.T) {
			assertMigrationDDLWindowProtection(t, newMigrationCopyFixture(t, false, true), change)
		})
	}
}
func assertMigrationDDLWindowProtection(t *testing.T, f migrationCopyFixture, change string) {
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
		if err := prepareMigrationCopyTestSchema(db); err != nil {
			return err
		}
		switch change {
		case "custom_config":
			return db.Create(&model.SystemConfig{ConfigKey: "operator_value", Name: "Preserve", Value: "business evidence", ValueType: "string", Revision: 1}).Error
		case "changed_menu":
			return db.Model(&model.MenuNode{}).Where("id = ?", "overview").Update("label", "operator menu").Error
		case "changed_default":
			return db.Model(&model.SystemConfig{}).Where("config_key = ?", "subscription_renewal_grace_days").Update("value", "14").Error
		case "changed_ledger":
			return db.Table("traffic_ledger_revision").Where("id = 1").Update("revision", 1).Error
		case "changed_email":
			return db.Table("email_templates").Where("slug = ?", "registration-welcome").Update("subject_template", "operator evidence").Error
		case "changed_subscription":
			return db.Table("subscription_templates").Where("slug = ?", "clash").Update("description", "operator evidence").Error
		case "changed_budget":
			return db.Model(&jobstore.Budget{}).Where("id = 1").Update("capacity", 10).Error
		}
		return nil
	}}
	if err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); !errors.Is(err, platform.ErrMigrationTargetOccupied) {
		t.Fatal("business writes inside DDL window were adopted as owned seeds", err)
	}
	destination := f.destination(t)
	var receipt migrationPreparation
	if err := destination.First(&receipt, 1).Error; err != nil || receipt.State != "preparing" || receipt.SeedFingerprint != "" {
		t.Fatal("unproven initialization received a reusable receipt", receipt.State, err)
	}
	switch change {
	case "changed_ledger":
		var revision int
		if err := destination.Table("traffic_ledger_revision").Select("revision").Where("id = 1").Scan(&revision).Error; err != nil || revision != 1 {
			t.Fatal("operator ledger evidence was overwritten", err)
		}
	case "changed_email", "changed_subscription":
		table, column, slug := "email_templates", "subject_template", "registration-welcome"
		if change == "changed_subscription" {
			table, column, slug = "subscription_templates", "description", "clash"
		}
		var value string
		if err := destination.Table(table).Select(column).Where("slug = ?", slug).Scan(&value).Error; err != nil || value != "operator evidence" {
			t.Fatal("operator template evidence was overwritten", err)
		}
	case "custom_config":
		var value model.SystemConfig
		if err := destination.Where("config_key = ?", "operator_value").First(&value).Error; err != nil || value.Value != "business evidence" {
			t.Fatal("custom configuration was overwritten", err)
		}
	case "changed_menu":
		var node model.MenuNode
		if err := destination.First(&node, "id = ?", "overview").Error; err != nil || node.Label != "operator menu" {
			t.Fatal("operator menu was overwritten", err)
		}
	case "changed_default":
		var value model.SystemConfig
		if err := destination.Where("config_key = ?", "subscription_renewal_grace_days").First(&value).Error; err != nil || value.Value != "14" {
			t.Fatal("operator policy was overwritten", err)
		}
	case "changed_budget":
		var budget jobstore.Budget
		if err := destination.First(&budget, 1).Error; err != nil || budget.Capacity != 10 {
			t.Fatal("operator budget was overwritten", err)
		}
	}
	assertMigrationCopySourcePreserved(t, f)
}

func TestMySQLMigrationCopierOwnedBaselineProofCannotBeForgedByStoredRows(t *testing.T) {
	for _, change := range []string{"invalid_token", "wrong_source", "wrong_target", "unknown_table", "ordinary_without_proof", "expired_claim"} {
		t.Run(change, func(t *testing.T) {
			f := newMigrationCopyFixture(t, false, true)
			copier := MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
				switch change {
				case "invalid_token":
					if err := db.Model(&migrationPreparation{}).Where("id = 1").Update("token", strings.Repeat("f", 64)).Error; err != nil {
						return err
					}
				case "wrong_source":
					if err := db.Model(&migrationPreparation{}).Where("id = 1").Update("source_identity", strings.Repeat("f", 64)).Error; err != nil {
						return err
					}
				case "wrong_target":
					if err := db.Model(&migrationPreparation{}).Where("id = 1").Update("target_identity", strings.Repeat("f", 64)).Error; err != nil {
						return err
					}
				case "unknown_table":
					if err := db.Exec("CREATE TABLE operator_real_data(id int PRIMARY KEY, value varchar(32))").Error; err != nil {
						return err
					}
					if err := db.Exec("INSERT INTO operator_real_data(id,value)VALUES(1,'preserve')").Error; err != nil {
						return err
					}
				case "ordinary_without_proof":
					return datastore.RunMigrations(db.WithContext(context.Background()))
				case "expired_claim":
					if err := f.source.Model(&jobstore.Record{}).Where("id = ?", f.claim.Run.ID).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
						return err
					}
				}
				return prepareMigrationCopyTestSchema(db)
			}}
			if err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err == nil {
				t.Fatal("stored metadata bypassed fresh baseline protection")
			}
			destination := f.destination(t)
			var baseline int64
			if err := destination.Table("schema_migrations").Where("version = ?", "0001_init.up.sql").Count(&baseline).Error; err != nil || baseline != 0 {
				t.Fatal("failed proof fabricated a baseline migration", baseline, err)
			}
			if destination.Migrator().HasTable(&model.User{}) {
				t.Fatal("application schema was created despite failed purpose proof")
			}
			if change == "unknown_table" {
				var value string
				if err := destination.Raw("SELECT value FROM operator_real_data WHERE id=1").Scan(&value).Error; err != nil || value != "preserve" {
					t.Fatal("unknown target data was overwritten", err)
				}
			}
			assertMigrationCopySourcePreserved(t, f)
		})
	}
}

// Public admission already rejects same-driver migrations. Direct adapter
// callers must also fail before target initialization, rather than hang while
// the source's server-wide read lock blocks writes to another database.
func TestMySQLMigrationCopierRejectsSameServerBeforeAnyTargetWrites(t *testing.T) {
	f := newMigrationCopyFixture(t, true, true)
	destination := f.destination(t)
	sourceID, err := migrationDatabaseIdentity(f.source)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := migrationDatabaseIdentity(destination)
	if err != nil || sourceID == targetID {
		t.Fatal("actual MySQL database identities were not distinguished", err)
	}
	assertEmpty := func() {
		t.Helper()
		tables, err := destination.Migrator().GetTables()
		if err != nil || len(tables) != 0 {
			t.Fatal("same-server rejection modified target schema", tables, err)
		}
	}
	assertEmpty()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := (MigrationTargets{DB: f.source}).Check(ctx, f.target); !errors.Is(err, platform.ErrMigrationInvalid) || !strings.Contains(err.Error(), "FLUSH TABLES WITH READ LOCK") {
		t.Fatal("same actual MySQL server passed preflight", err)
	}
	assertEmpty()
	copier := MigrationCopier{DB: f.source, PrepareSchema: func(*gorm.DB) error { t.Fatal("unsupported topology initialized target"); return nil }}
	if err := copier.Copy(ctx, f.target, f.task.ID, f.claim.Run.ID); !errors.Is(err, platform.ErrMigrationInvalid) || !strings.Contains(err.Error(), "FLUSH TABLES WITH READ LOCK") {
		t.Fatal("same actual MySQL server reached copy", err)
	}
	assertEmpty()
	assertMigrationCopySourcePreserved(t, f)
}
