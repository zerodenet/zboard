package handler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionHandlersHaveNoDirectAuthorityDatabaseAccess(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(source, []byte("h."+"db")) {
			t.Fatalf("%s directly accesses the authority database", path)
		}
	}
}

func TestRequestedPersistenceClustersStayClosed(t *testing.T) {
	for _, path := range []string{
		"handlers.go",
		"certificate_management.go",
		"subscription_public_projection.go",
		"protocol_credentials.go",
		"network_entry_delivery.go",
		"subscription_rule_sets.go",
		"managed_rule_mutations.go",
		"managed_rule_public.go",
		"managed_rule_storage.go",
		"external_auth.go",
		"external_registration.go",
		"plugin_identity_bridge.go",
		"plugin_ui.go",
		"ssh_terminal.go",
		"subscription_access_isolation.go",
		"node_address_candidates.go",
		"node_load.go",
		"node_diagnostics_endpoint.go",
		"kernel_automation.go",
		"node_system_actions.go",
		"node_proxy_pool_config.go",
		"protocol_delivery_order.go",
		"native_jobs.go",
		"external_deletion.go",
		"dns_reconciliation.go",
		"node_config_publish.go",
		"protocol_load.go",
		"zero_event_runtime.go",
		"zero_events.go",
		"traffic_read_connection.go",
		"maintenance_setting.go",
		"email_templates.go",
		"system_calendar.go",
		"runtime_job_history.go",
		"runtime_diagnostics.go",
		"registration_email_verification.go",
		"protocol_credential_transaction.go",
		"principal_flow_trends.go",
		"history_retention.go",
		"credential_expiry_worker.go",
		"admin_system_info.go",
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(source, []byte("h."+"db")) {
			t.Fatalf("%s reintroduced handler-owned persistence", path)
		}
	}
}
