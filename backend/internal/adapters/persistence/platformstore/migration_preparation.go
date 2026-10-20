package platformstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const migrationPreparationTable = "zboard_migration_preparation"

// Destination-local recovery evidence, deliberately excluded from application
// transfers. No connection string or credential is stored. Preparing is uncertain:
// DDL and the prepared manifest cannot commit atomically on MySQL.
type migrationPreparation struct {
	ID                uint   `gorm:"primaryKey;autoIncrement:false"`
	Token             string `gorm:"size:64;not null;uniqueIndex"`
	SourceIdentity    string `gorm:"size:64;not null"`
	TargetIdentity    string `gorm:"size:64;not null"`
	SourceTaskID      uint   `gorm:"not null"`
	SourceTaskKey     string `gorm:"size:128;not null"`
	SourceRunID       string `gorm:"size:64;not null"`
	State             string `gorm:"size:24;not null"`
	SchemaFingerprint string `gorm:"size:64;not null"`
	SeedFingerprint   string `gorm:"size:64;not null"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (migrationPreparation) TableName() string { return migrationPreparationTable }

func preparationDigest(value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// Resolve the actual database, rather than hashing a credential-bearing DSN.
// SQLite's device/inode also rejects a replacement file at the same pathname.
func migrationDatabaseIdentity(db *gorm.DB) (string, error) {
	if datastore.IsSQLite(db) {
		var databases []struct{ Name, File string }
		if err := db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
			return "", err
		}
		for _, database := range databases {
			if database.Name != "main" || database.File == "" {
				continue
			}
			filename, err := filepath.EvalSymlinks(database.File)
			if err != nil {
				return "", errors.New("cannot resolve SQLite migration database identity")
			}
			fileID, err := migrationFileIdentity(filename)
			if err != nil {
				return "", errors.New("cannot verify stable SQLite migration file identity on this filesystem and operating system")
			}
			return preparationDigest([]string{"sqlite", fileID}), nil
		}
		return "", errors.New("SQLite migration requires a persistent main database")
	}
	var identity struct{ Server, Database string }
	if err := db.Raw("SELECT @@server_uuid AS `server`, DATABASE() AS `database`").Scan(&identity).Error; err != nil {
		return "", errors.New("cannot verify MySQL server and database identity")
	}
	if identity.Server == "" || identity.Database == "" {
		return "", errors.New("MySQL migration requires a named database identity")
	}
	return preparationDigest([]string{"mysql", identity.Server, identity.Database}), nil
}

// The source snapshot uses MySQL's server-wide FLUSH TABLES WITH READ LOCK.
// Even a different database on that same server cannot accept the destination
// writes. Public admission already limits migrations to different drivers; this
// is defense for direct adapter calls, before any destination DDL or receipt.
func ensureMigrationTopologySupported(source, target *gorm.DB) error {
	if source == nil || datastore.IsSQLite(source) || datastore.IsSQLite(target) {
		return nil
	}
	source = source.WithContext(target.Statement.Context)
	identities := make([]string, 2)
	for i, db := range []*gorm.DB{source, target} {
		if err := db.Raw("SELECT @@server_uuid").Scan(&identities[i]).Error; err != nil || identities[i] == "" {
			return fmt.Errorf("%w: cannot verify actual MySQL server identity", platform.ErrMigrationInvalid)
		}
	}
	if identities[0] == identities[1] {
		return fmt.Errorf("%w: same-server MySQL copy cannot run under the source server-wide FLUSH TABLES WITH READ LOCK; use the supported cross-driver migration path", platform.ErrMigrationInvalid)
	}
	return nil
}

func migrationTaskIdentity(db *gorm.DB, taskID uint, runID string, claimed bool) (model.Task, error) {
	var task model.Task
	if taskID == 0 || runID == "" {
		return task, platform.ErrMigrationInvalid
	}
	if err := db.Where("id = ? AND type = ?", taskID, "database_migration").First(&task).Error; err != nil {
		return task, err
	}
	if !strings.HasPrefix(task.IdempotencyKey, "database_migration:") || len(task.IdempotencyKey) < 40 {
		return task, platform.ErrMigrationInvalid
	}
	var run jobstore.Record
	if err := db.Where("id = ? AND owner = ? AND handler = ? AND resource = ? AND `key` = ?", runID, "system", "database_migration", jobs.MaintenanceResource, fmt.Sprintf("database_migration:%d", taskID)).First(&run).Error; err != nil {
		return task, err
	}
	if claimed && (run.State != string(jobs.Running) || run.ExpiresAt == nil || !run.ExpiresAt.After(time.Now().UTC())) {
		return task, jobs.ErrLeaseLost
	}
	return task, nil
}

func verifyPreparationSource(source *gorm.DB, receipt migrationPreparation) error {
	identity, err := migrationDatabaseIdentity(source)
	if err != nil {
		return err
	}
	if identity != receipt.SourceIdentity {
		return fmt.Errorf("%w: destination preparation belongs to another source database", platform.ErrMigrationTargetOccupied)
	}
	original, err := migrationTaskIdentity(source, receipt.SourceTaskID, receipt.SourceRunID, false)
	if err != nil || original.IdempotencyKey != receipt.SourceTaskKey {
		return fmt.Errorf("%w: original source preparation task cannot be verified", platform.ErrMigrationTargetOccupied)
	}
	return nil
}

func loadMigrationPreparation(db *gorm.DB, lock bool) (migrationPreparation, error) {
	var receipt migrationPreparation
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var receipts []migrationPreparation
	if err := query.Order("id ASC").Limit(2).Find(&receipts).Error; err != nil {
		return receipt, fmt.Errorf("%w: destination preparation record is incomplete", platform.ErrMigrationTargetOccupied)
	}
	if len(receipts) != 1 || receipts[0].ID != 1 {
		return receipt, fmt.Errorf("%w: destination preparation singleton cannot be verified", platform.ErrMigrationTargetOccupied)
	}
	receipt = receipts[0]
	if len(receipt.Token) != 64 || receipt.State != "prepared" || len(receipt.SchemaFingerprint) != 64 || len(receipt.SeedFingerprint) != 64 {
		return receipt, fmt.Errorf("%w: destination preparation is incomplete or already completed; inspect before retry", platform.ErrMigrationTargetOccupied)
	}
	identity, err := migrationDatabaseIdentity(db)
	if err != nil {
		return receipt, err
	}
	if identity != receipt.TargetIdentity {
		return receipt, fmt.Errorf("%w: destination database identity changed", platform.ErrMigrationTargetOccupied)
	}
	return receipt, nil
}

func prepareMigrationDestination(ctx context.Context, source, destination *gorm.DB, taskID uint, runID string, prepare func(*gorm.DB) error) (migrationPreparation, error) {
	source, destination = source.WithContext(ctx), destination.WithContext(ctx)
	if destination.Migrator().HasTable(migrationPreparationTable) {
		receipt, err := loadMigrationPreparation(destination, false)
		if err != nil {
			return receipt, err
		}
		if err := verifyPreparationSource(source, receipt); err != nil {
			return receipt, err
		}
		if err := verifyPreparationManifest(destination, receipt, false); err != nil {
			return receipt, err
		}
		return receipt, nil
	}
	var receipt migrationPreparation
	if err := EnsureMigrationTargetEmpty(destination); err != nil {
		return receipt, err
	}
	task, err := migrationTaskIdentity(source, taskID, runID, true)
	if err != nil {
		return receipt, err
	}
	sourceID, err := migrationDatabaseIdentity(source)
	if err != nil {
		return receipt, err
	}
	targetID, err := migrationDatabaseIdentity(destination)
	if err != nil {
		return receipt, err
	}
	if sourceID == targetID {
		return receipt, platform.ErrMigrationInvalid
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return receipt, err
	}
	now := time.Now().UTC()
	receipt = migrationPreparation{ID: 1, Token: hex.EncodeToString(token), SourceIdentity: sourceID, TargetIdentity: targetID, SourceTaskID: taskID, SourceTaskKey: task.IdempotencyKey, SourceRunID: runID, State: "preparing", CreatedAt: now, UpdatedAt: now}
	// A concurrent owner, or a crash between CREATE TABLE and this singleton
	// insert, cannot be adopted as an empty target on the next attempt.
	if err := destination.Migrator().CreateTable(&migrationPreparation{}); err != nil {
		return receipt, err
	}
	if err := destination.Create(&receipt).Error; err != nil {
		return receipt, err
	}
	if prepare == nil {
		return receipt, errors.New("migration destination schema initializer is required")
	}
	ownedDestination := datastore.WithOwnedMigrationPreparation(destination, func(proof *gorm.DB) error {
		return verifyMigrationSchemaPreparation(proof, source, receipt)
	})
	if err := prepare(ownedDestination); err != nil {
		return receipt, fmt.Errorf("prepare target schema: %w", err)
	}
	err = destination.Transaction(func(tx *gorm.DB) error {
		var current migrationPreparation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error; err != nil {
			return err
		}
		if !sameMigrationPreparationOwner(current, receipt) || current.State != "preparing" || current.SchemaFingerprint != "" || current.SeedFingerprint != "" {
			return platform.ErrMigrationTargetOccupied
		}
		schema, seeds, err := migrationPreparationManifest(tx, true)
		if err != nil {
			return err
		}
		if err := validateInitialMigrationSeeds(tx); err != nil {
			return err
		}
		receipt.State, receipt.SchemaFingerprint, receipt.SeedFingerprint = "prepared", schema, seeds
		return tx.Model(&migrationPreparation{}).Where("id = ? AND token = ? AND state = ?", 1, receipt.Token, "preparing").Updates(map[string]any{"state": receipt.State, "schema_fingerprint": schema, "seed_fingerprint": seeds, "updated_at": time.Now().UTC()}).Error
	})
	return receipt, err
}

// The composition root's fresh-MySQL guard invokes this in-process proof only
// for this exact metadata-only database. It cannot be supplied by request JSON
// and never exempts any application/unknown table from baseline validation.
func verifyMigrationSchemaPreparation(db, source *gorm.DB, expected migrationPreparation) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var receipts []migrationPreparation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Limit(2).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) != 1 {
			return platform.ErrMigrationTargetOccupied
		}
		current := receipts[0]
		if current.ID != 1 || current.Token != expected.Token || current.State != "preparing" || current.SourceIdentity != expected.SourceIdentity || current.TargetIdentity != expected.TargetIdentity || current.SourceTaskID != expected.SourceTaskID || current.SourceTaskKey != expected.SourceTaskKey || current.SourceRunID != expected.SourceRunID || current.SchemaFingerprint != "" || current.SeedFingerprint != "" {
			return platform.ErrMigrationTargetOccupied
		}
		identity, err := migrationDatabaseIdentity(tx)
		if err != nil {
			return err
		}
		if identity != expected.TargetIdentity {
			return platform.ErrMigrationTargetOccupied
		}
		if err := verifyPreparationSource(source, current); err != nil {
			return err
		}
		if _, err := migrationTaskIdentity(source, current.SourceTaskID, current.SourceRunID, true); err != nil {
			return err
		}
		tables, err := tx.Migrator().GetTables()
		if err != nil {
			return err
		}
		for _, table := range tables {
			if table != migrationPreparationTable && table != "schema_migrations" {
				return platform.ErrMigrationTargetOccupied
			}
		}
		var history int64
		if err := tx.Table("schema_migrations").Count(&history).Error; err != nil {
			return err
		}
		if history != 0 {
			return platform.ErrMigrationTargetOccupied
		}
		return nil
	})
}

func verifyPreparationManifest(db *gorm.DB, receipt migrationPreparation, lock bool) error {
	schema, seeds, err := migrationPreparationManifest(db, lock)
	if err != nil {
		return err
	}
	if schema != receipt.SchemaFingerprint || seeds != receipt.SeedFingerprint {
		return fmt.Errorf("%w: destination changed since owned initialization; no rows were cleared", platform.ErrMigrationTargetOccupied)
	}
	return nil
}

func sameMigrationPreparationOwner(a, b migrationPreparation) bool {
	return a.ID == b.ID && a.Token == b.Token && a.SourceIdentity == b.SourceIdentity && a.TargetIdentity == b.TargetIdentity && a.SourceTaskID == b.SourceTaskID && a.SourceTaskKey == b.SourceTaskKey && a.SourceRunID == b.SourceRunID
}

func guardMigrationPreparation(tx *gorm.DB, receipt migrationPreparation) error {
	current, err := loadMigrationPreparation(tx, true)
	if err != nil {
		return err
	}
	if !sameMigrationPreparationOwner(current, receipt) || current.SchemaFingerprint != receipt.SchemaFingerprint || current.SeedFingerprint != receipt.SeedFingerprint {
		return platform.ErrMigrationTargetOccupied
	}
	return verifyPreparationManifest(tx, current, true)
}

func completeMigrationPreparation(tx *gorm.DB, receipt migrationPreparation) error {
	result := tx.Model(&migrationPreparation{}).Where("id = ? AND token = ? AND state = ?", 1, receipt.Token, "prepared").Updates(map[string]any{"state": "completed", "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return platform.ErrMigrationTargetOccupied
	}
	return nil
}

func migrationQuote(db *gorm.DB, name string) string {
	var quoted strings.Builder
	db.Dialector.QuoteTo(&quoted, name)
	return quoted.String()
}

var mysqlAutoIncrementCounter = regexp.MustCompile(` AUTO_INCREMENT=[0-9]+`)

// Only the schema initializer's bounded seed families may be nonempty when a
// receipt is first prepared. This never admits an ordinary preinitialized DB:
// the singleton ownership record must predate PrepareSchema and bind this source.
func migrationPreparationManifest(db *gorm.DB, lock bool) (string, string, error) {
	if lock && !datastore.IsSQLite(db) {
		var isolation string
		if err := db.Raw("SELECT @@transaction_isolation").Scan(&isolation).Error; err != nil {
			return "", "", err
		}
		if isolation != "REPEATABLE-READ" && isolation != "SERIALIZABLE" {
			return "", "", errors.New("destination migration requires REPEATABLE READ or SERIALIZABLE to protect empty-table ranges")
		}
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return "", "", err
	}
	sort.Strings(tables)
	schema := []any{}
	seeds := []any{}
	allowed := map[string]bool{"schema_migrations": true, "email_templates": true, "subscription_templates": true, "system_configs": true, "automatic_renewal_scans": true, "menu_revisions": true, "menu_nodes": true, "job_execution_budget": true, "job_execution_groups": true}
	if datastore.IsSQLite(db) {
		allowed["traffic_ledger_revision"] = true
	}
	for _, table := range tables {
		if datastore.IsSQLite(db) && strings.HasPrefix(table, "sqlite_") {
			continue
		}
		if datastore.IsSQLite(db) {
			var definitions []struct{ Type, Name, SQL string }
			if err := db.Raw("SELECT type, name, sql FROM sqlite_master WHERE tbl_name = ? AND name NOT LIKE 'sqlite_%' ORDER BY type, name", table).Scan(&definitions).Error; err != nil {
				return "", "", err
			}
			schema = append(schema, []any{table, definitions})
		} else {
			rows, err := db.Raw("SHOW CREATE TABLE " + migrationQuote(db, table)).Rows()
			if err != nil {
				return "", "", err
			}
			var name, definition string
			if !rows.Next() {
				rows.Close()
				return "", "", errors.New("cannot inspect destination schema")
			}
			err = rows.Scan(&name, &definition)
			rows.Close()
			if err != nil {
				return "", "", err
			}
			if !strings.Contains(definition, "ENGINE=InnoDB") {
				return "", "", errors.New("destination migration requires transactional InnoDB tables")
			}
			// InnoDB may advance the counter despite a rolled-back INSERT; it is not
			// schema drift, nor permission to alter any row or primary-key identity.
			schema = append(schema, []string{table, mysqlAutoIncrementCounter.ReplaceAllString(definition, "")})
		}
		if table == migrationPreparationTable {
			continue // Pin its schema, but not the changing recovery state itself.
		}
		query := db.Table(migrationQuote(db, table)).Limit(1025)
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		rows, err := query.Rows()
		if err != nil {
			return "", "", err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", "", err
		}
		contents := []string{}
		for rows.Next() {
			if !allowed[table] {
				rows.Close()
				return "", "", fmt.Errorf("%w: destination contains data outside owned initialization in %s", platform.ErrMigrationTargetOccupied, table)
			}
			values, destinations := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				destinations[i] = &values[i]
			}
			if err := rows.Scan(destinations...); err != nil {
				rows.Close()
				return "", "", err
			}
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[i] = string(bytes)
				}
				if stamp, ok := value.(time.Time); ok {
					values[i] = stamp.UTC().Format(time.RFC3339Nano)
				}
			}
			payload, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				return "", "", err
			}
			contents = append(contents, string(payload))
			if len(contents) > 1024 {
				rows.Close()
				return "", "", fmt.Errorf("%w: destination initialization exceeds bounded manifest", platform.ErrMigrationTargetOccupied)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", "", err
		}
		sort.Strings(contents)
		seeds = append(seeds, []any{table, columns, contents})
	}
	if !datastore.IsSQLite(db) {
		var triggers []struct{ Name, Table, Timing, Event, Statement string }
		if err := db.Raw("SELECT TRIGGER_NAME AS `name`, EVENT_OBJECT_TABLE AS `table`, ACTION_TIMING AS `timing`, EVENT_MANIPULATION AS `event`, ACTION_STATEMENT AS `statement` FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE() ORDER BY TRIGGER_NAME").Scan(&triggers).Error; err != nil {
			return "", "", err
		}
		schema = append(schema, triggers)
	}
	return preparationDigest(schema), preparationDigest(seeds), nil
}
