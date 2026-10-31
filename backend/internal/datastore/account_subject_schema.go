package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const accountSubjectVersion = "0079_account_subjects.up.sql"

// Authentication-owned rows still reference users and are physically removed.
// Only historical ownership/actor references move to immutable subjects.
var subjectReferences = map[string][]string{
	"audit_logs": {"user_id"},
	"tickets":    {"user_id"}, "orders": {"user_id"}, "subscriptions": {"user_id"}, "protocol_credentials": {"user_id"}, "traffic_records": {"user_id"},
	"node_operations": {"requested_by"}, "provider_accounts": {"created_by"}, "managed_dns_records": {"created_by"},
	"wallet_accounts": {"user_id"}, "order_fundings": {"user_id"},
	"wallet_adjustments": {"user_id", "actor_id", "reversal_actor_id"}, "wallet_payout_results": {"actor_id"},
	"order_value_budgets": {"user_id"}, "subscription_value_settlements": {"user_id", "actor_id"},
	"payment_receipts": {"resolved_by"}, "subscription_automatic_renewals": {"user_id"}, "consumption_refunds": {"user_id", "actor_id"},
}

func reconcileAccountSubjectSchema(db *gorm.DB) error {
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", accountSubjectVersion).Count(&applied).Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TABLE IF NOT EXISTS account_cleanup_states (id BIGINT NOT NULL PRIMARY KEY, last_user_id BIGINT NOT NULL DEFAULT 0)").Error; err != nil {
		return err
	}
	if applied > 0 {
		return validateAccountSubjectSchema(db)
	}
	if IsSQLite(db) {
		if err := reconcileSQLiteSubjects(db); err != nil {
			return err
		}
	} else {
		if err := db.Exec("CREATE TABLE IF NOT EXISTS account_subjects (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB").Error; err != nil {
			return err
		}
		if err := db.Exec("INSERT IGNORE INTO account_subjects(id) SELECT id FROM users").Error; err != nil {
			return err
		}
		for table, columns := range subjectReferences {
			for _, column := range columns {
				var keys []struct{ Name, Target string }
				if err := db.Raw(`SELECT constraint_name AS name, referenced_table_name AS target FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND table_name=? AND column_name=? AND referenced_table_name IS NOT NULL`, table, column).Scan(&keys).Error; err != nil {
					return err
				}
				if len(keys) != 1 || (keys[0].Target != "users" && keys[0].Target != "account_subjects") {
					return fmt.Errorf("unexpected historical owner constraint: %s.%s", table, column)
				}
				if keys[0].Target == "account_subjects" {
					continue
				}
				name := keys[0].Name
				if strings.Contains(name, "`") {
					return errors.New("invalid owner constraint name")
				}
				ddl := fmt.Sprintf("ALTER TABLE `%s` DROP FOREIGN KEY `%s`, ADD CONSTRAINT `%s` FOREIGN KEY (`%s`) REFERENCES account_subjects(id) ON DELETE RESTRICT", table, name, "as_"+table+"_"+column, column)
				if err := retargetSubjectConstraint(db, ddl+", ALGORITHM=INPLACE, LOCK=NONE"); err != nil {
					return err
				}
			}
		}
	}
	if err := validateAccountSubjectSchema(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: accountSubjectVersion, AppliedAt: time.Now().UTC()}).Error
}

func validateAccountSubjectSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("account_subjects") {
		return errors.New("immutable account subjects missing; restore accounting identities before startup")
	}

	if IsSQLite(db) {
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='account_subjects'").Scan(&ddl).Error; err != nil {
			return err
		}
		if !strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
			return errors.New("account subject ID allocator must be monotonic")
		}
		for table := range subjectReferences {
			var keys []struct{ Table string }
			if err := db.Raw("PRAGMA foreign_key_list(`" + table + "`)").Scan(&keys).Error; err != nil {
				return err
			}
			for _, key := range keys {
				if key.Table == "users" {
					return fmt.Errorf("historical ownership still depends on live accounts: %s", table)
				}
			}
		}
	} else {
		var extra string
		if err := db.Raw("SELECT extra FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='account_subjects' AND column_name='id'").Scan(&extra).Error; err != nil {
			return err
		}
		if !strings.Contains(extra, "auto_increment") {
			return errors.New("account subject ID allocator must be monotonic")
		}
		var keys []struct{ OwnerTable, OwnerColumn, Target string }
		if err := db.Raw("SELECT table_name AS owner_table,column_name AS owner_column,referenced_table_name AS target FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND referenced_table_name IS NOT NULL").Scan(&keys).Error; err != nil {
			return err
		}
		for table, columns := range subjectReferences {
			for _, column := range columns {
				count := 0
				for _, key := range keys {
					if key.OwnerTable == table && key.OwnerColumn == column {
						if key.Target != "account_subjects" {
							return fmt.Errorf("invalid historical owner target: %s.%s", table, column)
						}
						count++
					}
				}
				if count != 1 {
					return fmt.Errorf("missing historical owner constraint: %s.%s", table, column)
				}
			}
		}
	}
	var missing []uint
	if err := db.Table("users u").Select("u.id").Joins("LEFT JOIN account_subjects s ON s.id=u.id").Where("s.id IS NULL").Limit(1).Scan(&missing).Error; err != nil {
		return err
	}
	if len(missing) > 0 {
		return errors.New("live account lacks immutable accounting identity")
	}

	return nil
}

var sqliteUserReference = regexp.MustCompile("(?i)REFERENCES\\s+[\"`]?users[\"`]?\\s*\\([\"`]?id[\"`]?\\)(?:\\s+ON DELETE (?:RESTRICT|CASCADE|SET NULL|NO ACTION))?")

// The existing enforced FK proves that every owner is a user, and the complete
// user ID set has just been copied to subjects. Retarget it without scanning or
// copying a telemetry table. There is no COPY fallback. Constraint checks are
// changed only on this pinned DDL connection and restored before it is pooled;
// normal account deletion always runs with constraints enabled.
func retargetSubjectConstraint(db *gorm.DB, ddl string) error {
	return migrationSession(db.Statement.Context, db, func(session *gorm.DB, connection *sql.Conn) (err error) {
		var checks, wait int
		if err := session.Raw("SELECT @@SESSION.foreign_key_checks").Scan(&checks).Error; err != nil {
			return err
		}
		if err := session.Raw("SELECT @@SESSION.lock_wait_timeout").Scan(&wait).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, restoreMigrationSession(connection, "SET SESSION foreign_key_checks=?", checks), restoreMigrationSession(connection, "SET SESSION lock_wait_timeout=?", wait))
		}()
		if err := session.Exec("SET SESSION lock_wait_timeout=5").Error; err != nil {
			return err
		}
		if err := session.Exec("SET SESSION foreign_key_checks=0").Error; err != nil {
			return err
		}
		return session.Exec(ddl).Error
	})
}

func reconcileSQLiteSubjects(db *gorm.DB) error {
	return migrationSession(db.Statement.Context, db, func(session *gorm.DB, connection *sql.Conn) (err error) {
		var previous int
		if err := session.Raw("PRAGMA foreign_keys").Scan(&previous).Error; err != nil {
			return err
		}
		if err := session.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, restoreMigrationSession(connection, fmt.Sprintf("PRAGMA foreign_keys=%d", previous)))
		}()
		return session.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE IF NOT EXISTS account_subjects(id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT)").Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT OR IGNORE INTO account_subjects(id) SELECT id FROM users").Error; err != nil {
				return err
			}
			for table := range subjectReferences {
				var ddl string
				if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&ddl).Error; err != nil {
					return err
				}
				replacement := sqliteUserReference.ReplaceAllString(ddl, "REFERENCES account_subjects(id) ON DELETE RESTRICT")
				if replacement == ddl {
					continue
				}
				// Preserve the complete table, indexes and triggers, including checks
				// and composite financial foreign keys. Only its user target changes.
				var extras []struct{ SQL string }
				if err := tx.Raw("SELECT sql FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger') AND sql IS NOT NULL", table).Scan(&extras).Error; err != nil {
					return err
				}
				var columns []struct{ Name string }
				if err := tx.Raw("PRAGMA table_info(`" + table + "`)").Scan(&columns).Error; err != nil {
					return err
				}
				names := make([]string, 0, len(columns))
				for _, c := range columns {
					names = append(names, "`"+c.Name+"`")
				}
				start := strings.Index(replacement, "(")
				if start < 0 {
					return fmt.Errorf("invalid owner table: %s", table)
				}
				temporary := table + "_subject_migration"
				if err := tx.Exec("CREATE TABLE `" + temporary + "` " + replacement[start:]).Error; err != nil {
					return err
				}
				joined := strings.Join(names, ",")
				if err := tx.Exec("INSERT INTO `" + temporary + "` (" + joined + ") SELECT " + joined + " FROM `" + table + "`").Error; err != nil {
					return err
				}
				if err := tx.Exec("DROP TABLE `" + table + "`").Error; err != nil {
					return err
				}
				if err := tx.Exec("ALTER TABLE `" + temporary + "` RENAME TO `" + table + "`").Error; err != nil {
					return err
				}
				for _, extra := range extras {
					if err := tx.Exec(extra.SQL).Error; err != nil {
						return err
					}
				}
			}

			rows, err := tx.Raw("PRAGMA foreign_key_check").Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			if rows.Next() {
				return errors.New("account subject migration found invalid foreign keys")
			}
			return rows.Err()
		})
	})
}
