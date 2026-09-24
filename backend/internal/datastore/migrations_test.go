package datastore

import (
	"regexp"
	"slices"
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

func TestMigrationInventoryRetainsBaselineAndAddsPlugins(t *testing.T) {
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
	expectedUp := []string{
		preReleaseBaselineVersion,
		"0002_plugins.up.sql",
		"0003_external_identities.up.sql",
		"0004_plugin_governance.up.sql",
		"0005_plugin_signing_key.up.sql",
		"0006_node_proxy_pool_subscriptions.up.sql",
		"0007_jobs.up.sql",
		"0008_job_timeouts.up.sql",
		"0009_api_token_expiry.up.sql",
		"0010_registration_events.up.sql",
		"0011_mail_acceptance.up.sql",
		"0012_mail_delivery_attempts.up.sql",
		"0013_integration_invocation_quota.up.sql",
		"0014_job_attempt_retries.up.sql",
		"0015_job_schedule_planning.up.sql",
		"0016_protocol_endpoint_usage_daily.up.sql",
		"0017_protocol_endpoint_egress.up.sql",
		"0018_subscription_mutations.up.sql",
		"0019_subscription_traffic_reset.up.sql",
	}
	if !slices.Equal(up, expectedUp) {
		t.Fatalf("up migrations = %v, want baseline and plugin migration after %s", up, preReleaseBaselineVersion)
	}
	expectedDown := []string{
		"0001_init.down.sql",
		"0002_plugins.down.sql",
		"0003_external_identities.down.sql",
		"0004_plugin_governance.down.sql",
		"0005_plugin_signing_key.down.sql",
		"0006_node_proxy_pool_subscriptions.down.sql",
		"0007_jobs.down.sql",
		"0008_job_timeouts.down.sql",
		"0009_api_token_expiry.down.sql",
		"0010_registration_events.down.sql",
		"0011_mail_acceptance.down.sql",
		"0012_mail_delivery_attempts.down.sql",
		"0013_integration_invocation_quota.down.sql",
		"0014_job_attempt_retries.down.sql",
		"0015_job_schedule_planning.down.sql",
		"0016_protocol_endpoint_usage_daily.down.sql",
		"0017_protocol_endpoint_egress.down.sql",
		"0018_subscription_mutations.down.sql",
		"0019_subscription_traffic_reset.down.sql",
	}
	if !slices.Equal(down, expectedDown) {
		t.Fatalf("down migrations = %v, want matching baseline and plugin down migrations", down)
	}
	if err := validateMigrationInventory(up); err != nil {
		t.Fatal(err)
	}

	payload, err := migrations.Files.ReadFile(preReleaseBaselineVersion)
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	allBaselineTables := append(append([]string{}, preReleaseBaselineTables...), preReleaseReconciledTables...)
	if count := strings.Count(source, "CREATE TABLE "); count != len(allBaselineTables) {
		t.Fatalf("baseline creates %d tables, want %d", count, len(allBaselineTables))
	}
	for _, table := range allBaselineTables {
		if !strings.Contains(source, "CREATE TABLE `"+table+"`") {
			t.Errorf("baseline is missing table %s", table)
		}
	}
	for _, key := range []string{
		"'register_switch'",
		"'register_email_verification'",
		"'smtp_password'",
		"'registration-welcome'",
		"'maintenance-notice'",
		"'znet-sink'",
		"'clash'",
		"'sing-box'",
		`"version":3`,
		`"mode":"rule"`,
		`"mixed_enabled":true`,
		`"dns"`,
		`"tun"`,
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
