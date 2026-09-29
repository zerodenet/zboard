package datastore

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
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
		if err := session.Model(&schemaMigration{}).Where("version = ?", trafficHourlyApplicationVersion).Count(&applied).Error; err != nil || applied > 0 {
			return err
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

// RebuildMySQLTrafficHourly participates in a caller-owned transaction. Transfers
// copy the authoritative ledger, then rebuild this projection before committing.
// SQLite transfers are maintained by their transaction-local ledger triggers.
func RebuildMySQLTrafficHourly(tx *gorm.DB) error {
	if IsSQLite(tx) {
		return nil
	}
	statements, err := mysqlTrafficHourlyStatements()
	if err != nil {
		return err
	}
	for _, statement := range statements[1:] {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("rebuild traffic hourly: %w", err)
		}
	}
	return nil
}
