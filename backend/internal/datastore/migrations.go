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
	"email_templates",
	"registration_email_challenges",
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
	"plan_sku_operations",
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

// preReleaseReconciledTables are included in a fresh squashed baseline but are
// installed additively for databases that recorded the baseline before those
// operational resources were introduced. They must not become requirements of
// the legacy baseline signature checked before route-time reconciliation.
var preReleaseReconciledTables = []string{"announcements"}

var preReleaseBaselineColumns = []struct {
	table      string
	column     string
	columnType string
}{
	{table: "flow_usages", column: "flow_id", columnType: "varchar(128)"},
	{table: "email_templates", column: "subject_template", columnType: "varchar(200)"},
	{table: "registration_email_challenges", column: "code_hash", columnType: "char(64)"},
	{table: "managed_certificates", column: "provider_account_id", columnType: "bigint unsigned"},
	{table: "managed_certificates", column: "webroot_path", columnType: "varchar(255)"},
	{table: "node_groups", column: "revision", columnType: "bigint unsigned"},
	{table: "plan_skus", column: "billing_mode", columnType: "varchar(20)"},
	{table: "plan_skus", column: "entitlement_mode", columnType: "varchar(24)"},
	{table: "plans", column: "revision", columnType: "bigint unsigned"},
	{table: "protocol_credentials", column: "credential_id", columnType: "varchar(96)"},
	{table: "protocol_endpoints", column: "managed_principal_ready", columnType: "tinyint(1)"},
	{table: "protocol_endpoints", column: "mieru_principal_ready", columnType: "tinyint(1)"},
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
	{table: "email_templates", index: "idx_email_templates_category_order"},
	{table: "registration_email_challenges", index: "ux_registration_email_challenge"},
	{table: "node_groups", index: "uk_node_groups_code"},
	{table: "node_operations", index: "idx_node_operations_history_cursor"},
	{table: "managed_certificates", index: "idx_managed_certificates_provider"},
	{table: "orders", index: "idx_orders_created_at_id"},
	{table: "plan_sku_operations", index: "idx_plan_sku_operations_operation"},
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
	if IsSQLite(db) {
		return runSQLiteMigrations(db)
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
		// An already-applied squashed baseline is not replayed. Install additive
		// commerce schema before validating the current baseline inventory so an
		// older development database can advance without failing on the new fields.
		if err := reconcilePlanSKUCommerceSchema(sqlDB); err != nil {
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
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS email_templates (
		  id bigint unsigned NOT NULL AUTO_INCREMENT,
		  name varchar(80) NOT NULL,
		  slug varchar(80) NOT NULL,
		  category varchar(24) NOT NULL,
		  trigger_key varchar(64) DEFAULT NULL,
		  subject_template varchar(200) NOT NULL,
		  body_template text NOT NULL,
		  is_active tinyint(1) NOT NULL DEFAULT '1',
		  sort_order int NOT NULL DEFAULT '0',
		  revision bigint unsigned NOT NULL DEFAULT '1',
		  created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		  updated_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		  PRIMARY KEY (id),
		  UNIQUE KEY uk_email_templates_slug (slug),
		  UNIQUE KEY uk_email_templates_trigger (trigger_key),
		  KEY idx_email_templates_category_order (category, sort_order, id),
		  KEY idx_email_templates_active (is_active)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
	`); err != nil {
		return fmt.Errorf("create email template resource: %w", err)
	}
	if _, err := db.Exec(`
		INSERT IGNORE INTO email_templates
		  (name, slug, category, trigger_key, subject_template, body_template, is_active, sort_order, revision, created_at, updated_at)
		VALUES
		  ('注册欢迎通知', 'registration-welcome', 'registration', 'user.registered', '欢迎加入 {{site_name}}', '你好，{{user_email}}：\n\n你的 {{site_name}} 账户已创建成功。\n\n访问地址：{{site_url}}\n注册时间：{{registered_at}}\n\n此邮件由系统自动发送，请勿直接回复。', 0, -100, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),
		  ('维护通知', 'maintenance-notice', 'operational', NULL, '{{site_name}} 服务维护通知', '你好，{{user_email}}：\n\n我们计划进行服务维护，请在发送前补充维护时间、影响范围和恢复计划。\n\n{{site_name}} 运营团队', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`); err != nil {
		return fmt.Errorf("seed email templates: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS registration_email_challenges (
		  id bigint unsigned NOT NULL AUTO_INCREMENT,
		  email varchar(128) NOT NULL,
		  purpose varchar(32) NOT NULL DEFAULT 'register',
		  code_hash char(64) NOT NULL,
		  requested_ip_hash char(64) NOT NULL DEFAULT '',
		  attempts int NOT NULL DEFAULT 0,
		  last_sent_at datetime(3) NOT NULL,
		  expires_at datetime(3) NOT NULL,
		  consumed_at datetime(3) DEFAULT NULL,
		  created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		  updated_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		  PRIMARY KEY (id),
		  UNIQUE KEY ux_registration_email_challenge (email, purpose),
		  KEY idx_registration_email_challenges_ip (requested_ip_hash),
		  KEY idx_registration_email_challenges_expires (expires_at),
		  KEY idx_registration_email_challenges_consumed (consumed_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
	`); err != nil {
		return fmt.Errorf("create registration email challenge resource: %w", err)
	}
	if _, err := db.Exec(`
		INSERT IGNORE INTO system_configs
		  (config_key, name, value, value_type, description, is_public, is_secret, revision, created_at, updated_at)
		VALUES
		  ('register_email_verification', '注册邮箱验证码', 'false', 'bool', '注册时必须先通过邮箱验证码；启用前需完成 SMTP 配置', 1, 0, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`); err != nil {
		return fmt.Errorf("seed registration email verification config: %w", err)
	}

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

	var providerColumn, webrootColumn int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(column_name = 'provider_account_id'), 0),
		        COALESCE(SUM(column_name = 'webroot_path'), 0)
		   FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'managed_certificates'
		    AND column_name IN ('provider_account_id', 'webroot_path')`,
	).Scan(&providerColumn, &webrootColumn); err != nil {
		return fmt.Errorf("inspect managed certificate challenge columns: %w", err)
	}
	if providerColumn == 0 {
		if _, err := db.Exec("ALTER TABLE managed_certificates ADD COLUMN provider_account_id bigint unsigned DEFAULT NULL AFTER node_id"); err != nil {
			return fmt.Errorf("add managed certificate provider ownership: %w", err)
		}
	}
	if webrootColumn == 0 {
		if _, err := db.Exec("ALTER TABLE managed_certificates ADD COLUMN webroot_path varchar(255) NOT NULL DEFAULT '' AFTER challenge_type"); err != nil {
			return fmt.Errorf("add managed certificate webroot path: %w", err)
		}
	}

	var providerIndex int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.statistics
		  WHERE table_schema = DATABASE()
		    AND table_name = 'managed_certificates'
		    AND index_name = 'idx_managed_certificates_provider'`,
	).Scan(&providerIndex); err != nil {
		return fmt.Errorf("inspect managed certificate provider index: %w", err)
	}
	if providerIndex == 0 {
		if _, err := db.Exec("ALTER TABLE managed_certificates ADD KEY idx_managed_certificates_provider (provider_account_id)"); err != nil {
			return fmt.Errorf("add managed certificate provider index: %w", err)
		}
	}

	var providerConstraint int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.referential_constraints
		  WHERE constraint_schema = DATABASE()
		    AND table_name = 'managed_certificates'
		    AND constraint_name = 'fk_managed_certificates_provider'`,
	).Scan(&providerConstraint); err != nil {
		return fmt.Errorf("inspect managed certificate provider constraint: %w", err)
	}
	if providerConstraint == 0 {
		if _, err := db.Exec(
			"ALTER TABLE managed_certificates ADD CONSTRAINT fk_managed_certificates_provider FOREIGN KEY (provider_account_id) REFERENCES provider_accounts (id) ON DELETE RESTRICT",
		); err != nil {
			return fmt.Errorf("add managed certificate provider constraint: %w", err)
		}
	}

	var managedReadyColumn int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'protocol_endpoints'
		    AND column_name = 'managed_principal_ready'`,
	).Scan(&managedReadyColumn); err != nil {
		return fmt.Errorf("inspect managed principal readiness column: %w", err)
	}
	if managedReadyColumn == 0 {
		if _, err := db.Exec(
			"ALTER TABLE protocol_endpoints ADD COLUMN managed_principal_ready tinyint(1) NOT NULL DEFAULT 0 AFTER multiplier_milli",
		); err != nil {
			return fmt.Errorf("add managed principal readiness column: %w", err)
		}
	}

	var mieruReadyColumn int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = DATABASE()
		    AND table_name = 'protocol_endpoints'
		    AND column_name = 'mieru_principal_ready'`,
	).Scan(&mieruReadyColumn); err != nil {
		return fmt.Errorf("inspect Mieru principal readiness column: %w", err)
	}
	if mieruReadyColumn == 0 {
		if _, err := db.Exec(
			"ALTER TABLE protocol_endpoints ADD COLUMN mieru_principal_ready tinyint(1) NOT NULL DEFAULT 0 AFTER managed_principal_ready",
		); err != nil {
			return fmt.Errorf("add Mieru principal readiness column: %w", err)
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
