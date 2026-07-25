package datastore

import (
	"regexp"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/migrations"
)

func TestSplitMigrationStatements(t *testing.T) {
	source := `
-- a leading comment
CREATE TABLE demo (value VARCHAR(32));
INSERT INTO demo (value) VALUES ('a;b'), ("c;d");
/* a block ; comment */
UPDATE demo SET value = 'it''s safe'; # trailing comment
`
	statements, err := splitMigrationStatements(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("got %d statements: %#v", len(statements), statements)
	}
}

func TestSplitMigrationStatementsRejectsUnterminatedInput(t *testing.T) {
	if _, err := splitMigrationStatements("SELECT 'broken;"); err == nil {
		t.Fatal("expected unterminated quote error")
	}
}

func TestEveryEmbeddedUpMigrationParses(t *testing.T) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		payload, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		statements, err := splitMigrationStatements(string(payload))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if len(statements) == 0 {
			t.Fatalf("%s contains no statements", entry.Name())
		}
	}
}

func TestPreReleaseMigrationInventoryIsSquashed(t *testing.T) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var up, down []string
	for _, entry := range entries {
		switch {
		case entry.IsDir():
		case strings.HasSuffix(entry.Name(), ".up.sql"):
			up = append(up, entry.Name())
		case strings.HasSuffix(entry.Name(), ".down.sql"):
			down = append(down, entry.Name())
		}
	}
	if len(up) != 1 || up[0] != preReleaseBaselineVersion {
		t.Fatalf("up migrations = %v, want only %s", up, preReleaseBaselineVersion)
	}
	if len(down) != 1 || down[0] != "0001_init.down.sql" {
		t.Fatalf("down migrations = %v, want only 0001_init.down.sql", down)
	}
	if err := validateMigrationInventory(up); err != nil {
		t.Fatal(err)
	}

	payload, err := migrations.Files.ReadFile(preReleaseBaselineVersion)
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	if count := strings.Count(source, "CREATE TABLE "); count != len(preReleaseBaselineTables) {
		t.Fatalf("baseline creates %d tables, want %d", count, len(preReleaseBaselineTables))
	}
	for _, table := range preReleaseBaselineTables {
		if !strings.Contains(source, "CREATE TABLE `"+table+"`") {
			t.Errorf("baseline is missing table %s", table)
		}
	}
	for _, key := range []string{
		"'register_switch'",
		"'smtp_password'",
		"'znet-sink'",
		"'clash'",
		"'sing-box'",
		`"version":2`,
		`"policy_groups"`,
		`"main_group":"main"`,
	} {
		if !strings.Contains(source, key) {
			t.Errorf("baseline is missing seed contract %s", key)
		}
	}
	for _, obsolete := range []string{
		"ALTER TABLE",
		"migration_0014",
		`"profile"`,
		`"group_name"`,
		"'balanced'",
		"plan_protocol_endpoints",
		"access_groups",
	} {
		if strings.Contains(source, obsolete) {
			t.Errorf("baseline retains obsolete development artifact %q", obsolete)
		}
	}
	if regexp.MustCompile(`AUTO_INCREMENT=[0-9]+`).MatchString(source) {
		t.Fatal("baseline contains environment-specific AUTO_INCREMENT counters")
	}

	downPayload, err := migrations.Files.ReadFile("0001_init.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downStatements, err := splitMigrationStatements(string(downPayload))
	if err != nil {
		t.Fatal(err)
	}
	if len(downStatements) != 3 {
		t.Fatalf("down baseline has %d statements, want foreign-key guard, drop, restore", len(downStatements))
	}
}

func TestValidateMigrationInventoryRequiresInitialBaseline(t *testing.T) {
	for name, versions := range map[string][]string{
		"empty": nil,
		"wrong first": {
			"0002_example.up.sql",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateMigrationInventory(versions); err == nil {
				t.Fatalf("validateMigrationInventory(%v) succeeded", versions)
			}
		})
	}
	if err := validateMigrationInventory([]string{preReleaseBaselineVersion}); err != nil {
		t.Fatal(err)
	}
}
