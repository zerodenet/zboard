package datastore

import (
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type schemaMigration struct {
	Version   string    `gorm:"primaryKey;size:191"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string { return "schema_migrations" }

func IsSQLite(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == DriverSQLite
}

// databaseModels is dependency-ordered for fresh schema creation and is also
// the authoritative logical table inventory used by cross-database migration.
func databaseModels() []interface{} {
	return []interface{}{
		&model.User{}, &model.Installation{}, &model.SystemConfig{}, &model.Announcement{}, &model.AnnouncementRead{},
		&model.Plan{}, &model.PlanSKU{}, &model.PlanSKUOperation{}, &model.Node{}, &model.NodeGroup{}, &model.ProtocolEndpoint{},
		&model.NodeGroupEndpoint{}, &model.Subscription{}, &model.Order{}, &model.PaymentEvent{},
		&model.SubscriptionMember{}, &model.SubscriptionToken{}, &model.SubscriptionTemplate{},
		&model.SubscriptionRuleSet{}, &model.SubscriptionTemplateRuleSetBinding{}, &model.ProtocolCredential{},
		&model.FlowUsage{}, &model.TrafficRecord{}, &model.AuditLog{}, &model.EmailTemplate{},
		&model.RegistrationEmailChallenge{}, &model.Ticket{}, &model.TicketMessage{}, &model.UserAPIToken{},
		&model.Task{}, &model.TaskItem{}, &model.ProtocolDeployment{}, &model.QuotaEvent{},
		&model.NodeKernelState{}, &model.NodeOperation{}, &model.ProviderAccount{}, &model.ManagedDNSRecord{},
		&model.ProviderOperation{}, &model.ManagedCertificate{}, &model.CertificateProtocolEndpoint{},
		&model.CertificateOperation{},
	}
}

func runSQLiteMigrations(db *gorm.DB) error {
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	models := append([]interface{}{&schemaMigration{}}, databaseModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("apply sqlite schema: %w", err)
	}
	if err := reconcileSQLiteOperationalTables(db); err != nil {
		return err
	}
	record := schemaMigration{Version: preReleaseBaselineVersion, AppliedAt: time.Now().UTC()}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
		return fmt.Errorf("record sqlite schema version: %w", err)
	}
	return nil
}

// MigrationTables is the complete, dependency-ordered application data
// inventory. schema_migrations is deliberately omitted: the target database
// owns its own schema history.
func MigrationTables(db *gorm.DB) ([]string, error) {
	statements := databaseModels()
	tables := make([]string, 0, len(statements)+11)
	for _, statement := range statements {
		parsed := &gorm.Statement{DB: db}
		if err := parsed.Parse(statement); err != nil {
			return nil, fmt.Errorf("resolve migration table: %w", err)
		}
		tables = append(tables, parsed.Schema.Table)
	}
	tables = append(tables,
		"subscription_flow_start_events", "fair_use_node_coverage", "fair_use_policies",
		"subscription_fair_use_states", "subscription_fair_use_events",
		"zero_event_node_cursors", "principal_flow_node_generations", "principal_flow_observations",
		"principal_flow_currents", "principal_flow_scope_currents", "principal_flow_scope_observations",
	)
	return tables, nil
}

func reconcileSQLiteOperationalTables(db *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_flow_start_events (id INTEGER PRIMARY KEY AUTOINCREMENT, node_id INTEGER NOT NULL, core_instance_id TEXT NOT NULL DEFAULT '', event_id TEXT NOT NULL, sequence INTEGER NOT NULL DEFAULT 0, principal_key TEXT NOT NULL DEFAULT '', user_id INTEGER NOT NULL DEFAULT 0, subscription_id INTEGER NOT NULL DEFAULT 0, protocol_credential_id INTEGER NOT NULL DEFAULT 0, protocol_endpoint_id INTEGER NOT NULL DEFAULT 0, mapping_state TEXT NOT NULL DEFAULT 'unmapped', occurred_at DATETIME NOT NULL, received_at DATETIME NOT NULL, created_at DATETIME NOT NULL, UNIQUE(node_id, event_id))`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_subscription_time ON subscription_flow_start_events(subscription_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_subscription_node_time ON subscription_flow_start_events(subscription_id, node_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_subscription_received ON subscription_flow_start_events(subscription_id, received_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_subscription_node_received ON subscription_flow_start_events(subscription_id, node_id, received_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_user_time ON subscription_flow_start_events(user_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_flow_start_received ON subscription_flow_start_events(received_at)`,
		`CREATE TABLE IF NOT EXISTS fair_use_node_coverage (node_id INTEGER PRIMARY KEY, core_instance_id TEXT NOT NULL DEFAULT '', last_sequence INTEGER NOT NULL DEFAULT 0, last_event_id TEXT NOT NULL DEFAULT '', continuous_since_at DATETIME NOT NULL, last_received_at DATETIME NOT NULL, last_event_occurred_at DATETIME NOT NULL, last_gap_from_sequence INTEGER NOT NULL DEFAULT 0, last_gap_to_sequence INTEGER NOT NULL DEFAULT 0, last_gap_at DATETIME, gap_count INTEGER NOT NULL DEFAULT 0, updated_at DATETIME NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_fair_use_node_coverage_received ON fair_use_node_coverage(last_received_at)`,
		`CREATE TABLE IF NOT EXISTS fair_use_policies (scope_type TEXT NOT NULL, scope_id INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 0, evaluation_interval_seconds INTEGER NOT NULL DEFAULT 60, connection_start_threshold INTEGER NOT NULL DEFAULT 120, connection_start_window_seconds INTEGER NOT NULL DEFAULT 60, connection_start_penalty INTEGER NOT NULL DEFAULT 10, working_node_threshold INTEGER NOT NULL DEFAULT 3, working_node_window_seconds INTEGER NOT NULL DEFAULT 300, working_node_penalty INTEGER NOT NULL DEFAULT 15, score_max INTEGER NOT NULL DEFAULT 100, recovery_per_interval INTEGER NOT NULL DEFAULT 8, warning_score INTEGER NOT NULL DEFAULT 30, violation_score INTEGER NOT NULL DEFAULT 60, enforcement_mode TEXT NOT NULL DEFAULT 'observe', restriction_duration_seconds INTEGER NOT NULL DEFAULT 3600, revision INTEGER NOT NULL DEFAULT 1, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(scope_type, scope_id))`,
		`CREATE INDEX IF NOT EXISTS idx_fair_use_policy_enabled ON fair_use_policies(enabled)`,
		`CREATE TABLE IF NOT EXISTS subscription_fair_use_states (subscription_id INTEGER PRIMARY KEY, score INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'normal', current_active_flows INTEGER, connection_starts INTEGER NOT NULL DEFAULT 0, working_nodes INTEGER NOT NULL DEFAULT 0, telemetry_completeness TEXT NOT NULL DEFAULT 'unknown', last_evaluated_at DATETIME, last_complete_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_fair_use_state ON subscription_fair_use_states(state)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_fair_use_evaluated ON subscription_fair_use_states(last_evaluated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscriptions_fair_use_candidates ON subscriptions(status, end_at, id)`,
		`CREATE TABLE IF NOT EXISTS subscription_fair_use_events (id INTEGER PRIMARY KEY AUTOINCREMENT, subscription_id INTEGER NOT NULL, event_type TEXT NOT NULL, score_before INTEGER NOT NULL DEFAULT 0, score_after INTEGER NOT NULL DEFAULT 0, state_before TEXT NOT NULL DEFAULT 'normal', state_after TEXT NOT NULL DEFAULT 'normal', metrics_json TEXT NOT NULL, reason TEXT NOT NULL, occurred_at DATETIME NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_fair_use_event_subscription_time ON subscription_fair_use_events(subscription_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_fair_use_event_type_time ON subscription_fair_use_events(event_type, occurred_at)`,
		`CREATE TABLE IF NOT EXISTS zero_event_node_cursors (node_id INTEGER PRIMARY KEY, core_instance_id TEXT NOT NULL DEFAULT '', sequence INTEGER NOT NULL DEFAULT 0, config_revision INTEGER NOT NULL DEFAULT 0, occurred_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS principal_flow_node_generations (node_id INTEGER PRIMARY KEY, core_instance_id TEXT NOT NULL, started_at DATETIME NOT NULL, closed_at DATETIME, updated_at DATETIME NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_generation_instance ON principal_flow_node_generations(core_instance_id)`,
		`CREATE TABLE IF NOT EXISTS principal_flow_observations (id INTEGER PRIMARY KEY AUTOINCREMENT, node_id INTEGER NOT NULL, core_instance_id TEXT NOT NULL, session_registry_revision INTEGER NOT NULL, event_id TEXT NOT NULL, sequence INTEGER NOT NULL DEFAULT 0, principal_key TEXT NOT NULL, user_id INTEGER NOT NULL DEFAULT 0, subscription_id INTEGER NOT NULL DEFAULT 0, protocol_credential_id INTEGER NOT NULL DEFAULT 0, protocol_endpoint_id INTEGER NOT NULL DEFAULT 0, active_flows INTEGER NOT NULL, observed_at DATETIME NOT NULL, created_at DATETIME NOT NULL, UNIQUE(node_id, event_id), UNIQUE(node_id, core_instance_id, session_registry_revision, principal_key))`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_observation_user_time ON principal_flow_observations(user_id, observed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_observation_subscription_time ON principal_flow_observations(subscription_id, observed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_observation_principal_time ON principal_flow_observations(node_id, principal_key, observed_at)`,
		`CREATE TABLE IF NOT EXISTS principal_flow_currents (node_id INTEGER NOT NULL, principal_key TEXT NOT NULL, core_instance_id TEXT NOT NULL, session_registry_revision INTEGER NOT NULL, user_id INTEGER NOT NULL DEFAULT 0, subscription_id INTEGER NOT NULL DEFAULT 0, protocol_credential_id INTEGER NOT NULL DEFAULT 0, protocol_endpoint_id INTEGER NOT NULL DEFAULT 0, active_flows INTEGER NOT NULL, observed_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(node_id, principal_key))`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_current_user ON principal_flow_currents(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_current_subscription ON principal_flow_currents(subscription_id)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_current_instance ON principal_flow_currents(node_id, core_instance_id)`,
		`CREATE TABLE IF NOT EXISTS principal_flow_scope_currents (scope_type TEXT NOT NULL, scope_id INTEGER NOT NULL, active_flows INTEGER NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(scope_type, scope_id))`,
		`CREATE TABLE IF NOT EXISTS principal_flow_scope_observations (id INTEGER PRIMARY KEY AUTOINCREMENT, scope_type TEXT NOT NULL, scope_id INTEGER NOT NULL, active_flows INTEGER NOT NULL, node_id INTEGER NOT NULL, core_instance_id TEXT NOT NULL DEFAULT '', session_registry_revision INTEGER NOT NULL DEFAULT 0, event_id TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'lifecycle', observed_at DATETIME NOT NULL, created_at DATETIME NOT NULL, UNIQUE(scope_type, scope_id, event_id))`,
		`CREATE INDEX IF NOT EXISTS idx_principal_flow_scope_time ON principal_flow_scope_observations(scope_type, scope_id, observed_at)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply sqlite operational schema: %w", err)
		}
	}
	return nil
}
