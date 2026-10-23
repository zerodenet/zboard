package handler

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// This is opt-in BUSINESS setup, not evidence that the target ran migrations.
// Keep newMySQLPublishHandlers/newMySQLOrderFixture for startup, permission and
// migration contracts. Every business invocation still owns a new random schema
// and executes complete CREATE TABLE DDL with FKs/CHECKs enabled.
type mysqlBusinessDatabase struct {
	DB          *gorm.DB
	DSN, Schema string
	close       func() error
}

func mysqlBusinessConfig(t *testing.T) *mysqlconfig.Config {
	t.Helper()
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN for isolated real MySQL tests")
	}
	cfg, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid isolated MySQL test configuration")
	}
	cfg.DBName, cfg.ParseTime, cfg.Loc = "", true, time.UTC
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
	return cfg
}
func openMySQLBusinessDatabase(cfg *mysqlconfig.Config) (out mysqlBusinessDatabase, err error) {
	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		return out, err
	}
	name := "zboard_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE DATABASE " + businessQuote(name) + " CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		return out, err
	}
	var pool *sql.DB
	cleanup := func() error {
		var err error
		if pool != nil {
			err = pool.Close()
		}
		_, dropErr := admin.Exec("DROP DATABASE " + businessQuote(name))
		return errors.Join(err, dropErr, admin.Close())
	}
	targetCfg := cfg.Clone()
	targetCfg.DBName = name
	db, err := datastore.Open(targetCfg.FormatDSN())
	if err != nil {
		return out, errors.Join(err, cleanup())
	}
	pool, err = db.DB()
	if err != nil {
		return out, errors.Join(err, cleanup())
	}
	return mysqlBusinessDatabase{DB: db, DSN: targetCfg.FormatDSN(), Schema: name, close: cleanup}, nil
}
func businessQuote(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

var businessIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// The cache contains immutable values only, never a database, runtime or pool.
// Failures are retained rather than retried/fallback-masked by later tests.
var businessTemplates struct {
	sync.Mutex
	entries map[[32]byte]*mysqlBusinessTemplateEntry
}

type mysqlBusinessTemplateEntry struct {
	once     sync.Once
	snapshot *mysqlBusinessSnapshot
	err      error
}

func mysqlBusinessTemplate(cfg *mysqlconfig.Config) (*mysqlBusinessSnapshot, error) {
	key := sha256.Sum256([]byte(cfg.FormatDSN())) // includes server/account/flags; never log it or DSN.
	businessTemplates.Lock()
	if businessTemplates.entries == nil {
		businessTemplates.entries = map[[32]byte]*mysqlBusinessTemplateEntry{}
	}
	entry := businessTemplates.entries[key]
	if entry == nil {
		entry = &mysqlBusinessTemplateEntry{}
		businessTemplates.entries[key] = entry
	}
	businessTemplates.Unlock()
	entry.once.Do(func() { entry.snapshot, entry.err = buildMySQLBusinessTemplate(cfg) })
	return entry.snapshot, entry.err
}
func buildMySQLBusinessTemplate(cfg *mysqlconfig.Config) (snapshot *mysqlBusinessSnapshot, err error) {
	source, err := openMySQLBusinessDatabase(cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, source.close())
		if err != nil {
			snapshot = nil
		}
	}()
	// Exactly the existing handler fixture's seven schema-only preparation steps.
	for _, prepare := range []func(*gorm.DB) error{datastore.RunMigrations, datastore.ReconcileCommerceSchema, datastore.ReconcileSubscriptionAccessSchema, datastore.ReconcileZeroEventSchema, datastore.ReconcileTrafficReadSchema, datastore.ReconcileOperationsSchema, datastore.ReconcileFairUseTelemetrySchema} {
		if err := prepare(source.DB); err != nil {
			return nil, err
		}
	}
	return captureMySQLBusinessSnapshot(source.DB)
}
func newMySQLBusinessHandlers(t *testing.T) (*handlers, string) {
	t.Helper()
	cfg := mysqlBusinessConfig(t)
	snapshot, err := mysqlBusinessTemplate(cfg)
	if err != nil {
		t.Fatal("build immutable business template", err)
	}
	target, err := openMySQLBusinessDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := target.close(); err != nil {
			t.Error("clean isolated business schema", err)
		}
	})
	if err := snapshot.install(target.DB); err != nil {
		t.Fatal("install business schema (not migrations)", err)
	}
	h, err := newTestHandlers(target.DB, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.CloseBackgroundJobs)
	return h, target.DSN
}

type mysqlBusinessSettings struct{ ServerUUID, Version, SQLMode, TimeZone, Charset, Collation string }

func businessSettings(db *gorm.DB) (out mysqlBusinessSettings, err error) {
	var checks int
	err = db.Raw("SELECT @@server_uuid,@@version,@@SESSION.sql_mode,@@SESSION.time_zone,@@character_set_database,@@collation_database,@@SESSION.foreign_key_checks").Row().Scan(&out.ServerUUID, &out.Version, &out.SQLMode, &out.TimeZone, &out.Charset, &out.Collation, &checks)
	if err == nil && checks != 1 {
		err = errors.New("business fixture requires FOREIGN_KEY_CHECKS=1; no setting will be changed")
	}
	return
}

type mysqlBusinessTable struct {
	Name, DDL     string
	Columns       []string
	Rows          [][]any
	SelfReference bool
	ColumnSchema  []mysqlBusinessColumn
}
type mysqlBusinessColumn struct {
	Name, Type, Nullable                 string
	Default                              *string
	Extra, Charset, Collation, Generated string
}
type mysqlBusinessSnapshot struct {
	Settings mysqlBusinessSettings
	Tables   []mysqlBusinessTable
	Triggers []string
}

var businessSeedTables = map[string]bool{"schema_migrations": true, "system_configs": true, "email_templates": true, "subscription_templates": true, "menu_nodes": true, "menu_revisions": true, "job_execution_budget": true, "job_execution_groups": true, "automatic_renewal_scans": true}

func captureMySQLBusinessSnapshot(db *gorm.DB) (snapshot *mysqlBusinessSnapshot, err error) {
	snapshot = &mysqlBusinessSnapshot{}
	snapshot.Settings, err = businessSettings(db)
	if err != nil {
		return nil, err
	}
	// MigrationTables intentionally omits schema history and the derived hourly
	// table. Include these two explicitly; a future unrecognized object must fail.
	expected, err := datastore.MigrationTables(db)
	if err != nil {
		return nil, err
	}
	expected = append(expected, "schema_migrations", "traffic_usage_hourly")
	allowed := map[string]bool{}
	for _, name := range expected {
		allowed[name] = true
	}
	rows, err := db.Raw("SELECT TABLE_NAME,TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME").Rows()
	if err != nil {
		return nil, err
	}
	tables := map[string]mysqlBusinessTable{}
	for rows.Next() {
		var name, kind string
		if err = rows.Scan(&name, &kind); err != nil {
			break
		}
		if kind != "BASE TABLE" || !businessIdentifier.MatchString(name) || !allowed[name] {
			err = fmt.Errorf("unsupported business template object %s (%s)", name, kind)
			break
		}
		tables[name] = mysqlBusinessTable{Name: name}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	if len(tables) != len(allowed) {
		return nil, errors.New("business template table inventory differs from complete fixture")
	}
	// Current MySQL preparation redirects0024/0025 to the application reconciler,
	// which explicitly drops the four traffic_hourly triggers. Verify the actual
	// set rather than assuming that published CREATE TRIGGER statements ran.
	objectQueries := []struct{ kind, query string }{
		{"trigger", "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() ORDER BY TRIGGER_NAME"},
		{"routine", "SELECT ROUTINE_NAME FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE() ORDER BY ROUTINE_NAME"},
		{"event", "SELECT EVENT_NAME FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE() ORDER BY EVENT_NAME"},
	}
	for _, q := range objectQueries {
		rows, e := db.Raw(q.query).Rows()
		if e != nil {
			return nil, e
		}
		var names []string
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				break
			}
			names = append(names, name)
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return nil, e
		}
		if q.kind == "trigger" {
			snapshot.Triggers = append([]string(nil), names...)
		}
		// Supporting a DEFINER/SQL-mode-dependent body would expand this small
		// helper into a dump/restore engine. Fail explicitly instead of omitting it.
		if len(names) != 0 {
			return nil, fmt.Errorf("unsupported business template %s objects", q.kind)
		}
	}
	parents := map[string]map[string]bool{}
	selfReferences := map[string]bool{}
	for name := range tables {
		parents[name] = map[string]bool{}
	}
	rows, err = db.Raw("SELECT TABLE_NAME,REFERENCED_TABLE_SCHEMA,REFERENCED_TABLE_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY TABLE_NAME,CONSTRAINT_NAME,ORDINAL_POSITION").Rows()
	if err != nil {
		return nil, err
	}
	var database string
	// Consume and close metadata before querying database identity; this also
	// works with a single-connection pool. Never inspect another schema.
	type reference struct{ child, schema, parent string }
	var refs []reference
	for rows.Next() {
		var ref reference
		if err = rows.Scan(&ref.child, &ref.schema, &ref.parent); err != nil {
			break
		}
		refs = append(refs, ref)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	if err = db.Raw("SELECT DATABASE()").Scan(&database).Error; err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if ref.schema != database || parents[ref.child] == nil || parents[ref.parent] == nil {
			return nil, errors.New("cross-schema/unknown foreign key in business template")
		}
		// MySQL creates an empty self-referencing table with its complete FK
		// enabled in the same CREATE. It is not a cross-table dependency.
		// Below we refuse populated self-reference seeds, whose row order
		// would require a separate proven restoration algorithm.
		if ref.child == ref.parent {
			selfReferences[ref.child] = true
			continue
		}
		parents[ref.child][ref.parent] = true
	}
	order, err := businessTableOrder(parents)
	if err != nil {
		return nil, err
	}
	for _, name := range order {
		table := tables[name]
		rows, e := db.Raw("SHOW CREATE TABLE " + businessQuote(name)).Rows()
		if e != nil {
			return nil, e
		}
		var returned string
		if !rows.Next() {
			e = errors.New("missing SHOW CREATE row")
		} else {
			e = rows.Scan(&returned, &table.DDL)
		}
		if rows.Next() && e == nil {
			e = errors.New("extra SHOW CREATE rows")
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return nil, e
		}
		// No SQL rewriting, CREATE LIKE or qualified template-schema substitutions.
		// Complete SHOW CREATE preserves CHECKs/FKs/indexes/options/AUTO_INCREMENT.
		if returned != name || !strings.HasPrefix(table.DDL, "CREATE TABLE "+businessQuote(name)+" (") || !strings.Contains(table.DDL, "ENGINE=InnoDB") || strings.Contains(table.DDL, businessQuote(database)+".") {
			return nil, fmt.Errorf("unsupported SHOW CREATE shape for %s", name)
		}
		columns, e := db.Raw("SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,EXTRA,COALESCE(CHARACTER_SET_NAME,''),COALESCE(COLLATION_NAME,''),COALESCE(GENERATION_EXPRESSION,'') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION", name).Rows()
		if e != nil {
			return nil, e
		}
		for columns.Next() {
			var c mysqlBusinessColumn
			if e = columns.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default, &c.Extra, &c.Charset, &c.Collation, &c.Generated); e != nil {
				break
			}
			table.ColumnSchema = append(table.ColumnSchema, c)
		}
		e = errors.Join(e, columns.Err(), columns.Close())
		if e != nil {
			return nil, e
		}
		table.Columns, table.Rows, e = captureBusinessSeeds(db, name)
		if e != nil {
			return nil, e
		}
		table.SelfReference = selfReferences[name]
		if table.SelfReference && len(table.Rows) != 0 {
			return nil, fmt.Errorf("unsupported populated self-reference seeds in %s", name)
		}
		snapshot.Tables = append(snapshot.Tables, table)
	}
	return snapshot, nil
}

// Comparison only; install always executes original SHOW CREATE verbatim.
// MySQL may add redundant CHARACTER SET when parsing a column COLLATE. The
// physical column metadata is ALSO compared exactly; only this direct, known
// column/type/collation prefix is made explicit. Defaults/CHECKs/expressions,
// FK/index definitions, AUTO_INCREMENT and table options are never rewritten.
func businessComparableSnapshot(source *mysqlBusinessSnapshot) *mysqlBusinessSnapshot {
	if source == nil {
		return nil
	}
	out := *source
	out.Tables = append([]mysqlBusinessTable(nil), source.Tables...)
	for i, table := range out.Tables {
		lines := strings.Split(table.DDL, "\n")
		for _, c := range table.ColumnSchema {
			if c.Charset == "" || c.Collation == "" {
				continue
			}
			prefix := "  " + businessQuote(c.Name) + " " + c.Type + " COLLATE " + c.Collation
			for j, line := range lines {
				if strings.HasPrefix(line, prefix) && (len(line) == len(prefix) || line[len(prefix)] == ' ') {
					lines[j] = "  " + businessQuote(c.Name) + " " + c.Type + " CHARACTER SET " + c.Charset + " COLLATE " + c.Collation + line[len(prefix):]
				}
			}
		}
		out.Tables[i].DDL = strings.Join(lines, "\n")
	}
	return &out
}
func businessTableOrder(parents map[string]map[string]bool) ([]string, error) {
	pending := map[string]map[string]bool{}
	for table, deps := range parents {
		for parent := range deps {
			if _, ok := parents[parent]; !ok {
				return nil, errors.New("business template contains an unknown foreign-key parent")
			}
		}
		pending[table] = deps
	}
	var order []string
	for len(pending) > 0 {
		var ready []string
		for table, deps := range pending {
			blocked := false
			for parent := range deps {
				if _, ok := pending[parent]; ok {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = append(ready, table)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("business template contains a foreign-key cycle")
		}
		sort.Strings(ready)
		for _, table := range ready {
			order = append(order, table)
			delete(pending, table)
		}
	}
	return order, nil
}
func captureBusinessSeeds(db *gorm.DB, table string) (columns []string, data [][]any, err error) {
	rows, err := db.Raw("SELECT * FROM " + businessQuote(table) + " LIMIT 1025").Rows()
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	columns, err = rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		if !businessSeedTables[table] {
			return nil, nil, fmt.Errorf("business template contains non-initialization rows in %s", table)
		}
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, nil, err
		}
		for i, value := range values {
			if raw, ok := value.([]byte); ok {
				kind := strings.ToUpper(types[i].DatabaseTypeName())
				if strings.Contains(kind, "BLOB") || kind == "BINARY" || kind == "VARBINARY" || kind == "BIT" {
					values[i] = append([]byte(nil), raw...)
				} else {
					values[i] = string(raw)
				}
			}
		}
		data = append(data, values)
		if len(data) > 1024 {
			return nil, nil, errors.New("business initialization seed exceeds bounded capture")
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	keys := make(map[int]string, len(data))
	for i, row := range data {
		raw, e := json.Marshal(row)
		if e != nil {
			return nil, nil, e
		}
		keys[i] = string(raw)
	}
	type seedRow struct {
		key    string
		values []any
	}
	ordered := make([]seedRow, len(data))
	for i, row := range data {
		ordered[i] = seedRow{keys[i], row}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].key < ordered[j].key })
	for i, row := range ordered {
		data[i] = row.values
	}
	return columns, data, nil
}
func (s *mysqlBusinessSnapshot) install(db *gorm.DB) error {
	for _, table := range s.Tables {
		if table.SelfReference && len(table.Rows) != 0 {
			return errors.New("populated self-reference seed restoration is unsupported")
		}
	}
	settings, err := businessSettings(db)
	if err != nil {
		return err
	}
	if settings != s.Settings {
		return errors.New("business template server/session/database settings changed")
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("business template requires a newly empty schema")
	}
	for _, table := range s.Tables {
		if err := db.Exec(table.DDL).Error; err != nil {
			return fmt.Errorf("create business fixture table %s: %w", table.Name, err)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range s.Tables {
			if len(table.Rows) == 0 {
				continue
			}
			quoted := make([]string, len(table.Columns))
			marks := make([]string, len(table.Columns))
			for i, name := range table.Columns {
				quoted[i] = businessQuote(name)
				marks[i] = "?"
			}
			statement := "INSERT INTO " + businessQuote(table.Name) + " (" + strings.Join(quoted, ",") + ") VALUES (" + strings.Join(marks, ",") + ")"
			for _, row := range table.Rows {
				if err := tx.Exec(statement, row...).Error; err != nil {
					return fmt.Errorf("restore initialization seed %s: %w", table.Name, err)
				}
			}
		}
		return nil
	})
}

// Uncached helpers below are used only by the equivalence/cleanup contracts.
func businessDatabaseForTest(t *testing.T, cfg *mysqlconfig.Config) mysqlBusinessDatabase {
	t.Helper()
	db, err := openMySQLBusinessDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func newMySQLBusinessOrderFixture(t *testing.T) orderFixture {
	t.Helper()
	h, _ := newMySQLBusinessHandlers(t)
	user := model.User{ID: 1, Email: "reader@example.test", Password: "unused", Status: userStatusActive}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "Catalog", Code: "catalog", IsEnabled: true}
	if err := h.db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	return newOrderFixtureFromCatalog(t, catalogFixture{h: h, group: group})
}
