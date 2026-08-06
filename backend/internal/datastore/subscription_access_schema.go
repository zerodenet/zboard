package datastore

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const subscriptionTokensTable = "subscription_tokens"

// ReconcileSubscriptionAccessSchema replaces the legacy user-scoped token
// boundary with one token row per subscription. Legacy rows with no
// subscription_id are deliberately deleted: retaining them would preserve the
// implicit account-level aggregation that this schema boundary removes.
func ReconcileSubscriptionAccessSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open subscription access schema database: %w", err)
	}

	exists, err := tableExists(sqlDB, subscriptionTokensTable)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if _, err := sqlDB.Exec(`DELETE FROM subscription_tokens WHERE subscription_id IS NULL`); err != nil {
		return fmt.Errorf("invalidate legacy aggregate subscription tokens: %w", err)
	}

	// InnoDB requires a usable index for every foreign key column. Install the
	// non-unique replacement before removing the legacy user-level unique index,
	// otherwise MySQL rejects the DROP INDEX with error 1553.
	nonUniqueUserIndexes, err := singleColumnIndexes(sqlDB, subscriptionTokensTable, "user_id", false)
	if err != nil {
		return err
	}
	if len(nonUniqueUserIndexes) == 0 {
		if _, err := sqlDB.Exec(`ALTER TABLE subscription_tokens ADD KEY idx_subscription_tokens_user (user_id)`); err != nil {
			return fmt.Errorf("add subscription token user lookup index: %w", err)
		}
	}

	uniqueUserIndexes, err := singleColumnIndexes(sqlDB, subscriptionTokensTable, "user_id", true)
	if err != nil {
		return err
	}
	for _, indexName := range uniqueUserIndexes {
		if err := dropIndex(sqlDB, subscriptionTokensTable, indexName); err != nil {
			return err
		}
	}

	if _, err := sqlDB.Exec(`ALTER TABLE subscription_tokens MODIFY subscription_id bigint unsigned NOT NULL`); err != nil {
		return fmt.Errorf("require subscription token ownership: %w", err)
	}

	// Add the subscription-level unique index before dropping any existing
	// non-unique foreign-key index. The new unique index can then satisfy the
	// foreign key while redundant indexes are removed safely.
	uniqueSubscriptionIndexes, err := singleColumnIndexes(sqlDB, subscriptionTokensTable, "subscription_id", true)
	if err != nil {
		return err
	}
	if len(uniqueSubscriptionIndexes) == 0 {
		if _, err := sqlDB.Exec(`ALTER TABLE subscription_tokens ADD UNIQUE KEY uq_subscription_token_subscription (subscription_id)`); err != nil {
			return fmt.Errorf("add subscription token uniqueness: %w", err)
		}
	}

	nonUniqueSubscriptionIndexes, err := singleColumnIndexes(sqlDB, subscriptionTokensTable, "subscription_id", false)
	if err != nil {
		return err
	}
	for _, indexName := range nonUniqueSubscriptionIndexes {
		if err := dropIndex(sqlDB, subscriptionTokensTable, indexName); err != nil {
			return err
		}
	}
	return nil
}

func tableExists(sqlDB *sql.DB, table string) (bool, error) {
	var count int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`,
		table,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	return count > 0, nil
}

func singleColumnIndexes(sqlDB *sql.DB, table, column string, unique bool) ([]string, error) {
	nonUnique := 1
	if unique {
		nonUnique = 0
	}
	rows, err := sqlDB.Query(
		`SELECT index_name
		   FROM information_schema.statistics
		  WHERE table_schema = DATABASE()
		    AND table_name = ?
		    AND index_name <> 'PRIMARY'
		    AND non_unique = ?
		  GROUP BY index_name
		 HAVING COUNT(*) = 1 AND MAX(column_name = ?) = 1`,
		table, nonUnique, column,
	)
	if err != nil {
		return nil, fmt.Errorf("inspect %s.%s indexes: %w", table, column, err)
	}
	defer rows.Close()
	return scanIndexNames(rows)
}

func scanIndexNames(rows *sql.Rows) ([]string, error) {
	result := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan index name: %w", err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read index names: %w", err)
	}
	return result, nil
}

func dropIndex(sqlDB *sql.DB, table, indexName string) error {
	statement := fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", quoteMySQLIdentifier(table), quoteMySQLIdentifier(indexName))
	if _, err := sqlDB.Exec(statement); err != nil {
		return fmt.Errorf("drop index %s on %s: %w", indexName, table, err)
	}
	return nil
}

func quoteMySQLIdentifier(value string) string {
	return strings.ReplaceAll(value, "`", "``")
}
