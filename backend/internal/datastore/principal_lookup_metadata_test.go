package datastore

import (
	"database/sql/driver"
	"errors"
	"gorm.io/gorm"
	"strings"
	"testing"
)

type principalMetadataDialector struct{ gorm.Dialector }
type principalMetadataMigrator struct{ gorm.Migrator }

func (d principalMetadataDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return principalMetadataMigrator{d.Dialector.Migrator(db)}
}
func (principalMetadataMigrator) HasTable(any) bool { return true }

func TestPrincipalLookupMetadataFailuresNeverInvalidateOrAlter(t *testing.T) {
	for _, mode := range []string{"query", "scan", "rows", "close"} {
		t.Run(mode, func(t *testing.T) {
			conn := &modelColumnConn{fault: mode, columns: []driver.Value{"principal_lookup_version"}}
			db, pool := modelColumnTestDB(t, conn)
			db.Dialector = principalMetadataDialector{db.Dialector}
			err := reconcilePrincipalLookupSchema(db)
			if err == nil || !strings.Contains(err.Error(), "inspect protocol_credentials columns") {
				t.Fatal("metadata error did not stop principal repair", err)
			}
			if mode != "scan" && !errors.Is(err, errModelColumnMetadata) {
				t.Fatal("metadata cause lost", err)
			}
			if mode == "scan" && !strings.Contains(err.Error(), "converting NULL") {
				t.Fatal("metadata Scan cause lost", err)
			}
			if len(conn.execs) != 0 || pool.Stats().InUse != 0 || (mode != "query" && conn.closeCalls != 1) {
				t.Fatal("metadata error wrote state or retained connection")
			}
		})
	}
}
