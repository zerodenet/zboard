package datastore

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

const trafficHourlyVersion = "0024_traffic_usage_hourly.up.sql"
const trafficHourlyApplicationVersion = "0025_traffic_hourly_application.up.sql"

func mysqlTrafficHourlyStatements() ([]string, error) {
	payload, err := migrations.Files.ReadFile(trafficHourlyApplicationVersion)
	if err != nil {
		return nil, err
	}
	return splitMigrationStatements(string(payload))
}

// MySQL DDL is not transactional. Reconcile each committed object, then rebuild
// only derived data and mark completion atomically. Other application writers
// must be stopped for the upgrade; the advisory lock serializes new migrators.
func reconcileMySQLTrafficHourly(db *gorm.DB) error {
	statements, err := mysqlTrafficHourlyStatements()
	if err != nil {
		return err
	}
	return migrationSession(db.Statement.Context, db, func(session *gorm.DB, conn *sql.Conn) (err error) {
		var name string
		if err := conn.QueryRowContext(session.Statement.Context, "SELECT DATABASE()").Scan(&name); err != nil {
			return err
		}
		name = fmt.Sprintf("zboard.hourly.%x", sha256.Sum256([]byte(name)))[:64]
		var locked int
		if err := conn.QueryRowContext(session.Statement.Context, "SELECT GET_LOCK(?, 30)", name).Scan(&locked); err != nil {
			return err
		}
		if locked != 1 {
			return fmt.Errorf("timed out acquiring traffic hourly migration lock")
		}
		defer func() { err = errors.Join(err, restoreMigrationSession(conn, "DO RELEASE_LOCK(?)", name)) }()
		var applied int64
		if err := session.Model(&schemaMigration{}).Where("version = ?", trafficHourlyApplicationVersion).Count(&applied).Error; err != nil {
			return err
		}
		if applied > 0 {
			needed, err := hourlyTableNeedsRecovery(session)
			if err != nil || !needed {
				return err
			}
		}
		if err := session.Exec(statements[0]).Error; err != nil {
			return err
		}
		for _, dimension := range []string{"user_id", "subscription_id", "node_id", "protocol_endpoint_id"} {
			index := "idx_traffic_hourly_" + dimension
			var count int64
			if err := session.Raw("SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_usage_hourly' AND INDEX_NAME = ?", index).Scan(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := session.Exec("CREATE INDEX " + index + " ON traffic_usage_hourly (" + dimension + ", record_at)").Error; err != nil {
					return err
				}
			}
		}
		for _, trigger := range []string{"traffic_hourly_insert", "traffic_hourly_update_remove", "traffic_hourly_update_add", "traffic_hourly_delete"} {
			// Metadata may hide triggers from an account lacking TRIGGER privilege.
			// Explicit DROP must fail rather than silently leaving a second writer.
			// This needs schema TRIGGER permission, not binlog SUPER permission.
			if err := session.Exec("DROP TRIGGER IF EXISTS " + trigger).Error; err != nil {
				return fmt.Errorf("remove legacy %s: %w", trigger, err)
			}
		}
		return session.Transaction(func(tx *gorm.DB) error {
			if err := RebuildMySQLTrafficHourly(tx); err != nil {
				return err
			}
			for _, version := range []string{trafficHourlyVersion, trafficHourlyApplicationVersion} {
				if err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE version = VALUES(version)", version, time.Now().UTC()).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// MySQL can commit CREATE TABLE and crash before the transactional backfill.
// An empty derived table with confirmed source facts still needs recovery even
// when older migration records exist. No reliable pending row is reset here.
func hourlyTableNeedsRecovery(db *gorm.DB) (bool, error) {
	if !db.Migrator().HasTable("traffic_usage_hourly") {
		return true, nil
	}
	var bucket struct{ N int }
	read := db.Table("traffic_usage_hourly").Select("1 AS n").Limit(1).Scan(&bucket)
	if read.Error != nil {
		return false, read.Error
	}
	if read.RowsAffected > 0 {
		return false, nil
	}
	query := db.Table("traffic_records").Select("id").Limit(1)
	if db.Migrator().HasColumn(usageProjectionTable, "hourly_pending") {
		confirmed := "NOT EXISTS (SELECT 1 FROM metering_usage_projection_pending p WHERE p.traffic_record_id = traffic_records.id AND p.hourly_pending = 1)"
		if db.Migrator().HasColumn("traffic_records", "record_count") && db.Migrator().HasColumn(usageProjectionTable, "delta_record_count") {
			// A mutable minute may already contain projected contributions even
			// while newer increments remain pending. They still require recovery.
			confirmed += " OR EXISTS (SELECT 1 FROM metering_usage_projection_pending p WHERE p.traffic_record_id = traffic_records.id AND p.hourly_pending = 1 AND p.delta_record_count > 0 AND traffic_records.record_count > p.delta_record_count)"
		}
		query = query.Where(confirmed)
	}
	var source struct{ ID uint }
	read = query.Scan(&source)
	return read.RowsAffected > 0, read.Error
}

// RebuildMySQLTrafficHourly is retained for existing copy callers. After 0054 it
// also rebuilds SQLite, whose synchronous hourly triggers have been removed.
func RebuildMySQLTrafficHourly(tx *gorm.DB) error {
	return RebuildTrafficHourly(tx)
}

// RebuildTrafficHourly participates in a caller-owned transaction. Pending
// hourly facts are excluded: their exact deltas still belong to the independent
// worker. Rebuilding them early and then consuming their intents would double
// project already settled usage. The pending rows are never cleared here.
func RebuildTrafficHourly(tx *gorm.DB) error {
	if tx.Migrator().HasColumn("traffic_records", "record_count") && tx.Migrator().HasColumn(usageProjectionTable, "delta_record_count") {
		return rebuildMinuteAwareHourly(tx)
	}
	if IsSQLite(tx) {
		if !tx.Migrator().HasColumn(usageProjectionTable, "hourly_pending") {
			return nil
		}
		payload, err := migrations.Files.ReadFile("sqlite/0024_traffic_usage_hourly.sql")
		if err != nil {
			return err
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			return err
		}
		for _, statement := range statements[:len(statements)-1] {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM traffic_usage_hourly").Error; err != nil {
			return err
		}
		insert := strings.Replace(statements[len(statements)-1], "FROM traffic_records GROUP BY", "FROM traffic_records WHERE NOT EXISTS (SELECT 1 FROM metering_usage_projection_pending p WHERE p.traffic_record_id = traffic_records.id AND p.hourly_pending = 1) GROUP BY", 1)
		return tx.Exec(insert).Error
	}
	statements, err := mysqlTrafficHourlyStatements()
	if err != nil {
		return err
	}
	for _, statement := range statements[1:] {
		if tx.Migrator().HasColumn(usageProjectionTable, "hourly_pending") {
			statement = strings.Replace(statement, "FROM traffic_records\nGROUP BY", "FROM traffic_records WHERE NOT EXISTS (SELECT 1 FROM metering_usage_projection_pending p WHERE p.traffic_record_id = traffic_records.id AND p.hourly_pending = 1)\nGROUP BY", 1)
		}
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("rebuild traffic hourly: %w", err)
		}
	}
	return nil
}
