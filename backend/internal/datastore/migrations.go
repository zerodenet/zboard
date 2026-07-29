package datastore

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

const createSchemaMigrationsSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(191) NOT NULL PRIMARY KEY,
  applied_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const (
	preReleaseBaselineVersion = "0001_init.up.sql"
	preSquashTerminalVersion  = "0032_subscription_policy_group_targets.up.sql"
)

var preReleaseBaselineTables = []string{
	"audit_logs",
	"certificate_operations",
	"certificate_protocol_endpoints",
	"provider_operations",
	"managed_dns_records",
	"provider_accounts",
	"flow_usages",
	"installations",
	"managed_certificates",
	"node_group_endpoints",
	"node_groups",
	"node_kernel_states",
	"node_operations",
	"nodes",
	"orders",
	"payment_events",
	"plan_skus",
	"plans",
	"protocol_credentials",
	"protocol_deployments",
	"protocol_endpoints",
	"quota_events",
	"subscription_members",
	"subscription_rule_sets",
	"subscription_template_rule_set_bindings",
	"subscription_templates",
	"subscription_tokens",
	"subscriptions",
	"system_configs",
	"task_items",
	"tasks",
	"ticket_messages",
	"tickets",
	"traffic_records",
	"user_api_tokens",
	"users",
}

var preReleaseBaselineColumns = []struct {
	table      string
	column     string
	columnType string
}{
	{table: "flow_usages", column: "flow_id", columnType: "varchar(128)"},
	{table: "node_groups", column: "revision", columnType: "bigint unsigned"},
	{table: "plans", column: "revision", columnType: "bigint unsigned"},
	{table: "protocol_credentials", column: "credential_id", columnType: "varchar(96)"},
	{table: "protocol_endpoints", column: "runtime_key", columnType: "varchar(36)"},
	{table: "subscription_template_rule_set_bindings", column: "action", columnType: "varchar(96)"},
	{table: "subscription_templates", column: "customization", columnType: "json"},
	{table: "subscription_templates", column: "renderer", columnType: "varchar(32)"},
}

var preReleaseBaselineIndexes = []struct {
	table string
	index string
}{
	{table: "audit_logs", index: "idx_audit_logs_history_cursor"},
	{table: "node_groups", index: "uk_node_groups_code"},
	{table: "node_operations", index: "idx_node_operations_history_cursor"},
	{table: "orders", index: "idx_orders_created_at_id"},
	{table: "protocol_deployments", index: "idx_protocol_deployments_history_cursor"},
	{table: "subscriptions", index: "idx_subscriptions_end_at_id"},
	{table: "tasks", index: "idx_tasks_history_cursor"},
	{table: "traffic_records", index: "idx_traffic_records_history_cursor"},
}

// RunMigrations applies every embedded .up.sql migration exactly once. MySQL
// DDL commits implicitly, so a migration is recorded only after all of its
// statements succeed; failures stop startup and keep the failing version
// unapplied for operator inspection. Before the first public release the
// repository ships one squashed v0.0.1 baseline. A database carrying the
// former development chain must already have reached its terminal migration.
// Its applied rows are preserved so the immediately previous development
// binary remains rollback-compatible.
func RunMigrations(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	if _, err := sqlDB.Exec(createSchemaMigrationsSQL); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	if err := validateMigrationInventory(versions); err != nil {
		return err
	}
	if err := preparePreReleaseMigrationHistory(sqlDB, versions); err != nil {
		return err
	}

	for _, version := range versions {
		var applied int
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		payload, err := migrations.Files.ReadFile(version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", version, err)
		}
		for index, statement := range statements {
			if _, err := sqlDB.Exec(statement); err != nil {
				return fmt.Errorf("apply migration %s statement %d: %w", version, index+1, err)
			}
		}
		if _, err := sqlDB.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, time.Now().UTC()); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}
	if isPreReleaseBaselineOnly(versions) {
		if err := finalizePreReleaseBaselineSchema(sqlDB); err != nil {
			return err
		}
		if err := validatePreReleaseBaselineSchema(sqlDB); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationInventory(versions []string) error {
	if len(versions) == 0 {
		return fmt.Errorf("no embedded database migrations found")
	}
	if versions[0] != preReleaseBaselineVersion {
		return fmt.Errorf("first embedded migration must be %s, got %s", preReleaseBaselineVersion, versions[0])
	}
	return nil
}

func isPreReleaseBaselineOnly(versions []string) bool {
	return len(versions) == 1 && versions[0] == preReleaseBaselineVersion
}

func preparePreReleaseMigrationHistory(db *sql.DB, versions []string) error {
	if !isPreReleaseBaselineOnly(versions) {
		return nil
	}

	var applied, baselineApplied, terminalApplied int
	if err := db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(version = ?), 0),
		        COALESCE(SUM(version = ?), 0)
		   FROM schema_migrations`,
		preReleaseBaselineVersion,
		preSquashTerminalVersion,
	).Scan(&applied, &baselineApplied, &terminalApplied); err != nil {
		return fmt.Errorf("inspect pre-release migration history: %w", err)
	}

	if applied == 0 {
		var applicationTables int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'",
		).Scan(&applicationTables); err != nil {
			return fmt.Errorf("inspect unversioned database: %w", err)
		}
		if applicationTables > 0 {
			return fmt.Errorf("unversioned pre-release database cannot adopt the squashed %s baseline; migrate it through %s with the previous v0.0.1 build or recreate the database", preReleaseBaselineVersion, preSquashTerminalVersion)
		}
		return nil
	}
	if baselineApplied == 0 {
		return fmt.Errorf("database migration history does not contain required baseline %s", preReleaseBaselineVersion)
	}
	if applied > 1 && terminalApplied == 0 {
		return fmt.Errorf("pre-release database must reach %s before adopting the squashed %s baseline", preSquashTerminalVersion, preReleaseBaselineVersion)
	}
	return nil
}

func databaseTableExists(db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
		table,
	).Scan(&count)
	return count > 0, err
}

func finalizePreReleaseBaselineSchema(db *sql.DB) error {
	archiveExists, err := databaseTableExists(db, "subscription_template_legacy_archives")
	if err != nil {
		return fmt.Errorf("inspect legacy subscription template archive: %w", err)
	}
	if archiveExists {
		var archived int
		if err := db.QueryRow("SELECT COUNT(*) FROM subscription_template_legacy_archives").Scan(&archived); err != nil {
			return fmt.Errorf("inspect legacy subscription template archive rows: %w", err)
		}
		if archived > 0 {
			return fmt.Errorf("legacy subscription template archive contains %d row(s); export or remove them before adopting the squashed v0.0.1 baseline", archived)
		}
		if _, err := db.Exec("DROP TABLE subscription_template_legacy_archives"); err != nil {
			return fmt.Errorf("remove empty legacy subscription template archive: %w", err)
		}
	}

	var oldIndex, finalIndex int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(index_name = 'uk_access_groups_code'), 0),
		        COALESCE(SUM(index_name = 'uk_node_groups_code'), 0)
		   FROM information_schema.statistics
		  WHERE table_schema = DATABASE()
		    AND table_name = 'node_groups'
		    AND index_name IN ('uk_access_groups_code', 'uk_node_groups_code')`,
	).Scan(&oldIndex, &finalIndex); err != nil {
		return fmt.Errorf("inspect node group code indexes: %w", err)
	}
	switch {
	case oldIndex > 0 && finalIndex == 0:
		if _, err := db.Exec("ALTER TABLE node_groups RENAME INDEX uk_access_groups_code TO uk_node_groups_code"); err != nil {
			return fmt.Errorf("rename node group code index: %w", err)
		}
	case oldIndex > 0 && finalIndex > 0:
		if _, err := db.Exec("ALTER TABLE node_groups DROP INDEX uk_access_groups_code"); err != nil {
			return fmt.Errorf("remove obsolete node group code index: %w", err)
		}
	}
	return nil
}

func validatePreReleaseBaselineSchema(db *sql.DB) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(preReleaseBaselineTables)), ",")
	args := make([]interface{}, 0, len(preReleaseBaselineTables))
	for _, table := range preReleaseBaselineTables {
		args = append(args, table)
	}
	var tableCount int
	if err := db.QueryRow(
		"SELECT COUNT(DISTINCT table_name) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ("+placeholders+")",
		args...,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("inspect pre-release baseline tables: %w", err)
	}
	if tableCount != len(preReleaseBaselineTables) {
		return fmt.Errorf("pre-release baseline schema is incomplete: found %d of %d required tables", tableCount, len(preReleaseBaselineTables))
	}

	for _, expected := range preReleaseBaselineColumns {
		var columnType string
		if err := db.QueryRow(
			"SELECT column_type FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
			expected.table,
			expected.column,
		).Scan(&columnType); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("pre-release baseline schema is missing column %s.%s", expected.table, expected.column)
			}
			return fmt.Errorf("inspect pre-release baseline column %s.%s: %w", expected.table, expected.column, err)
		}
		if !strings.EqualFold(strings.TrimSpace(columnType), expected.columnType) {
			return fmt.Errorf("pre-release baseline column %s.%s has type %s, want %s", expected.table, expected.column, columnType, expected.columnType)
		}
	}

	for _, expected := range preReleaseBaselineIndexes {
		var count int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			expected.table,
			expected.index,
		).Scan(&count); err != nil {
			return fmt.Errorf("inspect pre-release baseline index %s.%s: %w", expected.table, expected.index, err)
		}
		if count == 0 {
			return fmt.Errorf("pre-release baseline schema is missing index %s.%s", expected.table, expected.index)
		}
	}
	return nil
}

func splitMigrationStatements(source string) ([]string, error) {
	statements := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false
	lineComment := false
	blockComment := false
	runes := []rune(source)

	flush := func() {
		statement := strings.TrimSpace(current.String())
		if statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}

	for index := 0; index < len(runes); index++ {
		char := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}

		if lineComment {
			if char == '\n' {
				lineComment = false
				current.WriteRune(char)
			}
			continue
		}
		if blockComment {
			if char == '*' && next == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote == 0 {
			if char == '-' && next == '-' {
				lineComment = true
				index++
				continue
			}
			if char == '#' {
				lineComment = true
				continue
			}
			if char == '/' && next == '*' {
				blockComment = true
				index++
				continue
			}
			if char == '\'' || char == '"' || char == '`' {
				quote = char
				current.WriteRune(char)
				continue
			}
			if char == ';' {
				flush()
				continue
			}
			current.WriteRune(char)
			continue
		}

		current.WriteRune(char)
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '`' {
			escaped = true
			continue
		}
		if char == quote {
			if next == quote && quote != '`' {
				current.WriteRune(next)
				index++
				continue
			}
			quote = 0
		}
	}

	if quote != 0 || blockComment {
		return nil, fmt.Errorf("unterminated quoted value or block comment")
	}
	flush()
	return statements, nil
}
