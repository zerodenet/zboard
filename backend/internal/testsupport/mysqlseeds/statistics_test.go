package mysqlseeds

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBusinessOwnedSingletonSeedsRequireExactRowsAndColumns(t *testing.T) {
	at := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	cases := []struct {
		table string
		row   []any
	}{{"node_observation_material_budget", []any{int64(1), int64(0), int64(1)}}, {"principal_statistics_state", []any{int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(1), at, at}}, {"flow_start_statistics_state", []any{int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(1), at, at}}}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			columns := SingletonColumns(c.table)
			data := [][]any{append([]any(nil), c.row...)}
			if err := Validate(c.table, columns, data); err != nil {
				t.Fatal("genuine initializer rejected", err)
			}
			before, _ := json.Marshal(data)
			for _, bad := range [][][]any{nil, {}, {c.row, c.row}} {
				if err := Validate(c.table, columns, bad); err == nil {
					t.Fatal("absent/duplicate row accepted")
				}
			}
			for i, name := range columns {
				changed := append([]any(nil), c.row...)
				switch name {
				case "created_at":
					changed[i] = time.Time{}
				case "updated_at":
					changed[i] = at.Add(time.Millisecond)
				default:
					changed[i] = int64(99)
				}
				if err := Validate(c.table, columns, [][]any{changed}); err == nil {
					t.Fatal("active/modified singleton accepted", name)
				}
			}
			unknown := append([]string(nil), columns...)
			unknown[0] = "unowned_column"
			if err := Validate(c.table, unknown, data); err == nil {
				t.Fatal("unknown column accepted")
			}
			missing := columns[:len(columns)-1]
			if err := Validate(c.table, missing, data); err == nil {
				t.Fatal("missing column accepted")
			}
			duplicate := append([]string(nil), columns...)
			duplicate[1] = duplicate[0]
			if err := Validate(c.table, duplicate, data); err == nil {
				t.Fatal("duplicate column admitted")
			}
			after, _ := json.Marshal(data)
			if string(before) != string(after) {
				t.Fatal("seed validation mutated captured rows")
			}
		})
	}
}
func TestBusinessSeedUintRefusesLossyAndNoncanonicalValues(t *testing.T) {
	for _, v := range []any{float64(1), int64(-1), "01", "+1", "1.0", true, nil} {
		if _, ok := seedUint(v); ok {
			t.Fatal("unproven unsigned value accepted", v)
		}
	}
	for _, v := range []any{uint64(1), int64(1), int(1), uint(1), "1", []byte("1")} {
		if n, ok := seedUint(v); !ok || n != 1 {
			t.Fatal("actual driver integer representation rejected", v)
		}
	}
}

func TestUnknownFamilyCannotAcquireSingletonSeedOwnership(t *testing.T) {
	if SingletonColumns("users") != nil {
		t.Fatal("unknown family acquired strict seed ownership")
	}
	if err := Validate("users", []string{"id"}, [][]any{{int64(1)}}); err == nil {
		t.Fatal("unowned row accepted")
	}
	if err := ValidateColumns("users", []string{"id"}); err == nil {
		t.Fatal("unowned columns accepted")
	}
}
