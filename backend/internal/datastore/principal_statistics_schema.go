package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const principalStatisticsVersion = "0070_principal_statistics.up.sql"

var principalStatisticsTables = []string{"principal_statistics_state", "principal_statistics_pending", "principal_statistics_samples", "principal_statistics_hourly"}
var principalStatisticsColumns = map[string][]string{
	"principal_statistics_state":   {"id", "legacy_upper_id", "legacy_after_id", "legacy_expected_rows", "legacy_projected_rows", "projected_head", "revision", "created_at", "updated_at"},
	"principal_statistics_pending": {"observation_id", "scope_type", "scope_id", "observed_at", "created_at"},
	"principal_statistics_samples": {"observation_id", "scope_type", "scope_id", "event_id", "observed_at", "active_flows", "source_digest", "source_created_at", "review_at", "raw_retired_at"},
	"principal_statistics_hourly":  {"scope_type", "scope_id", "hour_at", "peak", "sample_count"},
}
var principalStatisticsPrimary = map[string][]string{"principal_statistics_state": {"id"}, "principal_statistics_pending": {"observation_id"}, "principal_statistics_samples": {"observation_id"}, "principal_statistics_hourly": {"scope_type", "scope_id", "hour_at"}}

type principalStatisticsIndexSpec struct {
	Table, Name string
	Columns     []string
	Unique      bool
}

var principalStatisticsIndexes = []principalStatisticsIndexSpec{
	{"principal_statistics_pending", "idx_principal_stats_pending_scope", []string{"scope_type", "scope_id", "observed_at", "observation_id"}, false},
	{"principal_statistics_samples", "uk_principal_stats_sample_event", []string{"scope_type", "scope_id", "event_id"}, true},
	{"principal_statistics_samples", "idx_principal_stats_sample_time", []string{"scope_type", "scope_id", "observed_at", "observation_id"}, false},
	{"principal_statistics_samples", "idx_principal_stats_sample_retention", []string{"raw_retired_at", "review_at", "observation_id"}, false},
}

func inspectPrincipalStatisticsTable(db *gorm.DB, table string) error {
	cols, pk, err := projectionColumns(db, table)
	if err != nil {
		return err
	}
	if len(cols) != len(principalStatisticsColumns[table]) || !slices.Equal(pk, principalStatisticsPrimary[table]) {
		return fmt.Errorf("principal statistics table %s columns/primary differ", table)
	}
	for _, name := range principalStatisticsColumns[table] {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		switch name {
		case "scope_type":
			kind = "varchar(24)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "event_id":
			kind = "varchar(191)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "source_digest":
			kind = "char(64)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "created_at", "updated_at", "observed_at", "source_created_at", "review_at", "raw_retired_at", "hour_at":
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		c, ok := cols[name]
		if !ok || c.kind != kind || c.nullable != (name == "raw_retired_at") {
			return fmt.Errorf("principal statistics %s.%s shape differs", table, name)
		}
		if name == "source_digest" && !IsSQLite(db) && c.collation != "ascii_bin" {
			return errors.New("principal statistics digest collation differs")
		}
		if (name == "scope_type" || name == "event_id") && !IsSQLite(db) && c.collation != "utf8mb4_unicode_ci" {
			return errors.New("principal statistics identity collation differs from source contract")
		}
	}
	return nil
}
func principalStatisticsIndex(db *gorm.DB, s principalStatisticsIndexSpec) (bool, error) {
	var columns []string
	if IsSQLite(db) {
		var names []struct {
			Name    string
			Unique  int
			Partial int
		}
		if err := db.Raw("SELECT name,\"unique\",partial FROM pragma_index_list(?)", s.Table).Scan(&names).Error; err != nil {
			return false, err
		}
		found := false
		for _, n := range names {
			if n.Name == s.Name {
				found = true
				if (n.Unique == 1) != s.Unique || n.Partial != 0 {
					return false, errors.New("principal statistics index uniqueness/partial filter differs")
				}
			}
		}
		if !found {
			return false, nil
		}
		var parts []struct {
			Cid  int
			Name *string
			Desc int
			Coll *string
			Key  int
		}
		if err := db.Raw("SELECT cid,name,\"desc\",coll,\"key\" FROM pragma_index_xinfo(?) ORDER BY seqno", s.Name).Scan(&parts).Error; err != nil {
			return false, err
		}
		for _, p := range parts {
			if p.Key == 0 {
				continue
			}
			if p.Cid < 0 || p.Name == nil || p.Coll == nil || *p.Coll != "BINARY" || p.Desc != 0 {
				return false, errors.New("principal statistics index expression/collation/order differs")
			}
			columns = append(columns, *p.Name)
		}
	} else {
		var parts []struct {
			ColumnName string
			IsVisible  string
			IndexType  string
			SeqInIndex int
			NonUnique  int
			SubPart    sql.NullInt64
			Collation  sql.NullString
		}
		if err := db.Raw("SELECT COLUMN_NAME,NON_UNIQUE,SUB_PART,COLLATION,IS_VISIBLE,INDEX_TYPE,SEQ_IN_INDEX FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX", s.Table, s.Name).Scan(&parts).Error; err != nil {
			return false, err
		}
		if len(parts) == 0 {
			return false, nil
		}
		for i, p := range parts {
			if p.IsVisible != "YES" || p.IndexType != "BTREE" || p.SeqInIndex != i+1 {
				return false, errors.New("principal statistics index is invisible/non-BTREE/noncontiguous")
			}
			if (p.NonUnique == 0) != s.Unique || p.SubPart.Valid || !p.Collation.Valid || p.Collation.String != "A" {
				return false, errors.New("principal statistics index prefix/order/uniqueness differs")
			}
			columns = append(columns, p.ColumnName)
		}
	}
	if !slices.Equal(columns, s.Columns) {
		return false, errors.New("principal statistics index columns differ")
	}
	return true, nil
}
func validatePrincipalStatisticsState(row model.PrincipalStatisticsState) error {
	if row.ID != 1 || row.Revision == 0 || row.LegacyAfterID > row.LegacyUpperID || row.LegacyProjectedRows > row.LegacyExpectedRows || row.ProjectedHead < row.LegacyAfterID || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || (row.LegacyUpperID == 0) != (row.LegacyExpectedRows == 0) || (row.LegacyAfterID == row.LegacyUpperID && row.LegacyProjectedRows != row.LegacyExpectedRows) {
		return errors.New("principal statistics legacy coverage state is invalid; restore before startup/copy")
	}
	return nil
}
func preflightPrincipalStatisticsSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("principal statistics database required")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var applied int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	missing := false
	for _, table := range principalStatisticsTables {
		missing = missing || !tables[table]
	}
	if missing && applied == 0 {
		for _, table := range principalStatisticsTables {
			if !tables[table] {
				continue
			}
			var found []map[string]interface{}
			q := db.Table(table).Select(principalStatisticsPrimary[table][0]).Limit(1).Find(&found)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected > 0 {
				return errors.New("partial principal statistics authority survives missing tables; restore before preparation/copy")
			}
		}
	}
	for _, table := range principalStatisticsTables {
		if !tables[table] {
			if applied > 0 {
				return fmt.Errorf("recorded principal statistics table %s missing; restore before startup/copy", table)
			}
			continue
		}
		if err := inspectPrincipalStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range principalStatisticsIndexes {
		if !tables[spec.Table] {
			continue
		}
		present, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if applied > 0 && !present {
			return errors.New("recorded principal statistics index missing; restore before startup/copy")
		}
	}
	if applied > 0 {
		var rows []model.PrincipalStatisticsState
		if err := db.Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("recorded principal statistics checkpoint missing or nonunique; never initialize as empty")
		}
		if err := validatePrincipalStatisticsState(rows[0]); err != nil {
			return err
		}
		return validatePrincipalStatisticsSourceFloor(db)
	}
	return nil
}
func validatePrincipalStatisticsPrepared(db *gorm.DB) error {
	if err := preflightPrincipalStatisticsSchema(db); err != nil {
		return err
	}
	for _, table := range principalStatisticsTables {
		if err := inspectPrincipalStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range principalStatisticsIndexes {
		present, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if !present {
			return errors.New("principal statistics index not prepared")
		}
	}
	var state model.PrincipalStatisticsState
	if err := db.First(&state, "id = ?", 1).Error; err != nil {
		return err
	}
	if err := validatePrincipalStatisticsState(state); err != nil {
		return err
	}
	if err := validatePrincipalStatisticsSourceFloor(db); err != nil {
		return err
	}
	// The source must retain its complete durable primary key and original scope
	// event uniqueness. Reads never accept a view or a missing raw source table.
	return validatePrincipalStatisticsSource(db)
}
func ValidatePreparedPrincipalStatisticsSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return errors.New("principal statistics prepared database required")
	}
	db = db.WithContext(ctx)
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errors.New("principal statistics migration receipt missing; owned preparation required")
	}
	return validatePrincipalStatisticsPrepared(db)
}
func reconcilePrincipalStatisticsSchema(db *gorm.DB) error {
	if err := preflightPrincipalStatisticsSchema(db); err != nil {
		return err
	}
	var applied int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&applied).Error; err != nil {
			return err
		}
	}
	if applied > 0 {
		return validatePrincipalStatisticsPrepared(db)
	}
	asset := principalStatisticsVersion
	if IsSQLite(db) {
		asset = "sqlite/0070_principal_statistics.sql"
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
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	for _, table := range principalStatisticsTables {
		if err := inspectPrincipalStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range principalStatisticsIndexes {
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
	if err := initializePrincipalStatisticsState(db); err != nil {
		return err
	}
	if err := validatePrincipalStatisticsPrepared(db); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: principalStatisticsVersion, AppliedAt: time.Now().UTC()}).Error
}
func initializePrincipalStatisticsState(db *gorm.DB) error {
	var states []model.PrincipalStatisticsState
	if err := db.Limit(2).Find(&states).Error; err != nil {
		return err
	}
	if len(states) > 1 {
		return errors.New("principal statistics checkpoint is nonunique")
	}
	if len(states) == 1 {
		return validatePrincipalStatisticsState(states[0])
	}
	for _, table := range principalStatisticsTables[1:] {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errors.New("principal statistics evidence exists without original coverage checkpoint; restore, never guess zero")
		}
	}
	// Source writers/workers must be stopped for owned preparation or copy. One
	// aggregate row captures both MAX and COUNT; old histories are not projected
	// synchronously here and quotas/requests never wait for a whole-history scan.
	var source struct {
		Upper uint64
		Count uint64
	}
	q := db.Table("principal_flow_scope_observations").Select("COALESCE(MAX(id),0) AS upper,COUNT(*) AS count").Scan(&source)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return errors.New("principal legacy source boundary missing")
	}
	now := time.Now().UTC()
	state := model.PrincipalStatisticsState{ID: 1, LegacyUpperID: source.Upper, LegacyExpectedRows: source.Count, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := validatePrincipalStatisticsState(state); err != nil {
		return err
	}
	return db.Create(&state).Error
}
func initializeCopiedPrincipalStatistics(target *gorm.DB, sourceHasState, sourceHasRaw bool) error {
	if sourceHasState {
		return validatePrincipalStatisticsPrepared(target)
	}
	if !target.Migrator().HasTable("principal_statistics_state") {
		if sourceHasRaw {
			return errors.New("principal legacy source requires a prepared statistics destination")
		}
		return nil
	}
	proof, err := requirePrincipalStatisticsCopyProof(target)
	if err != nil {
		return err
	}
	if err := initializePrincipalStatisticsState(target); err != nil {
		return err
	}
	proof.Consumed = true
	return nil
}
func requirePrincipalStatisticsDestination(db *gorm.DB) error {
	for _, table := range principalStatisticsTables {
		if err := inspectPrincipalStatisticsTable(db, table); err != nil {
			return err
		}
	}
	for _, spec := range principalStatisticsIndexes {
		found, err := principalStatisticsIndex(db, spec)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("principal statistics destination index missing")
		}
	}
	return nil
}
func ValidatePrincipalStatisticsBinaryRollback(db *gorm.DB) error {
	if !db.Migrator().HasTable("schema_migrations") {
		return nil
	}
	var receipt int64
	if err := db.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&receipt).Error; err != nil {
		return err
	}
	if receipt != 0 {
		return errors.New("migration0070 requires compatible principal statistics writers; restore the corresponding complete pre0070 backup before starting an older binary, even when statistics are empty")
	}
	for _, table := range principalStatisticsTables {
		if db.Migrator().HasTable(table) {
			return errors.New("partial principal statistics authority requires owned preparation/complete pre0070 backup before binary rollback")
		}
	}
	return nil
}

func validatePrincipalStatisticsSource(db *gorm.DB) error {
	source, primary, err := projectionColumns(db, "principal_flow_scope_observations")
	if err != nil {
		return err
	}
	if !slices.Equal(primary, []string{"id"}) {
		return errors.New("principal statistics source primary differs")
	}
	for _, name := range []string{"id", "scope_type", "scope_id", "active_flows", "node_id", "core_instance_id", "session_registry_revision", "event_id", "source", "observed_at", "created_at"} {
		kind := "bigint unsigned"
		if IsSQLite(db) {
			kind = "integer"
		}
		switch name {
		case "scope_type":
			kind = "varchar(24)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "event_id":
			kind = "varchar(191)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "source":
			kind = "varchar(32)"
			if IsSQLite(db) {
				kind = "text"
			}
		case "core_instance_id":
			kind = "mediumtext"
			if IsSQLite(db) {
				kind = "text"
			}
		case "observed_at", "created_at":
			kind = "datetime(3)"
			if IsSQLite(db) {
				kind = "datetime"
			}
		}
		c, ok := source[name]
		if !ok || c.nullable || c.kind != kind {
			return fmt.Errorf("principal statistics source %s differs", name)
		}
	}
	spec := principalStatisticsIndexSpec{"principal_flow_scope_observations", "uk_principal_flow_scope_event", []string{"scope_type", "scope_id", "event_id"}, true}
	if IsSQLite(db) {
		var names []struct{ Name string }
		if err := db.Raw(`SELECT name FROM pragma_index_list(?) WHERE "unique"=1 AND partial=0`, spec.Table).Scan(&names).Error; err != nil {
			return err
		}
		for _, name := range names {
			candidate := spec
			candidate.Name = name.Name
			found, err := principalStatisticsIndex(db, candidate)
			if err == nil && found {
				return nil
			}
		}
		return errors.New("principal statistics source complete event uniqueness missing")
	}
	found, err := principalStatisticsIndex(db, spec)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("principal statistics source event uniqueness missing")
	}
	return nil
}

func preflightPrincipalStatisticsCopy(db *gorm.DB) error {
	if err := preflightPrincipalStatisticsSchema(db); err != nil {
		return err
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	any := false
	for _, table := range principalStatisticsTables {
		any = any || tables[table]
	}
	if any {
		var count int64
		if !tables["schema_migrations"] {
			return errors.New("partial principal statistics source must be prepared before copy")
		}
		if err := db.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("partial principal statistics source must finish owned preparation before copy")
		}
	}
	return nil
}

// Keep one genuine highest-ID raw source pinned. Generic cross-driver copies
// then restore the allocator floor by copying a real row; no fake observation,
// implicit-commit AUTO_INCREMENT DDL or sqlite_sequence copying is required.
func validatePrincipalStatisticsSourceFloor(db *gorm.DB) error {
	var heads struct{ RawHead, SampleHead, PendingHead, StateHead, LegacyHead uint64 }
	q := db.Raw("SELECT COALESCE((SELECT MAX(id) FROM principal_flow_scope_observations),0) AS raw_head,COALESCE((SELECT MAX(observation_id) FROM principal_statistics_samples),0) AS sample_head,COALESCE((SELECT MAX(observation_id) FROM principal_statistics_pending),0) AS pending_head,COALESCE((SELECT MAX(projected_head) FROM principal_statistics_state),0) AS state_head,COALESCE((SELECT MAX(legacy_upper_id) FROM principal_statistics_state),0) AS legacy_head").Scan(&heads)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return errors.New("principal source allocator floor cannot be verified")
	}
	floor := heads.SampleHead
	for _, head := range []uint64{heads.PendingHead, heads.StateHead, heads.LegacyHead} {
		if head > floor {
			floor = head
		}
	}
	if heads.RawHead < floor {
		return errors.New("principal source allocator recovery anchor missing; restore original highest raw source before startup/copy")
	}
	return nil
}

// Existing wire observations are milliseconds. A legacy SQLite-only source may
// nevertheless contain finer internal timestamps. MySQL DATETIME(3) cannot
// preserve those bucket boundaries; refuse that copy, leaving the source and
// destination transaction intact, instead of silently moving historical peaks.
func validatePrincipalStatisticsCopyTime(source, target *gorm.DB, table string, record map[string]interface{}) error {
	if !IsSQLite(source) || IsSQLite(target) || (table != "principal_flow_scope_observations" && table != "principal_statistics_samples" && table != "principal_statistics_pending") {
		return nil
	}
	value, ok := record["observed_at"]
	if !ok {
		return errors.New("principal observed-time copy evidence missing")
	}
	var at time.Time
	switch value := value.(type) {
	case time.Time:
		at = value
	case string:
		parsed := false
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999+00:00", "2006-01-02 15:04:05.999999999"} {
			v, err := time.Parse(layout, value)
			if err == nil {
				at = v
				parsed = true
				break
			}
		}
		if !parsed {
			return errors.New("principal observed-time precision cannot be proved for target")
		}
	default:
		return errors.New("principal observed-time storage type unsupported for exact copy")
	}
	if at.Nanosecond()%int(time.Millisecond) != 0 {
		return errors.New("legacy SQLite principal observation exceeds MySQL millisecond precision; retain original source and choose a compatible precision-preserving migration")
	}
	return nil
}
