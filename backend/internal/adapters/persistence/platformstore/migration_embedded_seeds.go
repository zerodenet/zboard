package platformstore

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

// Read immutable migration literals through the destination SQL engine. This
// avoids copying default content or granting temporary-table/write privileges.
// Only logical seed columns are compared here; the sealed manifest additionally
// pins generated identifiers and timestamps.
func matchesMySQLMigrationSeeds(db *gorm.DB) (bool, error) {
	baseline, err := migrations.Files.ReadFile("0001_init.up.sql")
	if err != nil {
		return false, err
	}
	policy, err := migrations.Files.ReadFile("0050_subscription_recovery_policy.up.sql")
	if err != nil {
		return false, err
	}
	for _, spec := range []struct{ table, end string }{
		{"system_configs", "\nINSERT INTO email_templates\n"},
		{"email_templates", "\nINSERT INTO subscription_templates\n"},
		{"subscription_templates", "\n\nSET FOREIGN_KEY_CHECKS"},
	} {
		marker := "\nINSERT INTO " + spec.table + "\n"
		start := strings.Index(string(baseline), marker)
		if start < 0 {
			return false, fmt.Errorf("missing embedded initialization %s", spec.table)
		}
		remaining := string(baseline)[start+1:]
		end := strings.Index(remaining, spec.end)
		if end < 0 {
			return false, fmt.Errorf("missing embedded initialization boundary %s", spec.table)
		}
		columns, expressions, err := migrationSeedSelect(remaining[:end])
		if err != nil {
			return false, err
		}
		if spec.table == "system_configs" {
			policyColumns, policyRows, err := migrationSeedSelect(string(policy))
			if err != nil {
				return false, err
			}
			if strings.Join(columns, ",") != strings.Join(policyColumns, ",") {
				return false, fmt.Errorf("embedded config seed columns differ")
			}
			// 0092 updates only the known initializer description.
			for _, row := range policyRows {
				expressions = append(expressions, strings.ReplaceAll(row, "到期后停服，可在此天数内续费恢复；默认7天，范围0–3650，0表示不保留恢复窗口。缩短窗口会使超过新期限的旧订阅结束并清理凭据，历史订单和账务保留。主动退订不进入恢复窗口。", datastore.SubscriptionCleanupDescription))
			}
		}
		actual, err := migrationSeedRows(db, "SELECT "+strings.Join(columns, ",")+" FROM "+migrationQuote(db, spec.table)+" LIMIT 1025", columns)
		if err != nil {
			return false, err
		}
		expected, err := migrationSeedRows(db, strings.Join(expressions, " UNION ALL "), columns)
		if err != nil {
			return false, err
		}
		if spec.table == "system_configs" {
			for _, seed := range datastore.CredentialMaterialDefaults() {
				values := map[string]any{"config_key": seed.ConfigKey, "name": seed.Name, "value": seed.Value, "value_type": seed.ValueType, "description": seed.Description, "revision": seed.Revision, "is_public": 0, "is_secret": 0}
				if seed.IsPublic {
					values["is_public"] = 1
				}
				if seed.IsSecret {
					values["is_secret"] = 1
				}
				parts := make([]string, 0, len(columns))
				args := make([]any, 0, len(columns))
				for _, column := range columns {
					parts = append(parts, "? AS "+migrationQuote(db, column))
					args = append(args, values[column])
				}
				rows, e := migrationSeedRows(db, "SELECT "+strings.Join(parts, ","), columns, args...)
				if e != nil {
					return false, e
				}
				expected = append(expected, rows...)
			}
			sort.Strings(expected)
		}
		if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
			return false, nil
		}
	}
	return true, nil
}

func migrationSeedSelect(statement string) ([]string, []string, error) {
	statement = strings.TrimSpace(statement)
	if !regexp.MustCompile(`^INSERT INTO (system_configs|email_templates|subscription_templates)\s*\(`).MatchString(statement) {
		return nil, nil, fmt.Errorf("unsupported embedded seed statement")
	}
	open := strings.IndexByte(statement, '(')
	if open < 0 {
		return nil, nil, fmt.Errorf("missing seed columns")
	}
	close := strings.IndexByte(statement[open:], ')') + open
	if close < open {
		return nil, nil, fmt.Errorf("invalid seed columns")
	}
	columns := strings.Split(statement[open+1:close], ",")
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
		if !regexp.MustCompile(`^[a-z_]+$`).MatchString(columns[i]) {
			return nil, nil, fmt.Errorf("unsupported embedded seed column")
		}
	}
	values := strings.TrimSpace(statement[close+1:])
	if !strings.HasPrefix(values, "VALUES") {
		return nil, nil, fmt.Errorf("missing seed values")
	}
	values = strings.TrimSpace(values[len("VALUES"):])
	selected := []string{}
	indices := []int{}
	hasRevision := false
	for i, col := range columns {
		if col == "created_at" || col == "updated_at" || col == "id" {
			continue
		}
		selected = append(selected, col)
		indices = append(indices, i)
		hasRevision = hasRevision || col == "revision"
	}
	if !hasRevision {
		selected = append(selected, "revision")
	}
	queries := []string{}
	for strings.HasPrefix(values, "(") {
		parts, consumed, err := migrationSeedTuple(values)
		if err != nil {
			return nil, nil, err
		}
		if len(parts) != len(columns) {
			return nil, nil, fmt.Errorf("embedded seed arity differs")
		}
		for i, expression := range parts {
			if columns[i] == "created_at" || columns[i] == "updated_at" {
				if expression != "UTC_TIMESTAMP(3)" && expression != "NOW(3)" {
					return nil, nil, fmt.Errorf("unsupported embedded seed timestamp")
				}
			} else if !regexp.MustCompile(`^(NULL|-?[0-9]+|'([^'\\]|\\.|'')*')$`).MatchString(expression) {
				return nil, nil, fmt.Errorf("unsupported embedded seed expression")
			}
		}
		expressions := []string{}
		for _, i := range indices {
			expressions = append(expressions, parts[i])
		}
		if !hasRevision {
			expressions = append(expressions, "1")
		}
		queries = append(queries, "SELECT "+strings.Join(expressions, ","))
		values = strings.TrimSpace(values[consumed:])
		if strings.HasPrefix(values, ",") {
			values = strings.TrimSpace(values[1:])
			if !strings.HasPrefix(values, "(") {
				return nil, nil, fmt.Errorf("missing embedded seed tuple after comma")
			}
		} else {
			break
		}
	}
	if values != ";" && values != "" && !(strings.HasPrefix(statement, "INSERT INTO system_configs(") && values == "ON DUPLICATE KEY UPDATE config_key=VALUES(config_key);") {
		return nil, nil, fmt.Errorf("unsupported embedded seed suffix")
	}
	if len(queries) == 0 {
		return nil, nil, fmt.Errorf("missing embedded seed tuples")
	}
	return selected, queries, nil
}

// Split only trusted embedded SQL, preserving string escapes and nested calls.
func migrationSeedTuple(sql string) ([]string, int, error) {
	depth, start := 1, 1
	quoted := false
	parts := []string{}
	for i := 1; i < len(sql); i++ {
		c := sql[i]
		if quoted {
			if c == '\\' {
				i++
				continue
			}
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					continue
				}
				quoted = false
			}
			continue
		}
		switch c {
		case '\'':
			quoted = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return append(parts, strings.TrimSpace(sql[start:i])), i + 1, nil
			}
		case ',':
			if depth == 1 {
				parts = append(parts, strings.TrimSpace(sql[start:i]))
				start = i + 1
			}
		}
	}
	return nil, 0, fmt.Errorf("unterminated embedded seed tuple")
}

func migrationSeedRows(db *gorm.DB, query string, columns []string, args ...any) ([]string, error) {
	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		values, ptrs := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, value := range values {
			if value == nil {
				continue
			}
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			text := fmt.Sprint(value)
			if columns[i] == "customization" {
				var document any
				decoder := json.NewDecoder(strings.NewReader(text))
				decoder.UseNumber()
				if err := decoder.Decode(&document); err != nil {
					return nil, err
				}
				canonical, err := json.Marshal(document)
				if err != nil {
					return nil, err
				}
				text = string(canonical)
			}
			values[i] = text
		}
		payload, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		result = append(result, string(payload))
	}
	sort.Strings(result)
	return result, rows.Err()
}
