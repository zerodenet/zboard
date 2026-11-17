package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Pure diagnostics contract; no real MySQL result is implied by this unit test.
func TestFixtureTimingAggregationAndSafeOutput(t *testing.T) {
	var ledger mysqlFixtureTimingLedger
	ledger.record("publish", "create_schema", 10*time.Millisecond, nil)
	ledger.record("publish", "create_schema", 3*time.Millisecond, errors.New("private credential must never be serialized"))
	ledger.record("raw-private-identity", "create_schema", time.Second, nil)
	ledger.record("publish", "unknown-private-sql", time.Second, nil)
	rows := ledger.snapshot()
	if len(rows) != 1 || rows[0].Samples != 2 || rows[0].Errors != 1 || rows[0].WallTotalNS != int64(13*time.Millisecond) || rows[0].WallMaxNS != int64(10*time.Millisecond) {
		t.Fatal("aggregate changed", rows)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private") {
		t.Fatal("diagnostic output leaked a key or error value")
	}
	for _, tc := range []struct {
		failed bool
		optIn  string
		want   bool
	}{{false, "", false}, {false, "1", true}, {true, "", true}, {true, "0", true}, {false, "true", false}} {
		if shouldReportMySQLFixtureTiming(tc.failed, tc.optIn) != tc.want {
			t.Fatal("default diagnostics output policy changed")
		}
	}
}
