package datastore

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// Table-copy contract tests: these small SQLite schemas are not substitutes for
// the separately tested production migration/copier path.
func TestCopyPrincipalLookupPreservesSourceAndInvalidatesOnlyKnownMissing(t *testing.T) {
	for _, tc := range []struct {
		name         string
		missing      []string
		mixedVersion bool
	}{
		{name: "complete_preserves_v0_v1_v2_and_empty_digest"},
		{name: "missing_secret_digest", missing: []string{"secret_principal_digest"}},
		{name: "missing_principal_digest_mixed_version_key", missing: []string{"principal_digest"}, mixedVersion: true},
		{name: "missing_version_explicit_unknown", missing: []string{"principal_lookup_version"}},
		{name: "both_digests_missing", missing: []string{"principal_digest", "secret_principal_digest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, target := principalCopyTableDatabases(t)
			absent := map[string]bool{}
			for _, name := range tc.missing {
				absent[name] = true
			}
			version := "principal_lookup_version"
			if tc.mixedVersion {
				version = "PrInCiPaL_LoOkUp_VeRsIoN"
			}
			principalCopyTableSchema(t, source, absent, version)
			principalCopyTableSchema(t, target, nil, "principal_lookup_version")
			rows := make([]map[string]any, 201)
			for i := range rows {
				id := int64(i + 1)
				row := map[string]any{"id": id, "principal_key": fmt.Sprintf("stable-key-%d", id), "secret": fmt.Sprintf("opaque-ciphertext-%d", id), "retired_principal_hash": "", "payload": []byte{0, 0xff, 0x80, byte(i)}}
				if !absent["principal_digest"] {
					row["principal_digest"] = fmt.Sprintf("stored-digest-%d", id)
				}
				if !absent["secret_principal_digest"] {
					row["secret_principal_digest"] = ""
				}
				if !absent["principal_lookup_version"] {
					row[version] = id % 3
				}
				rows[i] = row
			}
			if err := source.Table("protocol_credentials").CreateInBatches(&rows, 100).Error; err != nil {
				t.Fatal(err)
			}
			columns := principalCopyActualColumns(t, source)
			before := principalCopySourceRows(t, source)
			inserts := 0
			versionInEveryInsert := true
			const callback = "test:principal_copy_insert"
			if err := target.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "protocol_credentials" && tx.Error == nil {
					inserts++
					versionInEveryInsert = versionInEveryInsert && strings.Contains(strings.ToLower(tx.Statement.SQL.String()), "principal_lookup_version")
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer target.Callback().Create().Remove(callback)
			if err := target.Transaction(func(tx *gorm.DB) error { return copyTableRows(source, tx, "protocol_credentials") }); err != nil {
				t.Fatal(err)
			}
			if inserts != 2 || !versionInEveryInsert {
				t.Fatalf("201rows did not use exactly2 INSERTs with an explicit version: %d/%t", inserts, versionInEveryInsert)
			}
			if !reflect.DeepEqual(columns, principalCopyActualColumns(t, source)) || !reflect.DeepEqual(before, principalCopySourceRows(t, source)) {
				t.Fatal("copy mutated source schema or stored values")
			}
			var copied []struct {
				ID                                                                                 int64
				PrincipalKey, Secret, RetiredPrincipalHash, PrincipalDigest, SecretPrincipalDigest string
				Payload                                                                            []byte
				Version                                                                            int64
				Storage                                                                            string
			}
			if err := target.Table("protocol_credentials").Select("*,principal_lookup_version AS version,typeof(payload) AS storage").Order("id").Find(&copied).Error; err != nil {
				t.Fatal(err)
			}
			if len(copied) != 201 {
				t.Fatal("copied row count changed")
			}
			for i, row := range copied {
				id := int64(i + 1)
				want := id % 3
				if len(tc.missing) > 0 {
					want = 0
				}
				if row.ID != id || row.Version != want {
					t.Fatalf("copied id/version=%d/%d, want%d/%d", row.ID, row.Version, id, want)
				}
				if row.PrincipalKey != fmt.Sprintf("stable-key-%d", id) || row.Secret != fmt.Sprintf("opaque-ciphertext-%d", id) || row.RetiredPrincipalHash != "" || row.Storage != "blob" || !bytes.Equal(row.Payload, []byte{0, 0xff, 0x80, byte(i)}) {
					t.Fatal("canonical text/binary storage changed")
				}
				if !absent["principal_digest"] && row.PrincipalDigest != fmt.Sprintf("stored-digest-%d", id) {
					t.Fatal("present principal digest was rewritten")
				}
				if row.SecretPrincipalDigest != "" {
					t.Fatal("copy invented a secret digest")
				}
			}
		})
	}
}

func TestCopyPrincipalLookupRejectsMissingTargetVersionWithoutSourceWrites(t *testing.T) {
	source, target := principalCopyTableDatabases(t)
	principalCopyTableSchema(t, source, map[string]bool{"principal_lookup_version": true}, "principal_lookup_version")
	principalCopyTableSchema(t, target, map[string]bool{"principal_lookup_version": true}, "principal_lookup_version")
	row := map[string]any{"id": int64(1), "principal_key": "stable-key", "secret": "opaque-ciphertext", "retired_principal_hash": "", "payload": []byte{0, 0xff}, "principal_digest": "stored", "secret_principal_digest": ""}
	if err := source.Table("protocol_credentials").Create(row).Error; err != nil {
		t.Fatal(err)
	}
	before := principalCopySourceRows(t, source)
	err := target.Transaction(func(tx *gorm.DB) error { return copyTableRows(source, tx, "protocol_credentials") })
	if err == nil || !strings.Contains(err.Error(), "write target table protocol_credentials") {
		t.Fatal("unprepared target without lookup version was accepted", err)
	}
	var count int64
	if err := target.Table("protocol_credentials").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("invalid target retained partial rows", err)
	}
	if !reflect.DeepEqual(before, principalCopySourceRows(t, source)) {
		t.Fatal("target failure changed source")
	}
}

func principalCopyTableDatabases(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	open := func(name string) *gorm.DB {
		db, err := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	return open("source"), open("target")
}
func principalCopyTableSchema(t *testing.T, db *gorm.DB, absent map[string]bool, version string) {
	t.Helper()
	ddl := "CREATE TABLE protocol_credentials(id INTEGER PRIMARY KEY,principal_key TEXT NOT NULL,secret TEXT NOT NULL,retired_principal_hash TEXT NOT NULL,payload BLOB NOT NULL"
	for _, c := range []struct{ name, definition string }{{"principal_digest", "TEXT NOT NULL DEFAULT ''"}, {"secret_principal_digest", "TEXT NOT NULL DEFAULT ''"}, {"principal_lookup_version", "INTEGER NOT NULL DEFAULT 0"}} {
		if absent[c.name] {
			continue
		}
		name := c.name
		if name == "principal_lookup_version" {
			name = version
		}
		ddl += "," + name + " " + c.definition
	}
	if err := db.Exec(ddl + ")").Error; err != nil {
		t.Fatal(err)
	}
}
func principalCopyActualColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	rows, err := db.Table("protocol_credentials").Rows()
	if err != nil {
		t.Fatal(err)
	}
	names, err := rows.Columns()
	closeErr := rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return names
}
func principalCopySourceRows(t *testing.T, db *gorm.DB) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := db.Table("protocol_credentials").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}
