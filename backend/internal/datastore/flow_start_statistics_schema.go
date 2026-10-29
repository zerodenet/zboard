package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const flowStartStatisticsVersion = "0071_flow_start_statistics.up.sql"

var flowStartStatisticsTables = []string{"flow_start_statistics_state", "flow_start_statistics_pending", "flow_start_statistics_points", "flow_start_statistics_minutes"}
var flowStartStatisticsColumns = map[string][]string{
	"flow_start_statistics_state":   {"id", "legacy_upper_id", "legacy_after_id", "legacy_expected_rows", "legacy_projected_rows", "projected_head", "revision", "created_at", "updated_at"},
	"flow_start_statistics_pending": {"source_id", "subscription_id", "node_id", "occurred_at", "received_at", "created_at"},
	"flow_start_statistics_points":  {"source_id", "subscription_id", "node_id", "occurred_at", "received_at", "source_created_at", "review_at", "fact_sha256"},
	"flow_start_statistics_minutes": {"subscription_id", "node_id", "basis", "minute_at", "event_count"},
}
var flowStartStatisticsPrimary = map[string][]string{"flow_start_statistics_state": {"id"}, "flow_start_statistics_pending": {"source_id"}, "flow_start_statistics_points": {"source_id"}, "flow_start_statistics_minutes": {"subscription_id", "node_id", "basis", "minute_at"}}

// Reuse the existing exact metadata reader (including visible BTREE and complete
// ordered columns), not schema-mutating reconciliation or name-only checks.
var flowStartStatisticsIndexes = []principalStatisticsIndexSpec{
	{"flow_start_statistics_pending", "idx_flow_stats_pending_received", []string{"subscription_id", "received_at", "source_id"}, false},
	{"flow_start_statistics_pending", "idx_flow_stats_pending_occurred", []string{"subscription_id", "occurred_at", "source_id"}, false},
	{"flow_start_statistics_points", "idx_flow_stats_point_retention", []string{"review_at", "source_id"}, false},
	{"flow_start_statistics_points", "idx_flow_stats_point_received", []string{"subscription_id", "received_at", "node_id", "source_id"}, false},
	{"flow_start_statistics_points", "idx_flow_stats_point_occurred", []string{"subscription_id", "occurred_at", "node_id", "source_id"}, false},
	{"flow_start_statistics_minutes", "idx_flow_stats_minute_read", []string{"subscription_id", "basis", "minute_at", "node_id"}, false},
}

func inspectFlowStartStatisticsTable(db *gorm.DB, table string) error {
	cols, pk, err := projectionColumns(db, table)
	if err != nil {
		return err
	}
	if len(cols) != len(flowStartStatisticsColumns[table]) || !slices.Equal(pk, flowStartStatisticsPrimary[table]) {
		return fmt.Errorf("flow-start statistics table %s columns/primary differ", table)
	}
	for _, name := range flowStartStatisticsColumns[table] {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		switch name {
		case "basis":
			kind = "varchar(16)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "fact_sha256":
			kind = "char(64)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "created_at", "updated_at", "occurred_at", "received_at", "source_created_at", "review_at", "minute_at":
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		c, ok := cols[name]
		if !ok || c.kind != kind || c.nullable {
			return fmt.Errorf("flow-start statistics %s.%s shape differs", table, name)
		}
		if name == "fact_sha256" && !IsSQLite(db) && c.collation != "ascii_bin" {
			return errors.New("flow-start statistics digest collation differs")
		}
		if name == "basis" && !IsSQLite(db) && c.collation != "utf8mb4_unicode_ci" {
			return errors.New("flow-start statistics identity collation differs from source contract")
		}
	}
	return nil
}
func validateFlowStartStatisticsState(row model.FlowStartStatisticsState) error {
	if row.ID != 1 || row.Revision == 0 || row.Revision > math.MaxInt64 || row.LegacyAfterID > row.LegacyUpperID || row.LegacyProjectedRows > row.LegacyExpectedRows || row.ProjectedHead < row.LegacyAfterID || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || (row.LegacyUpperID == 0) != (row.LegacyExpectedRows == 0) || (row.LegacyAfterID == row.LegacyUpperID && row.LegacyProjectedRows != row.LegacyExpectedRows) {
		return errors.New("flow-start statistics legacy coverage state is invalid; restore before startup/copy")
	}
	return nil
}
func preflightFlowStartStatisticsSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("flow-start statistics database required")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	missing := false
	for _, table := range flowStartStatisticsTables {
		missing = missing || !tables[table]
	}
	if missing && applied == 0 && tables["subscription_flow_start_events"] {
		columns, _, err := projectionColumns(db, "subscription_flow_start_events")
		if err != nil {
			return err
		}
		if _, registered := columns["statistics_registered"]; registered {
			return errors.New("source registration survives missing flow statistics authority; restore, never infer fresh coverage")
		}
	}
	if missing && applied == 0 {
		for _, table := range flowStartStatisticsTables {
			if !tables[table] {
				continue
			}
			var found []map[string]interface{}
			q := db.Table(table).Select(flowStartStatisticsPrimary[table][0]).Limit(1).Find(&found)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("partial flow-start statistics authority survives missing tables; restore before preparation/copy")
			}
		}
	}
	for _, table := range flowStartStatisticsTables {
		if !tables[table] {
			if applied > 0 {
				return fmt.Errorf("recorded flow-start statistics table %s missing; restore before startup/copy", table)
			}
			continue
		}
		if err := inspectFlowStartStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range flowStartStatisticsIndexes {
		if !tables[spec.Table] {
			continue
		}
		present, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if applied > 0 && !present {
			return errors.New("recorded flow-start statistics index missing; restore before startup/copy")
		}
	}
	if applied > 0 {
		var rows []model.FlowStartStatisticsState
		if err := db.Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("recorded flow-start statistics checkpoint missing or nonunique; never initialize as empty")
		}
		if err := validateFlowStartStatisticsState(rows[0]); err != nil {
			return err
		}
		return validateFlowStartStatisticsSource(db)
	}
	if tables["subscription_flow_start_events"] {
		return inspectFlowStartRegistration(db, false)
	}
	return nil
}
func validateFlowStartStatisticsPrepared(db *gorm.DB) error {
	if err := preflightFlowStartStatisticsSchema(db); err != nil {
		return err
	}
	for _, table := range flowStartStatisticsTables {
		if err := inspectFlowStartStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range flowStartStatisticsIndexes {
		present, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if !present {
			return errors.New("flow-start statistics index not prepared")
		}
	}
	var state model.FlowStartStatisticsState
	if err := db.First(&state, "id = ?", 1).Error; err != nil {
		return err
	}
	if err := validateFlowStartStatisticsState(state); err != nil {
		return err
	}
	// The source must retain its complete durable primary key and original scope
	// event uniqueness. Reads never accept a view or a missing raw source table.
	return validateFlowStartStatisticsSource(db)
}
func ValidatePreparedFlowStartStatisticsSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return errors.New("flow-start statistics prepared database required")
	}
	db = db.WithContext(ctx)
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errors.New("flow-start statistics migration receipt missing; owned preparation required")
	}
	return validateFlowStartStatisticsPrepared(db)
}
func reconcileFlowStartStatisticsSchema(db *gorm.DB) error {
	if err := preflightFlowStartStatisticsSchema(db); err != nil {
		return err
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if applied > 0 {
		return validateFlowStartStatisticsPrepared(db)
	}
	asset := flowStartStatisticsVersion
	if IsSQLite(db) {
		asset = "sqlite/0071_flow_start_statistics.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	statements, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	for _, statement := range statements {
		// DDL can partially commit on MySQL. An existing registration column may
		// only be accepted after its exact false-default shape is verified; never
		// rewrite retained true/false authority while retrying preparation.
		upper := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(upper, "ALTER TABLE SUBSCRIPTION_FLOW_START_EVENTS ADD COLUMN STATISTICS_REGISTERED") {
			columns, _, err := projectionColumns(db, "subscription_flow_start_events")
			if err != nil {
				return err
			}
			if _, ok := columns["statistics_registered"]; ok {
				if err := inspectFlowStartRegistration(db, false); err != nil {
					return err
				}
				continue
			}
		}
		if strings.HasPrefix(upper, "CREATE INDEX IDX_FLOW_START_STATISTICS_REGISTERED") {
			present, err := principalStatisticsIndex(db, flowStartRegistrationIndex)
			if err != nil {
				return err
			}
			if present {
				continue
			}
		}
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	for _, table := range flowStartStatisticsTables {
		if err := inspectFlowStartStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range flowStartStatisticsIndexes {
		found, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if !found {
			unique := ""
			if spec.Unique {
				unique = "UNIQUE "
			}
			if err := db.Exec("CREATE " + unique + "INDEX " + spec.Name + " ON " + spec.Table + "(" + strings.Join(spec.Columns, ",") + ")").Error; err != nil {
				return err
			}
		}
	}
	if err := initializeFlowStartStatisticsState(db); err != nil {
		return err
	}
	if err := validateFlowStartStatisticsPrepared(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: flowStartStatisticsVersion, AppliedAt: time.Now().UTC()}).Error
}
func initializeFlowStartStatisticsState(db *gorm.DB) error {
	var states []model.FlowStartStatisticsState
	if err := db.Limit(2).Find(&states).Error; err != nil {
		return err
	}
	if len(states) > 1 {
		return errors.New("flow-start statistics checkpoint is nonunique")
	}
	if len(states) == 1 {
		return validateFlowStartStatisticsState(states[0])
	}
	for _, table := range flowStartStatisticsTables[1:] {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("flow-start statistics evidence exists without original coverage checkpoint; restore, never guess zero")
		}
	}
	// Source writers/workers must be stopped for owned preparation or copy. One
	// aggregate row captures both MAX and COUNT; old histories are not projected
	// synchronously here and quotas/requests never wait for a whole-history scan.
	var source struct {
		Upper uint64
		Count uint64
	}
	q := db.Table("subscription_flow_start_events").Select("COALESCE(MAX(id),0) AS upper,COUNT(*) AS count").Scan(&source)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return errors.New("flow-start legacy source boundary missing")
	}
	now := time.Now().UTC()
	state := model.FlowStartStatisticsState{ID: 1, LegacyUpperID: source.Upper, LegacyExpectedRows: source.Count, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := validateFlowStartStatisticsState(state); err != nil {
		return err
	}
	return db.Create(&state).Error
}
func initializeCopiedFlowStartStatistics(target *gorm.DB, sourceHasState, sourceHasRaw bool) error {
	if sourceHasState {
		return validateFlowStartStatisticsPrepared(target)
	}
	if !target.Migrator().HasTable("flow_start_statistics_state") {
		if sourceHasRaw {
			return errors.New("flow-start legacy source requires a prepared statistics destination")
		}
		return nil
	}
	proof, err := requireFlowStartStatisticsCopyProof(target)
	if err != nil {
		return err
	}
	if err := initializeFlowStartStatisticsState(target); err != nil {
		return err
	}
	proof.Consumed = true
	return nil
}
func requireFlowStartStatisticsDestination(db *gorm.DB) error {
	for _, table := range flowStartStatisticsTables {
		if err := inspectFlowStartStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range flowStartStatisticsIndexes {
		found, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("flow-start statistics destination index missing")
		}
	}
	return validateFlowStartStatisticsSource(db)
}
func ValidateFlowStartStatisticsBinaryRollback(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var receipt int64
	if err := db.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&receipt).Error; err != nil {
		return err
	}
	if receipt != 0 {
		return errors.New("migration0071 requires compatible flow-start statistics writers; restore the corresponding complete pre0071 backup before starting an older binary, even when statistics are empty")
	}
	if db.Migrator().HasTable("subscription_flow_start_events") && db.Migrator().HasColumn("subscription_flow_start_events", "statistics_registered") {
		return errors.New("partial0071 source registration requires compatible binary or complete pre0071 restore")
	}
	for _, table := range flowStartStatisticsTables {
		if db.Migrator().HasTable(table) {
			return errors.New("partial flow-start statistics authority requires owned preparation/complete pre0071 backup before binary rollback")
		}
	}
	return nil
}

var flowStartRegistrationIndex = principalStatisticsIndexSpec{"subscription_flow_start_events", "idx_flow_start_statistics_registered", []string{"statistics_registered", "id"}, false}

func inspectFlowStartRegistration(db *gorm.DB, required bool) error {
	columns, _, err := projectionColumns(db, "subscription_flow_start_events")
	if err != nil {
		return err
	}
	column, ok := columns["statistics_registered"]
	if !ok {
		if required {
			return errors.New("recorded flow-start registration column missing; restore authority")
		}
		return nil
	}
	expected := "tinyint(1)"
	if IsSQLite(db) {
		expected = "boolean"
	}
	if column.nullable || column.kind != expected {
		return errors.New("flow-start registration column shape differs")
	}
	var value sql.NullString
	if IsSQLite(db) {
		err = db.Raw(`SELECT dflt_value FROM pragma_table_info('subscription_flow_start_events') WHERE name='statistics_registered'`).Row().Scan(&value)
	} else {
		err = db.Raw(`SELECT COLUMN_DEFAULT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='subscription_flow_start_events' AND COLUMN_NAME='statistics_registered'`).Row().Scan(&value)
	}
	if err != nil {
		return err
	}
	if !value.Valid || (strings.ToLower(strings.TrimSpace(value.String)) != "false" && strings.TrimSpace(value.String) != "0") {
		return errors.New("flow-start registration must default false, never guess complete")
	}
	present, err := principalStatisticsIndex(db, flowStartRegistrationIndex)
	if err != nil {
		return err
	}
	if required && !present {
		return errors.New("flow-start registration index missing")
	}
	return nil
}
func validateFlowStartStatisticsSource(db *gorm.DB) error {
	if err := inspectFlowStartRegistration(db, true); err != nil {
		return err
	}

	columns, primary, err := projectionColumns(db, "subscription_flow_start_events")
	if err != nil {
		return err
	}
	if !slices.Equal(primary, []string{"id"}) {
		return errors.New("flow-start source primary differs")
	}
	for _, name := range []string{"id", "node_id", "subscription_id", "occurred_at", "received_at", "created_at"} {
		expected := "bigint unsigned"
		if IsSQLite(db) {
			expected = "integer"
		}
		if strings.HasSuffix(name, "_at") {
			expected = "datetime(3)"
			if IsSQLite(db) {
				expected = "datetime"
			}
		}
		c, ok := columns[name]
		if !ok || c.nullable || c.kind != expected {
			return fmt.Errorf("flow-start source column %s differs", name)
		}
	}
	spec := principalStatisticsIndexSpec{"subscription_flow_start_events", "uk_subscription_flow_start_event", []string{"node_id", "event_id"}, true}
	if IsSQLite(db) {
		var names []struct{ Name string }
		if err := db.Raw(`SELECT name FROM pragma_index_list(?) WHERE "unique"=1 AND partial=0`, spec.Table).Scan(&names).Error; err != nil {
			return err
		}
		for _, name := range names {
			var columns []string
			if err := db.Raw(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, name.Name).Scan(&columns).Error; err != nil {
				return err
			}
			if !slices.Equal(columns, spec.Columns) {
				continue
			}
			test := spec
			test.Name = name.Name
			present, err := principalStatisticsIndex(db, test)
			if err != nil {
				return err
			}
			if present {
				return nil
			}
		}
		return errors.New("flow-start source event uniqueness missing")
	}
	present, err := principalStatisticsIndex(db, spec)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("flow-start source event uniqueness missing")
	}
	return nil
}
func preflightFlowStartStatisticsCopy(db *gorm.DB) error {
	if err := preflightFlowStartStatisticsSchema(db); err != nil {
		return err
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	present := false
	if tables["subscription_flow_start_events"] {
		columns, _, err := projectionColumns(db, "subscription_flow_start_events")
		if err != nil {
			return err
		}
		_, present = columns["statistics_registered"]
	}
	for _, table := range flowStartStatisticsTables {
		present = present || tables[table]
	}
	if present {
		if !tables["schema_migrations"] {
			return errors.New("partial flow-start statistics source requires owned preparation before copy")
		}
		var n int64
		if err := db.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&n).Error; err != nil {
			return err
		}
		if n != 1 {
			return errors.New("partial flow-start statistics source requires owned preparation before copy")
		}
	}
	return nil
}

// Unlike0070's permanently retained principal receipts, these raw/point/minute
// rows share the original15-day TTL. All can legitimately be empty. Do not pin
// a diagnostic solely to recover an obsolete historical MAX; copied live IDs
// determine the target allocator, and every new compatible write owns pending.
func validateFlowStartStatisticsCopyTime(source, target *gorm.DB, table string, record map[string]interface{}) error {
	if !IsSQLite(source) || IsSQLite(target) {
		return nil
	}
	var columns []string
	switch table {
	case "subscription_flow_start_events":
		columns = []string{"occurred_at", "received_at", "created_at"}
	case "flow_start_statistics_pending":
		columns = []string{"occurred_at", "received_at"}
	case "flow_start_statistics_points":
		columns = []string{"occurred_at", "received_at", "source_created_at"}
	case "flow_start_statistics_minutes":
		columns = []string{"minute_at"}
	default:
		return nil
	}
	for _, column := range columns {
		value, ok := record[column]
		if !ok {
			return errors.New("flow-start copy time evidence missing")
		}
		var at time.Time
		switch value := value.(type) {
		case time.Time:
			at = value
		case string:
			parsed := false
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999"} {
				v, err := time.Parse(layout, value)
				if err == nil {
					at = v
					parsed = true
					break
				}
			}
			if !parsed {
				return errors.New("flow-start copy timestamp representation is unsupported")
			}
		default:
			return errors.New("flow-start copy timestamp storage type is unsupported")
		}
		if at.Nanosecond()%int(time.Millisecond) != 0 {
			return errors.New("SQLite flow-start fact time exceeds MySQL millisecond precision; retain source and choose a precision-preserving migration")
		}
	}
	return nil
}
