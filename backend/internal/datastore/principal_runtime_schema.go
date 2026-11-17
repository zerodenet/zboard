package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const principalRuntimeVersion = "0069_principal_runtime_identity.up.sql"
const principalRuntimeIndex = "uk_principal_flow_observation_runtime_revision"
const principalRuntimeTable = "principal_flow_observations"

var principalRuntimeTables = []string{"principal_flow_node_generations", "principal_flow_currents", "principal_flow_scope_observations", principalRuntimeTable}

// Must precede operational CREATE IF NOT EXISTS and copying. Missing recorded
// identity/index evidence is never interpreted as a genuinely empty source.
func preflightPrincipalRuntimeSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("principal runtime schema requires database")
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	var count int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", principalRuntimeVersion).Count(&count).Error; err != nil {
			return err
		}
	}
	if count == 0 {
		return nil
	}
	for _, table := range principalRuntimeTables {
		if !tables[table] {
			return fmt.Errorf("recorded principal runtime table %s missing; restore before startup/copy", table)
		}
		columns, _, err := projectionColumns(db, table)
		if err != nil {
			return err
		}
		c, ok := columns["core_instance_id"]
		expected := "mediumtext"
		if IsSQLite(db) {
			expected = "text"
		}
		if !ok || c.nullable || c.kind != expected {
			return fmt.Errorf("recorded principal runtime raw identity column differs in %s", table)
		}
	}
	if err := verifyPrincipalDigestColumn(db); err != nil {
		return err
	}
	present, err := principalRuntimeUniqueIndex(db)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("recorded principal runtime unique index missing; restore before startup/copy")
	}
	if !IsSQLite(db) {
		if err := verifyPrincipalRuntimePerformanceIndexes(db); err != nil {
			return err
		}
	}
	return nil
}

func verifyPrincipalDigestColumn(db *gorm.DB) error {
	columns, _, err := projectionColumns(db, principalRuntimeTable)
	if err != nil {
		return err
	}
	c, ok := columns["core_instance_sha256"]
	kind := "char(64)"
	if IsSQLite(db) {
		kind = "text"
	}
	if !ok || c.nullable || c.kind != kind || (!IsSQLite(db) && c.collation != "ascii_bin") {
		return errors.New("principal runtime digest column missing or incompatible; restore evidence before startup/copy")
	}
	return nil
}

// Index identity includes uniqueness, complete columns, collation, no partial
// filter/expression/prefix and deterministic order. A similarly named weakened
// index is rejected rather than silently adopted/replaced.
func principalRuntimeUniqueIndex(db *gorm.DB) (bool, error) {
	want := []string{"node_id", "core_instance_sha256", "session_registry_revision", "principal_key"}
	if IsSQLite(db) {
		var list []struct {
			Name    string
			Unique  int
			Partial int
		}
		if err := db.Raw("SELECT name,\"unique\",partial FROM pragma_index_list(?)", principalRuntimeTable).Scan(&list).Error; err != nil {
			return false, err
		}
		found := false
		for _, i := range list {
			if i.Name == principalRuntimeIndex {
				found = true
				if i.Unique != 1 || i.Partial != 0 {
					return false, errors.New("principal runtime index is nonunique or partial")
				}
			}
		}
		if !found {
			return false, nil
		}
		var rows []struct {
			Cid  int
			Name *string
			Desc int
			Coll *string
			Key  int
		}
		if err := db.Raw("SELECT cid,name,\"desc\",coll,\"key\" FROM pragma_index_xinfo(?) ORDER BY seqno", principalRuntimeIndex).Scan(&rows).Error; err != nil {
			return false, err
		}
		var cols []string
		for _, r := range rows {
			if r.Key == 0 {
				continue
			}
			if r.Cid < 0 || r.Name == nil || r.Coll == nil || *r.Coll != "BINARY" || r.Desc != 0 {
				return false, errors.New("principal runtime index expression/order/collation differs")
			}
			cols = append(cols, *r.Name)
		}
		if !slices.Equal(cols, want) {
			return false, errors.New("principal runtime index columns differ")
		}
		return true, nil
	}
	var rows []struct {
		ColumnName string
		NonUnique  int
		SubPart    sql.NullInt64
		Collation  sql.NullString
	}
	if err := db.Raw("SELECT COLUMN_NAME,NON_UNIQUE,SUB_PART,COLLATION FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX", principalRuntimeTable, principalRuntimeIndex).Scan(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	var cols []string
	for _, r := range rows {
		if r.NonUnique != 0 || r.SubPart.Valid || !r.Collation.Valid || r.Collation.String != "A" {
			return false, errors.New("principal runtime index uniqueness/prefix/order differs")
		}
		cols = append(cols, r.ColumnName)
	}
	if !slices.Equal(cols, want) {
		return false, errors.New("principal runtime index columns differ")
	}
	return true, nil
}

func reconcilePrincipalRuntimeSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("principal runtime schema requires database")
	}
	if err := preflightPrincipalRuntimeSchema(db); err != nil {
		return err
	}
	var count int64
	if db.Migrator().HasTable("schema_migrations") {
		if err := db.Table("schema_migrations").Where("version = ?", principalRuntimeVersion).Count(&count).Error; err != nil {
			return err
		}
	}
	if count > 0 {
		return nil
	}
	if !IsSQLite(db) {
		if err := ReconcileZeroEventSchema(db); err != nil {
			return err
		}
	}
	asset := principalRuntimeVersion
	if IsSQLite(db) {
		asset = "sqlite/0069_principal_runtime_identity.sql"
	}
	raw, err := migrations.Files.ReadFile(asset)
	if err != nil {
		return err
	}
	ddl, err := splitMigrationStatements(string(raw))
	if err != nil {
		return err
	}
	expected := 7
	if IsSQLite(db) {
		expected = 3
	}
	if len(ddl) != expected {
		return errors.New("principal runtime migration inventory differs")
	}
	columns, _, err := projectionColumns(db, principalRuntimeTable)
	if err != nil {
		return err
	}
	if _, ok := columns["core_instance_sha256"]; !ok {
		if err := db.Exec(ddl[0]).Error; err != nil {
			return err
		}
	}
	if err := verifyPrincipalDigestColumn(db); err != nil {
		return err
	}
	// A first/partial migration always rechecks the full raw/digest relation and
	// duplicate keys before any old unique key is removed. No bad digest is fixed.
	if err := backfillPrincipalRuntimeDigests(db); err != nil {
		return err
	}
	var duplicate struct{ NodeID uint }
	q := db.Table(principalRuntimeTable).Select("node_id").Group("node_id,core_instance_sha256,session_registry_revision,principal_key").Having("COUNT(*) > 1").Limit(1).Scan(&duplicate)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected > 0 {
		return errors.New("principal runtime identity duplicates require explicit recovery; no rows were merged")
	}
	present, err := principalRuntimeUniqueIndex(db)
	if err != nil {
		return err
	}
	indexStatement := 2
	if IsSQLite(db) {
		indexStatement = 1
	}
	if !present {
		if err := db.Exec(ddl[indexStatement]).Error; err != nil {
			return err
		}
	}
	if present, err = principalRuntimeUniqueIndex(db); err != nil || !present {
		if err != nil {
			return err
		}
		return errors.New("principal runtime uniqueness not confirmed")
	}
	if !IsSQLite(db) {
		// Every ALTER is atomic; the replacement unique constraint is already live.
		for _, spec := range []struct {
			table     string
			statement int
		}{{principalRuntimeTables[0], 3}, {principalRuntimeTables[1], 4}, {principalRuntimeTables[2], 5}, {principalRuntimeTable, 6}} {
			columns, _, err := projectionColumns(db, spec.table)
			if err != nil {
				return err
			}
			c, ok := columns["core_instance_id"]
			if !ok || c.nullable {
				return fmt.Errorf("principal raw identity missing from %s", spec.table)
			}
			if c.kind == "mediumtext" {
				continue
			}
			if c.kind != "varchar(128)" {
				return fmt.Errorf("principal raw identity type differs in %s", spec.table)
			}
			if err := db.Exec(ddl[spec.statement]).Error; err != nil {
				return err
			}
		}
	}
	// Verify all final columns before recording; prior ALTERs remain resumable.
	for _, table := range principalRuntimeTables {
		columns, _, err := projectionColumns(db, table)
		if err != nil {
			return err
		}
		c, ok := columns["core_instance_id"]
		expected := "mediumtext"
		if IsSQLite(db) {
			expected = "text"
		}
		if !ok || c.nullable || c.kind != expected {
			return fmt.Errorf("principal runtime raw identity not confirmed in %s", table)
		}
	}
	if !IsSQLite(db) {
		if err := verifyPrincipalRuntimePerformanceIndexes(db); err != nil {
			return err
		}
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{Version: principalRuntimeVersion, AppliedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	return preflightPrincipalRuntimeSchema(db)
}

func principalIdentityLengthSQL(db *gorm.DB) string {
	if IsSQLite(db) {
		return "length(CAST(core_instance_id AS BLOB))"
	}
	return "OCTET_LENGTH(core_instance_id)"
}

func backfillPrincipalRuntimeDigests(db *gorm.DB) error {
	if !IsSQLite(db) {
		return backfillMySQLPrincipalRuntimeDigests(db)
	}
	size := principalIdentityLengthSQL(db)
	var cursor uint64
	digestSize := "OCTET_LENGTH(core_instance_sha256)"
	if IsSQLite(db) {
		digestSize = "length(CAST(core_instance_sha256 AS BLOB))"
	}
	for {
		var heads []struct {
			ID          uint64
			BodyBytes   int64
			DigestBytes int64
		}
		if err := db.Table(principalRuntimeTable).Select("id,"+size+" AS body_bytes,"+digestSize+" AS digest_bytes").Where("id > ?", cursor).Order("id").Limit(64).Find(&heads).Error; err != nil {
			return err
		}
		if len(heads) == 0 {
			return nil
		}
		for _, head := range heads {
			if head.DigestBytes != 0 && head.DigestBytes != 64 {
				return errors.New("principal runtime digest has invalid byte length; retain original evidence")
			}
			if head.BodyBytes < 0 || head.BodyBytes > metering.MaxRuntimeIdentityBytes {
				return errors.New("principal runtime backfill source exceeds auxiliary identity budget")
			}
			// At most one <=2MiB body is materialized, in an exact-key locked TX.
			if err := db.Transaction(func(tx *gorm.DB) error {
				var row struct {
					CoreInstanceID     string
					CoreInstanceSHA256 string
				}
				q := tx.Table(principalRuntimeTable).Clauses(clause.Locking{Strength: "UPDATE"}).Select("core_instance_id,core_instance_sha256").Where("id = ? AND "+size+" = ? AND "+digestSize+" = ?", head.ID, head.BodyBytes, head.DigestBytes).Take(&row)
				if q.Error != nil {
					return q.Error
				}
				digest := metering.RuntimeIdentityDigest(row.CoreInstanceID)
				if row.CoreInstanceSHA256 != "" && row.CoreInstanceSHA256 != digest {
					return errors.New("principal runtime raw/digest mismatch; retain original evidence")
				}
				if row.CoreInstanceSHA256 == "" {
					return tx.Table(principalRuntimeTable).Where("id = ? AND core_instance_sha256 = ?", head.ID, "").Update("core_instance_sha256", digest).Error
				}
				return nil
			}); err != nil {
				return err
			}
			cursor = head.ID
		}
	}
}

// Legacy source conversion is target-owned and only adds the derived digest.
// Recorded sources must already carry verified raw/digest pairs and full schema.
func principalRuntimeCopyRecord(table string, record map[string]interface{}) (int64, error) {
	if !slices.Contains(principalRuntimeTables, table) {
		return 0, nil
	}
	raw, ok := record["core_instance_id"].(string)
	if !ok {
		return 0, errors.New("copied principal runtime raw identity is not text")
	}
	if len(raw) > metering.MaxRuntimeIdentityBytes {
		return 0, errors.New("copied principal runtime identity exceeds evidence bound")
	}
	if table == principalRuntimeTable {
		digest := metering.RuntimeIdentityDigest(raw)
		if old, ok := record["core_instance_sha256"]; ok && old != "" && old != digest {
			return 0, errors.New("copied principal runtime raw/digest mismatch")
		}
		record["core_instance_sha256"] = digest
	}
	return int64(len(raw)), nil
}

func preflightPrincipalRuntimeCopy(db *gorm.DB) error {
	if err := preflightPrincipalRuntimeSchema(db); err != nil {
		return err
	}
	tables, err := migrationTableSet(db)
	if err != nil {
		return err
	}
	size := principalIdentityLengthSQL(db)
	var recorded int64
	if tables["schema_migrations"] {
		if err := db.Table("schema_migrations").Where("version = ?", principalRuntimeVersion).Count(&recorded).Error; err != nil {
			return err
		}
	}
	if recorded > 0 {
		var head struct{ ID uint64 }
		q := db.Table(principalRuntimeTable).Select("id").Where("core_instance_sha256 = ?", "").Limit(1).Find(&head)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected > 0 {
			return errors.New("recorded principal runtime digest is empty; restore before copy")
		}
	}
	if tables[principalRuntimeTable] && db.Migrator().HasColumn(principalRuntimeTable, "core_instance_sha256") {
		digestSize := "OCTET_LENGTH(core_instance_sha256)"
		if IsSQLite(db) {
			digestSize = "length(CAST(core_instance_sha256 AS BLOB))"
		}
		var bad struct{ ID uint64 }
		q := db.Table(principalRuntimeTable).Select("id").Where(digestSize + " NOT IN (0,64)").Limit(1).Find(&bad)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected > 0 {
			return errors.New("copied principal runtime digest length is invalid")
		}
	}

	for _, table := range principalRuntimeTables {
		if !tables[table] {
			continue
		}
		var head struct{ Maximum int64 }
		if err := db.Table(table).Select("COALESCE(MAX(" + size + "),0) AS maximum").Take(&head).Error; err != nil {
			return err
		}
		if head.Maximum > metering.MaxRuntimeIdentityBytes {
			return errors.New("copied principal runtime body exceeds read budget")
		}
	}
	return nil
}

// The two historical nonunique indexes are only performance aids. Their raw
// prefix must never be used as a generation/authorization equality test.

func requirePrincipalRuntimeDestination(db *gorm.DB, sourceTables map[string]bool) error {
	for _, table := range principalRuntimeTables {
		if !sourceTables[table] {
			continue
		}
		columns, _, err := projectionColumns(db, table)
		if err != nil {
			return err
		}
		c, ok := columns["core_instance_id"]
		expected := "mediumtext"
		if IsSQLite(db) {
			expected = "text"
		}
		if !ok || c.nullable || c.kind != expected {
			return fmt.Errorf("principal runtime destination %s not prepared", table)
		}
	}
	if !sourceTables[principalRuntimeTable] {
		return nil
	}
	if err := verifyPrincipalDigestColumn(db); err != nil {
		return err
	}
	found, err := principalRuntimeUniqueIndex(db)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("principal runtime destination uniqueness not prepared")
	}
	return nil
}

func verifyPrincipalRuntimePerformanceIndexes(db *gorm.DB) error {
	for _, spec := range []struct {
		Table, Name string
		Columns     []string
		Prefix      []int64
	}{
		{"principal_flow_node_generations", "idx_principal_flow_generation_instance", []string{"core_instance_id"}, []int64{128}},
		{"principal_flow_currents", "idx_principal_flow_current_instance", []string{"node_id", "core_instance_id"}, []int64{0, 128}},
	} {
		var rows []struct {
			ColumnName string
			NonUnique  int
			SubPart    sql.NullInt64
			Collation  sql.NullString
		}
		if err := db.Raw("SELECT COLUMN_NAME,NON_UNIQUE,SUB_PART,COLLATION FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX", spec.Table, spec.Name).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(spec.Columns) {
			return errors.New("principal runtime performance index missing or incompatible")
		}
		for i, r := range rows {
			prefix := int64(0)
			if r.SubPart.Valid {
				prefix = r.SubPart.Int64
			}
			if r.ColumnName != spec.Columns[i] || r.NonUnique != 1 || prefix != spec.Prefix[i] || !r.Collation.Valid || r.Collation.String != "A" {
				return errors.New("principal runtime performance index shape differs")
			}
		}
	}
	return nil
}
