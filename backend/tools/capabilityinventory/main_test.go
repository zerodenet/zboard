package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMultilineRoutesAndTimersAreInventoriedWithoutTests(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "handler")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := `package handler
func Example() {
 newRoute(
  http.MethodGet,
  "/api/v1/example",
  h.ExampleHandler,
 )
 go work()
 time.NewTicker(interval)
}

`
	if err := os.WriteFile(filepath.Join(dir, "example.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "example_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Kind]++
		if r.Owner != "unassigned" {
			t.Fatal("invented ownership", r)
		}
	}
	for _, kind := range []string{"route", "goroutine", "timer"} {
		if counts[kind] != 1 {
			t.Fatal(counts)
		}
	}
}

func TestExplicitCapabilityWriteOwnerIsRecordedWithoutGuessingUnknownCalls(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "handler")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := `package handler
func Example() {
 entryMutations.Save(ctx, actor, command)
 endpointMutations.Save(ctx, actor, before, command)
 h.services.ProtocolEndpointMultiplier.Update(ctx, actor, endpoint, multiplier)
 h.services.ProxyPoolMutations(h.credentialCipher, proxyPoolMutationInspector{h: h}).Save(ctx, actor, command)
 h.services.NodeGroupMutations.Create(ctx, actor, command)
 h.services.NodeGroupMutations.Update(ctx, actor, command)
 h.services.CertificateLifecycle.Create(ctx, actor, command)
 h.services.CertificateLifecycle.Update(ctx, actor, id, command)
 h.services.Announcements.Save(ctx, actor, command, now)
 h.services.Announcements.Delete(ctx, actor, id)
 h.services.Tickets.Create(ctx, actor, command, now)
 h.services.Audit.RecordAdmin(ctx, actor, event)
 h.services.CredentialExpiry().ExpireDue(ctx, now, limit)
 h.services.ProtocolEndpointOrder.Update(ctx, actor, command)
 h.services.SystemConfigDefaults.Reconcile(ctx)
 h.services.NodeActivity.Record(ctx, node, credential, activity)
 h.services.SSHHostTrust.Pin(ctx, node, expected, observed)
 h.services.SubscriptionTemplates.Save(ctx, actor, command, expected, bindings)
 h.services.SubscriptionTemplates.Delete(ctx, actor, id)
 h.subscriptionRuleSets().Save(ctx, actor, command)
 h.subscriptionRuleSets().Delete(ctx, actor, id)
 h.zeroEventAuthCache.Delete(nodeID)
 h.zeroEventAuthFailures.Delete(nodeID)
 h.accountAdministration().Create(ctx, actor, command)
 h.accountAdministration().Update(ctx, actor, id, command)
 tx.Update("field", value)
 tx.UpdateColumn("field", value)
 tx.UpdateColumns(values)
 tx.FirstOrCreate(&value)
}
`
	if err := os.WriteFile(filepath.Join(dir, "example.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, row := range rows {
		if row.Kind == "write_candidate" {
			owners[row.Symbol] = row.Owner
		}
	}
	if owners["entryMutations.Save"] != "network" {
		t.Fatalf("network entry owner=%q", owners["entryMutations.Save"])
	}
	if owners["endpointMutations.Save"] != "network" {
		t.Fatalf("protocol endpoint owner=%q", owners["endpointMutations.Save"])
	}
	if owners["h.services.ProtocolEndpointMultiplier.Update"] != "resources" {
		t.Fatalf("explicit owner=%q", owners["h.services.ProtocolEndpointMultiplier.Update"])
	}
	if owners["h.services.ProxyPoolMutations(h.credentialCipher, proxyPoolMutationInspector{h: h}).Save"] != "network" {
		t.Fatalf("proxy pool owner=%q", owners["h.services.ProxyPoolMutations(h.credentialCipher, proxyPoolMutationInspector{h: h}).Save"])
	}
	for _, symbol := range []string{"h.services.NodeGroupMutations.Create", "h.services.NodeGroupMutations.Update"} {
		if owners[symbol] != "network" {
			t.Fatalf("node group owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.accountAdministration().Create", "h.accountAdministration().Update"} {
		if owners[symbol] != "identity" {
			t.Fatalf("identity owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.services.CertificateLifecycle.Create", "h.services.CertificateLifecycle.Update"} {
		if owners[symbol] != "network" {
			t.Fatalf("certificate owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.services.Announcements.Save", "h.services.Announcements.Delete", "h.services.Tickets.Create"} {
		if owners[symbol] != "experience" {
			t.Fatalf("experience owner for %s=%q", symbol, owners[symbol])
		}
	}
	for symbol, owner := range map[string]string{
		"h.services.Audit.RecordAdmin":              "observability",
		"h.services.CredentialExpiry().ExpireDue":   "entitlements",
		"h.services.ProtocolEndpointOrder.Update":   "network",
		"h.services.SystemConfigDefaults.Reconcile": "platform",
		"h.services.NodeActivity.Record":            "network",
		"h.services.SSHHostTrust.Pin":               "network",
	} {
		if owners[symbol] != owner {
			t.Fatalf("explicit owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.services.SubscriptionTemplates.Save", "h.services.SubscriptionTemplates.Delete"} {
		if owners[symbol] != "entitlements" {
			t.Fatalf("subscription template owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.subscriptionRuleSets().Save", "h.subscriptionRuleSets().Delete"} {
		if owners[symbol] != "entitlements" {
			t.Fatalf("subscription rule set owner for %s=%q", symbol, owners[symbol])
		}
	}
	for _, symbol := range []string{"h.zeroEventAuthCache.Delete", "h.zeroEventAuthFailures.Delete"} {
		if _, exists := owners[symbol]; exists {
			t.Fatalf("in-memory cache mutation inventoried as persistence: %s", symbol)
		}
	}
	if owners["tx.Update"] != "unassigned" {
		t.Fatalf("unknown write owner=%q", owners["tx.Update"])
	}
	for _, symbol := range []string{"tx.UpdateColumn", "tx.UpdateColumns", "tx.FirstOrCreate"} {
		if owners[symbol] != "unassigned" {
			t.Fatalf("hidden write owner for %s=%q", symbol, owners[symbol])
		}
	}
}

func TestModelsRequireExactReviewedOwnership(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "model")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := `package model
type User struct{}
type PaymentEvent struct{}
type InventedFutureModel struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "models.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, row := range rows {
		if row.Kind == "model" {
			owners[row.Symbol] = row.Owner
		}
	}
	if owners["User"] != "identity" || owners["PaymentEvent"] != "commerce" {
		t.Fatal(owners)
	}
	if owners["InventedFutureModel"] != "unassigned" {
		t.Fatalf("new model was silently assigned: %v", owners)
	}
}

func TestRoutesUseReviewedContractSegmentsAndLeaveFuturePathsUnassigned(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "handler")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := `package handler
func routes() {
 newRoute(http.MethodGet, "/api/v1/plans", h.PlanListHandler)
 newRoute(http.MethodGet, "/api/v1/plans/1", h.PlanGetHandler)
 newRoute(http.MethodGet, "/api/v1/plans-invented", h.UnknownHandler)
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{}
	for _, row := range rows {
		if row.Kind == "route" {
			owners = append(owners, row.Owner)
		}
	}
	if len(owners) != 3 || owners[0] != "commerce" || owners[1] != "commerce" || owners[2] != "unassigned" {
		t.Fatal(owners)
	}
}

func TestRepositoryContractInventoryIsComplete(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"plugin_rpc":               14,
		"plugin_capability":        9,
		"host_operation":           3,
		"plugin_host_operation":    2,
		"external_http_scope":      3,
		"cross_domain_transaction": 8,
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Kind]++
		if row.Owner == "unassigned" {
			t.Errorf("unassigned inventory evidence: %+v", row)
		}
	}
	for kind, expected := range want {
		if counts[kind] != expected {
			t.Errorf("%s count=%d want=%d", kind, counts[kind], expected)
		}
	}
}

func TestNewContractEvidenceCannotBypassOwnershipLedger(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"pkg/pluginapi/v1/control.proto": `service PluginControl {
 rpc Known(Empty) returns (Empty);
 rpc Future
 (Empty) returns (Empty);
}`,
		"internal/plugins/capabilities.go": `package plugins
const KnownCapability = "zboard.known.v1"
const FutureCapability = "zboard.future.v1"
func pluginOperationCapability(operation string) string {
 switch operation {
 case "known.read": return KnownCapability
 case "future.read": return FutureCapability
 default: return ""
 }
}`,
		"internal/application/catalog.go": `package application
import "github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
func (s *Services) RegisterKnownCapabilities() {
 _ = catalog.Descriptor{Name: "known.read", Owner: "known"}
 _ = catalog.Descriptor{Name: "future.read", Owner: "future"}
}`,
		"internal/handler/integrations.go": `package handler
func (h *handlers) integrationRegistry() { h.services.RegisterKnownCapabilities() }`,
		"internal/owned/transaction.go": `package owned
func Commit() {}`,
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ledger := ownershipLedger{
		Version:                 1,
		PluginRPCs:              []ownedContract{{Name: "PluginControl.Known", Owner: "known", Source: "pkg/pluginapi/v1/control.proto"}},
		PluginCapabilities:      []ownedContract{{Name: "zboard.known.v1", Owner: "known", Source: "internal/plugins/capabilities.go"}},
		HostOperations:          []hostOperationContract{{ownedContract: ownedContract{Name: "known.read", Owner: "known", Source: "internal/application/catalog.go"}, PluginCapability: "zboard.known.v1", ExternalHTTP: true}},
		CrossDomainTransactions: []transactionContract{{ID: "known.commit", Owner: "known", Source: "internal/owned/transaction.go", Symbol: "Commit", Participants: []string{"known", "other"}, Atomicity: "transaction", Compensation: "rollback", Outbox: "none required"}},
	}
	payload, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, filepath.FromSlash(ownershipLedgerPath))
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, payload, 0600); err != nil {
		t.Fatal(err)
	}

	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	futureKinds := map[string]bool{}
	for _, row := range rows {
		if row.Symbol == "PluginControl.Future" || row.Symbol == "zboard.future.v1" || row.Symbol == "future.read" {
			if row.Owner != "unassigned" {
				t.Fatalf("future evidence inherited ownership: %+v", row)
			}
			futureKinds[row.Kind] = true
		}
	}
	for _, kind := range []string{"plugin_rpc", "plugin_capability", "host_operation", "plugin_host_operation", "external_http_scope"} {
		if !futureKinds[kind] {
			t.Errorf("future %s evidence was not inventoried", kind)
		}
	}
}

func TestStaleLedgerEvidenceIsUnassigned(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0700); err != nil {
		t.Fatal(err)
	}
	ledger := ownershipLedger{Version: 1, PluginRPCs: []ownedContract{{Name: "PluginControl.Removed", Owner: "extensions", Source: "pkg/pluginapi/v1/control.proto"}}}
	payload, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(ownershipLedgerPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Kind == "ledger_stale_plugin_rpc" && row.Symbol == "PluginControl.Removed" && row.Owner == "unassigned" {
			return
		}
	}
	t.Fatal("stale RPC ledger entry was not rejected")
}
