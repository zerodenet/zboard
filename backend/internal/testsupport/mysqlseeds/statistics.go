// Package mysqlseeds validates only migration-owned initialization rows for
// isolated MySQL test templates. Production does not import this test support.
package mysqlseeds

import (
	"fmt"
	"strconv"
	"time"
)

// These exact migration-owned singleton rows are not table-wide seed permission.
// Validate the values actually captured, never an earlier unlocked pre-query.
func SingletonColumns(table string) []string {
	switch table {
	case "node_observation_material_budget":
		return []string{"id", "retained_bytes", "revision"}
	case "principal_statistics_state", "flow_start_statistics_state":
		return []string{"id", "legacy_upper_id", "legacy_after_id", "legacy_expected_rows", "legacy_projected_rows", "projected_head", "revision", "created_at", "updated_at"}
	}
	return nil
}
func ValidateColumns(table string, columns []string) error {
	expected := SingletonColumns(table)
	if expected == nil {
		return fmt.Errorf("table %s is not an owned singleton seed family", table)
	}
	if len(expected) != len(columns) {
		return fmt.Errorf("business template initialization columns differ in %s", table)
	}
	wanted := map[string]bool{}
	for _, name := range expected {
		wanted[name] = true
	}
	for _, name := range columns {
		if !wanted[name] {
			return fmt.Errorf("business template initialization columns differ in %s", table)
		}
		delete(wanted, name)
	}
	return nil
}
func Validate(table string, columns []string, data [][]any) error {
	if SingletonColumns(table) == nil {
		return fmt.Errorf("table %s is not an owned singleton seed family", table)
	}
	if err := ValidateColumns(table, columns); err != nil {
		return err
	}
	reject := func() error {
		return fmt.Errorf("business template contains changed or non-initialization singleton in %s", table)
	}
	if len(data) != 1 || len(data[0]) != len(columns) {
		return reject()
	}
	row := map[string]any{}
	for i, name := range columns {
		row[name] = data[0][i]
	}
	for _, name := range SingletonColumns(table) {
		if name == "created_at" || name == "updated_at" {
			continue
		}
		expected := uint64(0)
		if name == "id" || name == "revision" {
			expected = 1
		}
		actual, ok := seedUint(row[name])
		if !ok || actual != expected {
			return reject()
		}
	}
	if table == "principal_statistics_state" || table == "flow_start_statistics_state" {
		created, ok := row["created_at"].(time.Time)
		updated, ok2 := row["updated_at"].(time.Time)
		if !ok || !ok2 || created.IsZero() || !created.Equal(updated) {
			return reject()
		}
	}
	return nil
}
func seedUint(value any) (uint64, bool) {
	switch n := value.(type) {
	case uint64:
		return n, true
	case int64:
		if n >= 0 {
			return uint64(n), true
		}
	case int:
		if n >= 0 {
			return uint64(n), true
		}
	case uint:
		return uint64(n), true
	case string:
		parsed, err := strconv.ParseUint(n, 10, 64)
		if err == nil && strconv.FormatUint(parsed, 10) == n {
			return parsed, true
		}
	case []byte:
		return seedUint(string(n))
	}
	return 0, false
}
