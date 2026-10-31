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
		"0020_menus.up.sql",
		"0021_order_change_snapshot.up.sql",
		"0022_catalog_archive.up.sql",
		"0023_subscription_lifecycle.up.sql",
		"0024_traffic_usage_hourly.up.sql",
		"0025_traffic_hourly_application.up.sql",
		"0026_fixed_quota_lifecycle.up.sql",
		"0027_external_forward_entries.up.sql",
		"0028_protocol_endpoint_usage_resets.up.sql",
		"0029_subscription_alerts.up.sql",
		"0030_protocol_endpoint_listen_address.up.sql",
		"0031_files_and_ticket_attachments.up.sql",
		"0032_order_creation_idempotency.up.sql",
		"0033_native_kernel_policy.up.sql",
		"0034_network_entry_listen_address.up.sql",
		"0035_publication_capability_deferral.up.sql",
		"0036_configuration_outcome_reconciliation.up.sql",
		"0037_commercial_purchase_snapshot.up.sql",
		"0038_wallet_topups.up.sql",
		"0039_payment_receipts.up.sql",
		"0040_order_funding.up.sql",
		"0041_wallet_adjustments.up.sql",
		"0042_entitlement_value_snapshot.up.sql",
		"0043_subscription_value_settlements.up.sql",
		"0044_retired_principal_hash.up.sql",
		"0045_publication_connection_phase.up.sql",
		"0046_principal_lookup_index.up.sql",
		"0047_credential_delivery.up.sql",
		"0048_entitlement_epochs.up.sql",
		"0049_entitlement_funding.up.sql",
		"0050_subscription_recovery_policy.up.sql",
		"0051_subscription_automatic_renewals.up.sql",
		"0052_consumption_refunds.up.sql",
		"0053_metering_usage_projection_pending.up.sql",
		"0054_hourly_usage_projection.up.sql",
		"0055_discount_purchase_snapshot.up.sql",
		"0056_observation_retention_index.up.sql",
		"0057_automatic_recurring_approval.up.sql",
		"0058_ticket_references.up.sql",
		"0059_node_asset_identity.up.sql",
		"0060_node_observation_handoff.up.sql",
		"0061_node_observation_material_retention.up.sql",
		"0062_traffic_diagnostic_material_retention.up.sql",
		"0063_metering_report_receipts.up.sql",
		"0064_traffic_record_meta_archives.up.sql",
		"0065_order_discount_redemptions.up.sql",
		"0066_automatic_renewal_pending_orders.up.sql",
		"0067_node_observation_retirement_budget.up.sql",
		"0068_coverage_runtime_identity.up.sql",
		"0069_principal_runtime_identity.up.sql",
		"0070_principal_statistics.up.sql",
		"0071_flow_start_statistics.up.sql",
		"0072_subscription_retirement_retry.up.sql",
		"0073_node_archive.up.sql",
		"0074_history_retention_indexes.up.sql",
		"0075_protocol_deletion.up.sql",
		"0076_compact_periodic_history.up.sql",
		"0077_principal_minute_buckets.up.sql",
		"0078_native_minute_settlement.up.sql",
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
		"0020_menus.down.sql",
		"0021_order_change_snapshot.down.sql",
		"0022_catalog_archive.down.sql",
		"0023_subscription_lifecycle.down.sql",
		"0024_traffic_usage_hourly.down.sql",
		"0025_traffic_hourly_application.down.sql",
		"0026_fixed_quota_lifecycle.down.sql",
		"0027_external_forward_entries.down.sql",
		"0028_protocol_endpoint_usage_resets.down.sql",
		"0029_subscription_alerts.down.sql",
		"0030_protocol_endpoint_listen_address.down.sql",
		"0031_files_and_ticket_attachments.down.sql",
		"0032_order_creation_idempotency.down.sql",
		"0033_native_kernel_policy.down.sql",
		"0034_network_entry_listen_address.down.sql",
		"0035_publication_capability_deferral.down.sql",
		"0036_configuration_outcome_reconciliation.down.sql",
		"0037_commercial_purchase_snapshot.down.sql",
		"0038_wallet_topups.down.sql",
		"0039_payment_receipts.down.sql",
		"0040_order_funding.down.sql",
		"0041_wallet_adjustments.down.sql",
		"0042_entitlement_value_snapshot.down.sql",
		"0043_subscription_value_settlements.down.sql",
		"0044_retired_principal_hash.down.sql",
		"0045_publication_connection_phase.down.sql",
		"0046_principal_lookup_index.down.sql",
		"0047_credential_delivery.down.sql",
		"0048_entitlement_epochs.down.sql",
		"0049_entitlement_funding.down.sql",
		"0050_subscription_recovery_policy.down.sql",
		"0051_subscription_automatic_renewals.down.sql",
		"0052_consumption_refunds.down.sql",
		"0053_metering_usage_projection_pending.down.sql",
		"0054_hourly_usage_projection.down.sql",
		"0055_discount_purchase_snapshot.down.sql",
		"0056_observation_retention_index.down.sql",
		"0057_automatic_recurring_approval.down.sql",
		"0058_ticket_references.down.sql",
		"0059_node_asset_identity.down.sql",
		"0060_node_observation_handoff.down.sql",
		"0061_node_observation_material_retention.down.sql",
		"0062_traffic_diagnostic_material_retention.down.sql",
		"0063_metering_report_receipts.down.sql",
		"0064_traffic_record_meta_archives.down.sql",
		"0065_order_discount_redemptions.down.sql",
		"0066_automatic_renewal_pending_orders.down.sql",
		"0067_node_observation_retirement_budget.down.sql",
		"0068_coverage_runtime_identity.down.sql",
		"0069_principal_runtime_identity.down.sql",
		"0070_principal_statistics.down.sql",
		"0071_flow_start_statistics.down.sql",
		"0072_subscription_retirement_retry.down.sql",
		"0073_node_archive.down.sql",
		"0074_history_retention_indexes.down.sql",
		"0075_protocol_deletion.down.sql",
		"0076_compact_periodic_history.down.sql",
		"0077_principal_minute_buckets.down.sql",
		"0078_native_minute_settlement.down.sql",
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
