package platformstore

import (
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/migrations"
)

func TestMigrationEmbeddedSeedLiteralsMatchPublishedShape(t *testing.T) {
	baseline, err := migrations.Files.ReadFile("0001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		table, end    string
		rows, columns int
	}{
		{"system_configs", "\nINSERT INTO email_templates\n", 18, 8},
		{"email_templates", "\nINSERT INTO subscription_templates\n", 2, 9},
		{"subscription_templates", "\n\nSET FOREIGN_KEY_CHECKS", 3, 8},
	} {
		start := strings.Index(string(baseline), "\nINSERT INTO "+spec.table+"\n")
		remaining := string(baseline)[start+1:]
		end := strings.Index(remaining, spec.end)
		columns, queries, err := migrationSeedSelect(remaining[:end])
		if err != nil || len(queries) != spec.rows || len(columns) != spec.columns {
			t.Fatalf("%s unexpected readonly projection rows=%d cols=%d err=%v", spec.table, len(queries), len(columns), err)
		}
		for _, query := range queries {
			if !strings.HasPrefix(query, "SELECT ") || strings.Contains(query, "UTC_TIMESTAMP") {
				t.Fatal("seed projection executes writes or volatile timestamp")
			}
		}
	}
	policy, err := migrations.Files.ReadFile("0050_subscription_recovery_policy.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	columns, queries, err := migrationSeedSelect(string(policy))
	if err != nil || len(columns) != 8 || len(queries) != 1 {
		t.Fatal("policy readonly projection failed", err)
	}
}

func TestMigrationEmbeddedSeedsRejectUnexpectedSQL(t *testing.T) {
	for _, statement := range []string{
		"INSERT INTO system_configs(value) VALUES('seed'); DELETE FROM users;",
		"INSERT INTO system_configs(value) VALUES((SELECT value FROM users));",
		"INSERT INTO system_configs(value) VALUES(CONCAT('operator', 'value'));",
		"INSERT INTO system_configs(value) VALUES('seed') ON DUPLICATE KEY UPDATE value='changed';",
		"INSERT INTO system_configs(value) VALUES('seed'),;",
		"INSERT INTO system_configs(value) VALUES('unterminated);",
		"INSERT INTO system_configs(value) VALUES('seed', 1);",
		"INSERT INTO system_configs(value) VALUES('seed') UNION SELECT 'extra';",
		"INSERT INTO system_configs(created_at) VALUES(SLEEP(1));",
		"INSERT INTO users(value) VALUES('seed');",
		"INSERT INTO system_configs(value) SELECT 'seed';",
	} {
		if _, _, err := migrationSeedSelect(statement); err == nil {
			t.Errorf("accepted unsupported embedded SQL: %s", statement)
		}
	}
	columns, queries, err := migrationSeedSelect("INSERT INTO email_templates(name,revision,created_at) VALUES('escaped \\' quote, (with) comma',1,UTC_TIMESTAMP(3)),('doubled '' quote',2,NOW(3));")
	if err != nil || len(columns) != 2 || len(queries) != 2 {
		t.Fatal("valid escaped literal rejected", err)
	}
}
