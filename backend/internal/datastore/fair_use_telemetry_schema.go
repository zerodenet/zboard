package datastore

import (
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

// ReconcileFairUseTelemetrySchema installs raw behavioural telemetry,
// delivery coverage, hierarchical policy and subscription evaluation state.
// Fair Use remains independent from traffic accounting and runtime credential
// state: billing facts, observability facts and business restrictions have
// distinct ownership and lifecycles.
func ReconcileFairUseTelemetrySchema(db *gorm.DB) error {
	if IsSQLite(db) {
		return nil
	}
	if db == nil {
		return fmt.Errorf("database is required")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open fair use telemetry database: %w", err)
	}
	statements := []struct {
		name string
		sql  string
	}{
		{
			name: "flow-start telemetry",
			sql: `CREATE TABLE IF NOT EXISTS subscription_flow_start_events (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				node_id bigint unsigned NOT NULL,
				core_instance_id varchar(128) NOT NULL DEFAULT '',
				event_id varchar(191) NOT NULL,
				sequence bigint unsigned NOT NULL DEFAULT 0,
				principal_key varchar(191) NOT NULL DEFAULT '',
				user_id bigint unsigned NOT NULL DEFAULT 0,
				subscription_id bigint unsigned NOT NULL DEFAULT 0,
				protocol_credential_id bigint unsigned NOT NULL DEFAULT 0,
				protocol_endpoint_id bigint unsigned NOT NULL DEFAULT 0,
				mapping_state varchar(16) NOT NULL DEFAULT 'unmapped',
				occurred_at datetime(3) NOT NULL,
				received_at datetime(3) NOT NULL,
				created_at datetime(3) NOT NULL,
				PRIMARY KEY (id),
				UNIQUE KEY uk_subscription_flow_start_event (node_id, event_id),
				KEY idx_subscription_flow_start_subscription_time (subscription_id, occurred_at),
				KEY idx_subscription_flow_start_subscription_node_time (subscription_id, node_id, occurred_at),
				KEY idx_subscription_flow_start_subscription_received (subscription_id, received_at),
				KEY idx_subscription_flow_start_subscription_node_received (subscription_id, node_id, received_at),
				KEY idx_subscription_flow_start_user_time (user_id, occurred_at),
				KEY idx_subscription_flow_start_received (received_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "delivery coverage",
			sql: `CREATE TABLE IF NOT EXISTS fair_use_node_coverage (
				node_id bigint unsigned NOT NULL,
				core_instance_id varchar(128) NOT NULL DEFAULT '',
				last_sequence bigint unsigned NOT NULL DEFAULT 0,
				last_event_id varchar(191) NOT NULL DEFAULT '',
				continuous_since_at datetime(3) NOT NULL,
				last_received_at datetime(3) NOT NULL,
				last_event_occurred_at datetime(3) NOT NULL,
				last_gap_from_sequence bigint unsigned NOT NULL DEFAULT 0,
				last_gap_to_sequence bigint unsigned NOT NULL DEFAULT 0,
				last_gap_at datetime(3) NULL,
				gap_count bigint unsigned NOT NULL DEFAULT 0,
				updated_at datetime(3) NOT NULL,
				PRIMARY KEY (node_id),
				KEY idx_fair_use_node_coverage_received (last_received_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "hierarchical policy",
			sql: `CREATE TABLE IF NOT EXISTS fair_use_policies (
				scope_type varchar(16) NOT NULL,
				scope_id bigint unsigned NOT NULL DEFAULT 0,
				enabled tinyint(1) NOT NULL DEFAULT 0,
				evaluation_interval_seconds int NOT NULL DEFAULT 60,
				connection_start_threshold int NOT NULL DEFAULT 120,
				connection_start_window_seconds int NOT NULL DEFAULT 60,
				connection_start_penalty int NOT NULL DEFAULT 10,
				working_node_threshold int NOT NULL DEFAULT 3,
				working_node_window_seconds int NOT NULL DEFAULT 300,
				working_node_penalty int NOT NULL DEFAULT 15,
				score_max int NOT NULL DEFAULT 100,
				recovery_per_interval int NOT NULL DEFAULT 8,
				warning_score int NOT NULL DEFAULT 30,
				violation_score int NOT NULL DEFAULT 60,
				enforcement_mode varchar(16) NOT NULL DEFAULT 'observe',
				restriction_duration_seconds int NOT NULL DEFAULT 3600,
				revision bigint unsigned NOT NULL DEFAULT 1,
				created_at datetime(3) NOT NULL,
				updated_at datetime(3) NOT NULL,
				PRIMARY KEY (scope_type, scope_id),
				KEY idx_fair_use_policy_enabled (enabled),
				KEY idx_fair_use_policy_scope (scope_type, scope_id)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "subscription evaluation state",
			sql: `CREATE TABLE IF NOT EXISTS subscription_fair_use_states (
				subscription_id bigint unsigned NOT NULL,
				score int NOT NULL DEFAULT 0,
				state varchar(16) NOT NULL DEFAULT 'normal',
				current_active_flows bigint unsigned NULL,
				connection_starts int NOT NULL DEFAULT 0,
				working_nodes int NOT NULL DEFAULT 0,
				telemetry_completeness varchar(16) NOT NULL DEFAULT 'unknown',
				last_evaluated_at datetime(3) NULL,
				last_complete_at datetime(3) NULL,
				created_at datetime(3) NOT NULL,
				updated_at datetime(3) NOT NULL,
				PRIMARY KEY (subscription_id),
				KEY idx_subscription_fair_use_state (state),
				KEY idx_subscription_fair_use_evaluated (last_evaluated_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
		{
			name: "evaluation history",
			sql: `CREATE TABLE IF NOT EXISTS subscription_fair_use_events (
				id bigint unsigned NOT NULL AUTO_INCREMENT,
				subscription_id bigint unsigned NOT NULL,
				event_type varchar(32) NOT NULL,
				score_before int NOT NULL DEFAULT 0,
				score_after int NOT NULL DEFAULT 0,
				state_before varchar(16) NOT NULL DEFAULT 'normal',
				state_after varchar(16) NOT NULL DEFAULT 'normal',
				metrics_json json NOT NULL,
				reason text NOT NULL,
				occurred_at datetime(3) NOT NULL,
				created_at datetime(3) NOT NULL,
				PRIMARY KEY (id),
				KEY idx_subscription_fair_use_event_subscription_time (subscription_id, occurred_at),
				KEY idx_subscription_fair_use_event_type_time (event_type, occurred_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		},
	}
	for _, statement := range statements {
		if _, err := sqlDB.Exec(statement.sql); err != nil {
			return fmt.Errorf("reconcile fair use %s schema: %w", statement.name, err)
		}
	}
	indexes := []struct {
		table string
		name  string
		ddl   string
	}{
		{
			table: "subscription_flow_start_events",
			name:  "idx_subscription_flow_start_subscription_received",
			ddl:   "ALTER TABLE subscription_flow_start_events ADD KEY idx_subscription_flow_start_subscription_received (subscription_id, received_at)",
		},
		{
			table: "subscription_flow_start_events",
			name:  "idx_subscription_flow_start_subscription_node_received",
			ddl:   "ALTER TABLE subscription_flow_start_events ADD KEY idx_subscription_flow_start_subscription_node_received (subscription_id, node_id, received_at)",
		},
		{
			table: "subscriptions",
			name:  "idx_subscriptions_fair_use_candidates",
			ddl:   "ALTER TABLE subscriptions ADD KEY idx_subscriptions_fair_use_candidates (status, end_at, id)",
		},
	}
	for _, index := range indexes {
		if err := ensureFairUseIndex(sqlDB, index.table, index.name, index.ddl); err != nil {
			return err
		}
	}

	// #69 initially used a subscription-only policy table before the policy
	// hierarchy was finalized. Preserve any values created by prerelease/test
	// deployments as explicit subscription overrides instead of dropping them.
	if db.Migrator().HasTable("subscription_fair_use_policies") {
		if _, err := sqlDB.Exec(`INSERT IGNORE INTO fair_use_policies (
			scope_type, scope_id, enabled, evaluation_interval_seconds,
			connection_start_threshold, connection_start_window_seconds, connection_start_penalty,
			working_node_threshold, working_node_window_seconds, working_node_penalty,
			score_max, recovery_per_interval, warning_score, violation_score,
			enforcement_mode, restriction_duration_seconds, revision, created_at, updated_at
		) SELECT
			'subscription', subscription_id, enabled, evaluation_interval_seconds,
			connection_start_threshold, connection_start_window_seconds, connection_start_penalty,
			working_node_threshold, working_node_window_seconds, working_node_penalty,
			score_max, recovery_per_interval, warning_score, violation_score,
			'observe', 3600, revision, created_at, updated_at
		FROM subscription_fair_use_policies`); err != nil {
			return fmt.Errorf("migrate subscription fair use policies: %w", err)
		}
	}
	return nil
}

func ensureFairUseIndex(sqlDB *sql.DB, table, index, ddl string) error {
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, index).Scan(&count); err != nil {
		return fmt.Errorf("inspect Fair Use index %s: %w", index, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := sqlDB.Exec(ddl); err != nil {
		return fmt.Errorf("create Fair Use index %s: %w", index, err)
	}
	return nil
}
