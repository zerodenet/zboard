package datastore

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

func TestPreparedProjectionGrantProofRequiresExactDirectScope(t *testing.T) {
	for _, tc := range []struct {
		grant, db string
		want      bool
	}{
		{"GRANT TRIGGER ON `prepared`.* TO `worker`@`%`", "prepared", true},
		{"GRANT SELECT, TRIGGER ON *.* TO `worker`@`%`", "prepared", true},
		{"GRANT ALL PRIVILEGES ON `prepared`.* TO `worker`@`%`", "prepared", true},
		{"GRANT SELECT ON `prepared`.* TO `worker`@`%`", "prepared", false},
		{"GRANT TRIGGER ON `other`.* TO `worker`@`%`", "prepared", false},
		{"GRANT TRIGGER ON `prepared`.`traffic_records` TO `worker`@`%`", "prepared", false},
		{"GRANT `trigger_role`@`%` TO `worker`@`%`", "prepared", false},
		{"GRANT TRIGGER ON `pre%`.* TO `worker`@`%`", "prepared", false},
		{"GRANT TRIGGER ON `pre``pared`.* TO `worker`@`%`", "pre`pared", true},
	} {
		if got := projectionGrantProvesTriggers(tc.grant, tc.db); got != tc.want {
			t.Fatalf("direct grant proof: got %v want %v", got, tc.want)
		}
	}
}
func TestPreparedProjectionMetadataErrorsRemainReadOnly(t *testing.T) {
	const query = "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?"
	for _, fault := range []string{"query", "scan", "rows", "close"} {
		t.Run(fault, func(t *testing.T) {
			conn := &modelColumnConn{fault: fault, columns: []driver.Value{"id"}}
			db, pool := modelColumnTestDB(t, conn)
			_, err := projectionStringRows(db, query, "nodes")
			if err == nil {
				t.Fatal("metadata error treated as complete schema")
			}
			if fault != "scan" && !errors.Is(err, errModelColumnMetadata) {
				t.Fatal("metadata cause lost", err)
			}
			if len(conn.execs) != 0 || pool.Stats().InUse != 0 || (fault != "query" && conn.closeCalls != 1) {
				t.Fatal("metadata validation wrote or retained connection")
			}
		})
	}
	secretCause := errors.New("SHOW GRANTS account private-value")
	err := projectionUnsupported("trigger visibility proof unavailable", secretCause)
	if !errors.Is(err, ErrProjectionValidationUnsupported) || !errors.Is(err, secretCause) || strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "SHOW GRANTS") {
		t.Fatal("unsafe typed error", err)
	}
}

func TestPreparedProjectionGrantProofRejectsRevokesAndUnknownRows(t *testing.T) {
	positive := "GRANT ALL PRIVILEGES ON *.* TO `worker`@`%`"
	revoke := "REVOKE TRIGGER ON `prepared`.* FROM `worker`@`%`"
	for _, rows := range [][]string{
		{positive, revoke}, {revoke, positive},
		{positive, "REVOKE SELECT ON `other`.* FROM `worker`@`%`"},
		{positive, "unrecognized grant visibility constraint"},
		{positive, "GRANT `role`@`%` TO `worker`@`%`"},
		{"GRANT SELECT ON `prepared`.* TO `worker`@`%`"},
	} {
		if projectionGrantSetProvesTriggers(rows, "prepared") {
			t.Fatal("incomplete privilege evidence proved trigger visibility")
		}
	}
	for _, rows := range [][]string{{positive}, {"GRANT USAGE ON *.* TO `worker`@`%`", "GRANT TRIGGER ON `prepared`.* TO `worker`@`%`"}} {
		if !projectionGrantSetProvesTriggers(rows, "prepared") {
			t.Fatal("direct complete grant proof rejected")
		}
	}
}
