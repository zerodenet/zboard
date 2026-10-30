package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var ErrProjectionNotPrepared = errors.New("projection database is not prepared")
var ErrProjectionValidationUnsupported = errors.New("projection database cannot be verified read-only")

// ProjectionValidationError retains the underlying error for errors.Is/As,
// without printing database statements, grants, account names or source values.
type ProjectionValidationError struct {
	Stage       string
	Kind, Cause error
}

func (e *ProjectionValidationError) Error() string { return e.Kind.Error() + ": " + e.Stage }
func (e *ProjectionValidationError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Cause}
}
func projectionError(stage string, cause error) error {
	return &ProjectionValidationError{Stage: stage, Kind: ErrProjectionNotPrepared, Cause: cause}
}
func projectionUnsupported(stage string, cause error) error {
	return &ProjectionValidationError{Stage: stage, Kind: ErrProjectionValidationUnsupported, Cause: cause}
}

// ValidatePreparedProjectionSchema checks the currently supported usage and
// node-observation projection dependencies. It never installs or repairs schema,
// initializes defaults, rotates credentials, creates a runtime, or claims work.
// This is a point-in-time admission check, not a lock against concurrent DDL.
// Deployment must finish preparation before starting workers and stop workers
// before schema changes. The application boundary also validates maintenance.
func ValidatePreparedProjectionSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return projectionError("database required", nil)
	}
	if db.Dialector.Name() != "sqlite" && db.Dialector.Name() != "mysql" {
		return projectionUnsupported("unsupported database driver", nil)
	}
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	inventorySQL := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE'"
	if IsSQLite(db) {
		inventorySQL = "SELECT name FROM sqlite_master WHERE type='table'"
	}
	names, err := projectionStringRows(db, inventorySQL)
	if err != nil {
		return projectionError("table metadata", err)
	}
	tables := map[string]bool{}
	for _, name := range names {
		tables[name] = true
	}
	if !tables["schema_migrations"] {
		return projectionError("migration receipts missing", nil)
	}
	versions, err := projectionStringRows(db, "SELECT version FROM schema_migrations WHERE applied_at IS NOT NULL ORDER BY version")
	if err != nil {
		return projectionError("migration receipts unreadable", err)
	}
	seen := map[string]bool{}
	for _, v := range versions {
		seen[v] = true
	}
	// Require only receipts owning this worker's actual dependencies. Missing
	// unrelated commerce/ticket receipts is not proof that a projection table is
	// unsafe. Extra history (including the retained development chain accepted
	// by preparePreReleaseMigrationHistory) is not reinterpreted or rewritten.
	// This validator does not certify whole-database or future-binary compatibility.
	required := []string{"0001_init.up.sql", "0007_jobs.up.sql", "0008_job_timeouts.up.sql", "0014_job_attempt_retries.up.sql", "0015_job_schedule_planning.up.sql", "0016_protocol_endpoint_usage_daily.up.sql", "0024_traffic_usage_hourly.up.sql", "0025_traffic_hourly_application.up.sql", usageProjectionSchemaVersion, hourlyProjectionSchemaVersion, nodeAssetSchemaVersion, nodeArchiveSchemaVersion, nodeObservationSchemaVersion, nodeObservationMaterialVersion, observationRetirementVersion}
	for _, v := range required {
		if !seen[v] {
			return projectionError("required migration receipt missing: "+v, nil)
		}
	}
	receiptColumns, receiptPK, e := projectionColumns(db, "schema_migrations")
	if e != nil {
		return projectionError("receipt structure", e)
	}
	if _, ok := receiptColumns["version"]; !ok || strings.Join(receiptPK, ",") != "version" {
		return projectionError("receipt primary key differs", nil)
	}
	if _, ok := receiptColumns["applied_at"]; !ok {
		return projectionError("receipt completion column missing", nil)
	}
	dependencies := []any{&model.MeteringUsageProjectionPending{}, &model.ProtocolEndpointUsageDaily{}, &model.SystemConfig{}}
	for _, value := range dependencies {
		stmt := &gorm.Statement{DB: db}
		if err = stmt.Parse(value); err != nil {
			return projectionError("dependency model", err)
		}
		if !tables[stmt.Table] {
			return projectionError("required table missing: "+stmt.Table, nil)
		}
		cols, primary, e := projectionColumns(db, stmt.Table)
		if e != nil {
			return projectionError("column metadata: "+stmt.Table, e)
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			col, ok := cols[field.DBName]
			if !ok {
				return projectionError("required column missing: "+stmt.Table+"."+field.DBName, nil)
			}
			if !projectionTypeCompatible(field, col.kind, IsSQLite(db)) {
				return projectionError("column type differs: "+stmt.Table+"."+field.DBName, nil)
			}
			if field.NotNull && col.nullable {
				return projectionError("nullable required column: "+stmt.Table+"."+field.DBName, nil)
			}
		}
		var want []string
		for _, field := range stmt.Schema.PrimaryFields {
			want = append(want, field.DBName)
		}
		if strings.Join(primary, ",") != strings.Join(want, ",") {
			return projectionError("primary key differs: "+stmt.Table, nil)
		}
	}
	// These source/read-model columns are exactly the projection SQL contract;
	// unrelated variable-size ledger Meta and credential tables are not read.
	narrow := map[string]string{
		"nodes":                "id,asset_tag,active_flows,bytes_up,bytes_down",
		"tasks":                "id,type,status",
		"traffic_records":      "id,user_id,subscription_id,node_id,protocol_endpoint_id,record_at,raw_bytes,upload_bytes,download_bytes,used_bytes,protocol_multiplier_milli",
		"traffic_usage_hourly": "record_at,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli,raw_bytes,upload_bytes,download_bytes,used_bytes,record_count",
	}
	for _, table := range []string{"nodes", "tasks", "traffic_records", "traffic_usage_hourly"} {
		if !tables[table] {
			return projectionError("required table missing: "+table, nil)
		}
		columns, pk, e := projectionColumns(db, table)
		if e != nil {
			return projectionError("source metadata: "+table, e)
		}
		for _, name := range strings.Split(narrow[table], ",") {
			column, ok := columns[name]
			if !ok {
				return projectionError("required source column missing: "+table+"."+name, nil)
			}
			if !IsSQLite(db) {
				expected := "bigint"
				nullable := false
				switch {
				case name == "asset_tag":
					expected = "varchar(64)"
				case name == "type":
					expected = "varchar(32)"
				case name == "status":
					expected = "smallint"
				case name == "record_at":
					expected = "datetime(3)"
					if table == "traffic_usage_hourly" {
						expected = "datetime"
					}
				case table == "nodes" || name == "id":
					expected = "bigint unsigned"
				case table == "traffic_records" && (name == "user_id" || name == "subscription_id" || name == "node_id" || name == "protocol_endpoint_id"):
					expected = "bigint unsigned"
					nullable = true
				}
				if column.kind != expected || column.nullable != nullable {
					return projectionError("source column shape differs: "+table+"."+name, nil)
				}
			} else {
				numeric := name != "asset_tag" && name != "type" && name != "record_at"
				if numeric && !strings.Contains(column.kind, "int") {
					return projectionError("source numeric type differs: "+table+"."+name, nil)
				}
				// Published SQLite 0024 stores the hour key as TEXT; source event timestamps remain datetime.
				validTimestamp := strings.HasPrefix(column.kind, "datetime") || (IsSQLite(db) && table == "traffic_usage_hourly" && column.kind == "text")
				if name == "record_at" && !validTimestamp {
					return projectionError("source timestamp type differs: "+table+"."+name, nil)
				}
			}
		}
		want := "id"
		if table == "traffic_usage_hourly" {
			want = "record_at,user_id,subscription_id,node_id,protocol_endpoint_id,protocol_multiplier_milli"
		}
		if strings.Join(pk, ",") != want {
			return projectionError("source primary key differs: "+table, nil)
		}
	}
	asset, err := inspectNodeAssetSchema(db)
	if err != nil {
		return projectionError("node asset evidence", err)
	}
	if !asset.receipt || !asset.nodes || !asset.column {
		return projectionError("node asset identity incomplete", nil)
	}
	if err := validateNodeArchiveSchema(db); err != nil {
		return projectionError("node archive authority", err)
	}
	for _, table := range observationTables {
		if !tables[table] {
			return projectionError("observation table missing: "+table, nil)
		}
		columns, e := observationColumnNames(db, table)
		if e != nil {
			return projectionError("observation columns: "+table, e)
		}
		for _, name := range observationColumns[table] {
			if !columns[name] {
				return projectionError("observation column missing: "+table+"."+name, nil)
			}
		}
		if _, _, e = projectionColumns(db, table); e != nil {
			return projectionError("observation transactional table: "+table, e)
		}
		if e = validateObservationShape(db, table); e != nil {
			return projectionError("observation shape: "+table, e)
		}
	}
	// Current node-observation ACKs read retirement markers and write the
	// review schedule. A prepared0060 database cannot run this binary's worker.
	// Reuse inspection only: reconciliation/index repair would violate this
	// admission boundary's strict read-only contract.
	material, recorded, materialErr := inspectObservationMaterialSchema(db)
	if materialErr != nil {
		return projectionError("observation material evidence", materialErr)
	}
	if !recorded || len(material) != 2 {
		return projectionError("observation material evidence incomplete", nil)
	}
	// readSource validates expired/retired receipt authority even though this
	// worker does not register the separate material-retention job. A recorded
	// but missing budget/receipt cannot be repaired or invented by admission.
	// This preflight performs only metadata, bounded singleton and source reads.
	if err = preflightObservationRetirementSchema(db); err != nil {
		return projectionError("observation retirement authority", err)
	}
	if err = projectionJobColumns(db, tables); err != nil {
		return err
	}
	if err = projectionIndexes(db); err != nil {
		return err
	}
	if err = preflightUsageProjectionSchema(db); err != nil {
		return projectionError("usage source protection", err)
	}
	if err = preflightHourlyProjectionSchema(db); err != nil {
		return projectionError("hourly cutover evidence", err)
	}
	if !IsSQLite(db) {
		// Metadata can hide triggers without TRIGGER permission; zero rows alone is
		// never proof. Direct current-schema/global grants are deliberately the only
		// supported proof, not role or partial/table-grant inference.
		if err = projectionTriggerVisibility(db); err != nil {
			return err
		}
	}
	query := "SELECT name FROM sqlite_master WHERE type='trigger' AND tbl_name='traffic_records' AND name IN ('traffic_hourly_replace','traffic_hourly_insert','traffic_hourly_update','traffic_hourly_delete','traffic_hourly_update_remove','traffic_hourly_update_add')"
	if !IsSQLite(db) {
		query = "SELECT TRIGGER_NAME FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND EVENT_OBJECT_TABLE='traffic_records' AND TRIGGER_NAME IN ('traffic_hourly_insert','traffic_hourly_update_remove','traffic_hourly_update_add','traffic_hourly_delete')"
	}
	triggers, err := projectionStringRows(db, query)
	if err != nil {
		return projectionError("hourly trigger metadata", err)
	}
	if len(triggers) != 0 {
		return projectionError("legacy hourly trigger still installed", nil)
	}
	var budgets []struct{ Capacity int }
	if err = db.Table("job_execution_budget").Select("capacity").Where("id = ?", 1).Find(&budgets).Error; err != nil {
		return projectionError("global budget source", err)
	}
	if len(budgets) != 1 || budgets[0].Capacity < 1 {
		return projectionError("global execution budget missing or invalid", nil)
	}
	// A zero-row SELECT checks actual SELECT visibility, not just metadata. No
	// payload, material parts, credentials or business rows are transferred.
	for _, table := range append([]string{"schema_migrations", "nodes", "tasks", "traffic_records", "traffic_usage_hourly"}, projectionDependencyTables()...) {
		rows, e := db.Table(table).Select("*").Where("1 = 0").Rows()
		if e != nil {
			return projectionError("dependency read permission: "+table, e)
		}
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return projectionError("dependency read completion: "+table, e)
		}
	}
	if err := ValidatePreparedFlowStartStatisticsSchema(ctx, db); err != nil {
		return projectionError("flow-start statistics dependencies", err)
	}
	if err := ValidatePreparedPrincipalStatisticsSchema(ctx, db); err != nil {
		return projectionError("principal statistics dependencies", err)
	}
	return nil
}
func projectionDependencyTables() []string {
	return []string{"flow_start_statistics_state", "flow_start_statistics_pending", "flow_start_statistics_points", "flow_start_statistics_minutes", "subscription_flow_start_events", "principal_statistics_state", "principal_statistics_pending", "principal_statistics_samples", "principal_statistics_hourly", "principal_flow_scope_observations", "job_execution_budget", "job_execution_groups", "job_dispatch_lanes", "job_runs", "job_attempts", "job_schedules", "metering_usage_projection_pending", "protocol_endpoint_usage_daily", "system_configs", "node_observation_intents", "node_observation_parts", "node_observation_states", "node_observation_material_budget", "node_observation_retirements", "node_observation_retirement_parts"}
}

type projectionColumn struct {
	kind      string
	nullable  bool
	collation string
}

func projectionColumns(db *gorm.DB, table string) (columns map[string]projectionColumn, primary []string, err error) {
	columns = map[string]projectionColumn{}
	keys := map[int]string{}
	if IsSQLite(db) {
		// table names come only from the fixed inventory/model contracts above.
		rows, e := db.Raw("PRAGMA table_info(" + table + ")").Rows()
		if e != nil {
			return nil, nil, e
		}
		for rows.Next() {
			var cid, required, pk int
			var name, kind string
			var def any
			if e = rows.Scan(&cid, &name, &kind, &required, &def, &pk); e != nil {
				break
			}
			columns[strings.ToLower(name)] = projectionColumn{kind: strings.ToLower(kind), nullable: required == 0 && pk == 0}
			if pk > 0 {
				keys[pk] = strings.ToLower(name)
			}
		}
		err = errors.Join(e, rows.Err(), rows.Close())
	} else {
		var engine sql.NullString
		var kind string
		r := db.Raw("SELECT ENGINE,TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table).Row()
		if err = r.Scan(&engine, &kind); err != nil {
			return nil, nil, err
		}
		if !engine.Valid || !strings.EqualFold(engine.String, "InnoDB") || kind != "BASE TABLE" {
			return nil, nil, errors.New("transactional base table required")
		}
		rows, e := db.Raw("SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLLATION_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table).Rows()
		if e != nil {
			return nil, nil, e
		}
		for rows.Next() {
			var name, kind, nullable string
			var collation sql.NullString
			if e = rows.Scan(&name, &kind, &nullable, &collation); e != nil {
				break
			}
			columns[strings.ToLower(name)] = projectionColumn{kind: strings.ToLower(kind), nullable: nullable == "YES", collation: collation.String}
		}
		err = errors.Join(e, rows.Err(), rows.Close())
		if err != nil {
			return nil, nil, err
		}
		rows, e = db.Raw("SELECT SEQ_IN_INDEX,COLUMN_NAME,SUB_PART FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME='PRIMARY' ORDER BY SEQ_IN_INDEX", table).Rows()
		if e != nil {
			return nil, nil, e
		}
		for rows.Next() {
			var n int
			var name string
			var prefix sql.NullInt64
			if e = rows.Scan(&n, &name, &prefix); e != nil {
				break
			}
			if prefix.Valid {
				e = errors.New("primary key prefix unsupported")
				break
			}
			keys[n] = strings.ToLower(name)
		}
		err = errors.Join(e, rows.Err(), rows.Close())
	}
	if err != nil {
		return nil, nil, err
	}
	for i := 1; i <= len(keys); i++ {
		if keys[i] == "" {
			return nil, nil, errors.New("invalid primary key positions")
		}
		primary = append(primary, keys[i])
	}
	return
}
func projectionTypeCompatible(f *schema.Field, kind string, sqlite bool) bool {
	if !sqlite && f.Size > 0 && strings.HasPrefix(kind, "varchar(") {
		var length int
		if _, err := fmt.Sscanf(kind, "varchar(%d)", &length); err != nil || length < f.Size {
			return false
		}
	}
	switch f.GORMDataType {
	case schema.Bool:
		if !sqlite {
			return kind == "tinyint(1)"
		}
		return strings.Contains(kind, "int") || kind == "boolean" || kind == "numeric"
	case schema.Int, schema.Uint:
		if !sqlite {
			if f.GORMDataType == schema.Uint {
				return kind == "bigint unsigned"
			}
			return kind == "bigint"
		}
		return strings.Contains(kind, "int")
	case schema.Time:
		if !sqlite {
			return kind == "datetime(3)"
		}
		return strings.HasPrefix(kind, "datetime")
	case schema.DataType("date"):
		return kind == "date" || kind == "text"
	case schema.String:
		return strings.Contains(kind, "char") || strings.Contains(kind, "text") || kind == "date"
	case schema.Bytes:
		return strings.Contains(kind, "blob") || strings.Contains(kind, "binary")
	}
	return false
}

func projectionTriggerVisibility(db *gorm.DB) error {
	var selected sql.NullString
	if err := db.Raw("SELECT DATABASE()").Row().Scan(&selected); err != nil {
		return projectionUnsupported("selected database proof unavailable", err)
	}
	if !selected.Valid || selected.String == "" {
		return projectionUnsupported("selected database required", nil)
	}
	rows, err := db.Raw("SHOW GRANTS FOR CURRENT_USER").Rows()
	if err != nil {
		return projectionUnsupported("trigger visibility proof unavailable", err)
	}
	var grants []string
	for rows.Next() {
		var grant string
		if err = rows.Scan(&grant); err != nil {
			break
		}
		grants = append(grants, grant)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return projectionUnsupported("trigger visibility proof unreadable", err)
	}
	if !projectionGrantSetProvesTriggers(grants, selected.String) {
		return projectionUnsupported("direct schema/global TRIGGER grant required to verify absence of competing hourly writers", nil)
	}
	return nil
}
func projectionGrantProvesTriggers(grant, database string) bool {
	if !strings.HasPrefix(grant, "GRANT ") {
		return false
	}
	p := strings.SplitN(strings.TrimPrefix(grant, "GRANT "), " ON ", 2)
	if len(p) != 2 {
		return false
	}
	target := strings.SplitN(p[1], " TO ", 2)
	if len(target) != 2 {
		return false
	}
	scope := target[0]
	quoted := "`" + strings.ReplaceAll(database, "`", "``") + "`.*"
	if scope != "*.*" && scope != quoted {
		return false
	}
	for _, privilege := range strings.Split(p[0], ",") {
		if x := strings.TrimSpace(privilege); x == "TRIGGER" || x == "ALL PRIVILEGES" {
			return true
		}
	}
	return false
}

// Single-column metadata reads explicitly propagate Query/Scan/Rows.Err/Close.
func projectionStringRows(db *gorm.DB, query string, args ...any) (result []string, err error) {
	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// A partial revoke can restrict a positive global grant. Never prove visibility
// by one positive row, or before consuming and closing the whole result.
func projectionGrantSetProvesTriggers(grants []string, database string) bool {
	proved := false
	for _, grant := range grants {
		if !projectionPlainGrant.MatchString(grant) {
			return false
		}
		proved = proved || projectionGrantProvesTriggers(grant, database)
	}
	return proved
}

var projectionPlainGrant = regexp.MustCompile("^GRANT [A-Z_ ,]+ ON (\\*\\.\\*|`([^`]|``)+`\\.\\*|`([^`]|``)+`\\.`([^`]|``)+`) TO (`([^`]|``)*`|'([^']|'')*')@(`([^`]|``)*`|'([^']|'')*')( WITH GRANT OPTION)?$")
