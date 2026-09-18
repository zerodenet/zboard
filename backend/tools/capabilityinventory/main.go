// capabilityinventory extracts current ownership evidence using Go syntax,
// including multiline declarations. Run from backend with -root . .
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Symbol string `json:"symbol"`
	Detail string `json:"detail,omitempty"`
	Owner  string `json:"owner"`
}

var explicitWriteOwners = map[string]string{
	"entryMutations.Save":                                    "network",
	"endpointMutations.Save":                                 "network",
	"h.accountAdministration().Create":                       "identity",
	"h.accountAdministration().Update":                       "identity",
	"h.services.Announcements.Delete":                        "experience",
	"h.services.Announcements.Save":                          "experience",
	"h.services.Audit.RecordAdmin":                           "observability",
	"h.services.CredentialExpiry().ExpireDue":                "entitlements",
	"h.services.DeliveryOrder.Update":                        "network",
	"h.services.FairUsePolicies.Delete":                      "metering",
	"h.services.FairUsePolicies.Save":                        "metering",
	"h.services.Installation.Create":                         "platform",
	"h.services.Maintenance.Update":                          "platform",
	"h.services.ManagedDNS(h.credentialCipher, h, h).Create": "network",
	"h.services.ManagedDNS(h.credentialCipher, h, h).Update": "network",
	"h.services.CertificateLifecycle.Create":                 "network",
	"h.services.CertificateLifecycle.Update":                 "network",
	"h.services.MessageRequests().Create":                    "messaging",
	"h.services.MessageTemplates.Delete":                     "messaging",
	"h.services.MessageTemplates.Save":                       "messaging",
	"h.services.NodeGroupMutations.Create":                   "network",
	"h.services.NodeGroupMutations.Update":                   "network",
	"h.services.NodeActivity.Record":                         "network",
	"h.services.NodeAdministration.Create":                   "resources",
	"h.services.NodeAdministration.Update":                   "resources",
	"h.services.OrderCreation.Create":                        "commerce",
	"h.services.PlanCreation.Create":                         "commerce",
	"h.services.PlanUpdate.Update":                           "commerce",
	"h.services.ProtocolEndpointMultiplier.Update":           "resources",
	"h.services.ProtocolEndpointOrder.Update":                "network",
	"h.services.ProxyPoolMutations(h.credentialCipher, proxyPoolMutationInspector{h: h}).Save": "network",
	"h.services.ProviderAccounts(h.credentialCipher, h).Update":                                "network",
	"h.services.ProviderCreation(h.credentialCipher, h, h).Create":                             "network",
	"h.services.ProviderDirectory.Delete":                                                      "network",
	"h.services.QuotaRequests().Create":                                                        "entitlements",
	"h.services.SKUCreation.Create":                                                            "commerce",
	"h.services.SKUUpdate.Update":                                                              "commerce",
	"h.services.SettingUpdate(h.credentialCipher).Update":                                      "platform",
	"h.services.SiteSettings.Update":                                                           "platform",
	"h.services.SSHHostTrust.Pin":                                                              "network",
	"h.services.SystemConfigDefaults.Reconcile":                                                "platform",
	"h.services.SubscriptionTemplates.Delete":                                                  "entitlements",
	"h.services.SubscriptionTemplates.Save":                                                    "entitlements",
	"h.services.Tickets.Create":                                                                "experience",
	"h.subscriptionRuleSets().Delete":                                                          "entitlements",
	"h.subscriptionRuleSets().Save":                                                            "entitlements",
}

// Model ownership is intentionally enumerated by exact type name. This is a
// reviewed P0 ledger, not a package- or filename-prefix heuristic.
var explicitModelOwners = map[string]string{
	"AccountRegistrationEvent":           "identity",
	"Announcement":                       "experience",
	"AnnouncementRead":                   "experience",
	"AuditLog":                           "observability",
	"CertificateOperation":               "network",
	"CertificateProtocolEndpoint":        "network",
	"EmailTemplate":                      "messaging",
	"ExternalIdentity":                   "identity",
	"FlowUsage":                          "metering",
	"Installation":                       "platform",
	"MailDeliveryAttempt":                "messaging",
	"ManagedCertificate":                 "network",
	"ManagedDNSRecord":                   "network",
	"NetworkEntry":                       "resources",
	"Node":                               "resources",
	"NodeConfigPublish":                  "resources",
	"NodeGroup":                          "resources",
	"NodeGroupEndpoint":                  "resources",
	"NodeGroupNetworkEntry":              "resources",
	"NodeKernelState":                    "resources",
	"NodeOperation":                      "resources",
	"NodeProxyPool":                      "network",
	"Order":                              "commerce",
	"PaymentEvent":                       "commerce",
	"Plan":                               "commerce",
	"PlanSKU":                            "commerce",
	"PlanSKUOperation":                   "commerce",
	"PluginAuthorization":                "extensions",
	"PluginData":                         "extensions",
	"PluginHostLease":                    "extensions",
	"PluginInstallation":                 "extensions",
	"PluginMigration":                    "extensions",
	"PluginOperation":                    "extensions",
	"PluginVersion":                      "extensions",
	"ProtocolCredential":                 "entitlements",
	"ProtocolDeployment":                 "resources",
	"ProtocolEndpoint":                   "resources",
	"ProtocolEndpointUsageDaily":         "metering",
	"ProviderAccount":                    "network",
	"ProviderOperation":                  "network",
	"QuotaEvent":                         "entitlements",
	"RegistrationEmailChallenge":         "identity",
	"Subscription":                       "entitlements",
	"SubscriptionMember":                 "entitlements",
	"SubscriptionRuleSet":                "entitlements",
	"SubscriptionTemplate":               "entitlements",
	"SubscriptionTemplateRuleSetBinding": "entitlements",
	"SubscriptionToken":                  "entitlements",
	"SystemConfig":                       "platform",
	"Task":                               "jobs",
	"TaskItem":                           "jobs",
	"Ticket":                             "experience",
	"TicketMessage":                      "experience",
	"TrafficRecord":                      "metering",
	"User":                               "identity",
	"UserAPIToken":                       "identity",
	"siteLegalItem":                      "experience",
	"sitePolicyDocument":                 "experience",
}

var ignoredInMemoryMutations = map[string]bool{
	"h.zeroEventAuthCache.Delete":    true,
	"h.zeroEventAuthFailures.Delete": true,
}

var explicitRuntimeSiteOwners = map[string]string{
	"internal/adapters/persistence/networkstore/inventory.go": "network",
	"internal/application/runtime_status.go":                  "jobs",
	"internal/capabilities/jobs/jobs.go":                      "jobs",
	"internal/capabilities/jobs/batch_runner.go":              "jobs",
	"internal/capabilities/jobs/runtime_queues.go":            "jobs",
	"internal/capabilities/jobs/runtime.go":                   "jobs",
	"internal/capabilities/network/connector_activity.go":     "network",
	"internal/capabilities/network/managed_dns.go":            "network",
	"internal/capabilities/network/publication_queue.go":      "network",
	"internal/handler/certificate_management.go":              "network",
	"internal/handler/kernel_activation.go":                   "resources",
	"internal/handler/runtime_jobs.go":                        "jobs",
	"internal/handler/ssh_terminal.go":                        "resources",
	"internal/plugins/manager.go":                             "extensions",
	"internal/plugins/storage_host.go":                        "extensions",
	"internal/plugins/supervisor.go":                          "extensions",
	"internal/zeroevent/file_compression.go":                  "events",
	"internal/zeroevent/file_spool.go":                        "events",
	"internal/zeroevent/file_storage.go":                      "events",
}

var explicitLifecycleOwners = map[string]string{
	"h.CloseBackgroundJobs":              "jobs",
	"h.CloseCredentialExpiryWorker":      "entitlements",
	"h.CloseFairUseEvaluationWorker":     "metering",
	"h.CloseHistoryRetentionWorker":      "observability",
	"h.CloseNodePublishWorker":           "resources",
	"h.CloseProxyPoolSubscriptionWorker": "network",
	"h.CloseZeroEventSpool":              "events",
	"h.StartAdminTaskWorker":             "jobs",
	"h.StartCertificateRenewalWorker":    "network",
	"h.StartCredentialExpiryWorker":      "entitlements",
	"h.StartDNSPublicObservationWorker":  "network",
	"h.StartFairUseEvaluationWorker":     "metering",
	"h.StartHistoryRetentionWorker":      "observability",
	"h.StartNodePublishWorker":           "resources",
	"h.StartProxyPoolSubscriptionWorker": "network",
}

// Route ownership follows reviewed public contract segments. Matching is
// segment-safe and deliberately leaves future namespaces unassigned until the
// ledger is updated.
var explicitRoutePrefixes = []struct {
	Prefix string
	Owner  string
}{
	{"/api/zero/events", "metering"},
	{"/api/v1/account/announcements", "experience"},
	{"/api/v1/account/integrations", "identity"},
	{"/api/v1/account/security", "identity"},
	{"/api/v1/account/identities", "identity"},
	{"/api/v1/account/subscriptions", "entitlements"},
	{"/api/v1/announcements", "experience"},
	{"/api/v1/admin/announcements", "experience"},
	{"/api/v1/admin/audit-logs", "observability"},
	{"/api/v1/admin/certificates", "network"},
	{"/api/v1/admin/dashboard", "observability"},
	{"/api/v1/admin/database-migrations", "platform"},
	{"/api/v1/admin/dns-records", "network"},
	{"/api/v1/admin/email-templates", "messaging"},
	{"/api/v1/admin/entity-references", "observability"},
	{"/api/v1/admin/fair-use", "metering"},
	{"/api/v1/admin/kernel", "resources"},
	{"/api/v1/admin/maintenance", "platform"},
	{"/api/v1/admin/network-entries", "resources"},
	{"/api/v1/admin/node-cleanup-script", "resources"},
	{"/api/v1/admin/node-groups", "resources"},
	{"/api/v1/admin/node-operations", "resources"},
	{"/api/v1/admin/nodes", "resources"},
	{"/api/v1/admin/node-proxy-pools", "network"},
	{"/api/v1/admin/operation-logs", "observability"},
	{"/api/v1/admin/orders", "commerce"},
	{"/api/v1/admin/plan-skus", "commerce"},
	{"/api/v1/admin/plans", "commerce"},
	{"/api/v1/admin/plugin-market", "extensions"},
	{"/api/v1/admin/plugins", "extensions"},
	{"/api/v1/admin/protocol-deployments", "resources"},
	{"/api/v1/admin/protocol-endpoints", "resources"},
	{"/api/v1/admin/provider-accounts", "network"},
	{"/api/v1/admin/provider-definitions", "network"},
	{"/api/v1/admin/runtime-jobs", "jobs"},
	{"/api/v1/admin/settings", "platform"},
	{"/api/v1/admin/smtp", "messaging"},
	{"/api/v1/admin/subscription-delivery-order", "entitlements"},
	{"/api/v1/admin/subscription-rule-sets", "entitlements"},
	{"/api/v1/admin/subscription-templates", "entitlements"},
	{"/api/v1/admin/subscriptions", "entitlements"},
	{"/api/v1/admin/system-configs", "platform"},
	{"/api/v1/admin/system-info", "platform"},
	{"/api/v1/admin/tasks", "jobs"},
	{"/api/v1/admin/tickets", "experience"},
	{"/api/v1/admin/traffic", "metering"},
	{"/api/v1/admin/users", "identity"},
	{"/api/v1/auth", "identity"},
	{"/api/v1/client/subscription", "entitlements"},
	{"/api/v1/integrations", "extensions"},
	{"/api/v1/nodes", "resources"},
	{"/api/v1/orders", "commerce"},
	{"/api/v1/plans", "commerce"},
	{"/api/v1/plugin-ui", "extensions"},
	{"/api/v1/rules", "entitlements"},
	{"/api/v1/setup", "platform"},
	{"/api/v1/subscription", "entitlements"},
	{"/api/v1/subscription-templates", "entitlements"},
	{"/api/v1/subscriptions", "entitlements"},
	{"/api/v1/system", "platform"},
	{"/api/v1/tickets", "experience"},
	{"/api/v1/traffic", "metering"},
	{"/api/v1/version", "platform"},
	{"/healthz", "platform"},
	{"/readyz", "platform"},
}

func routeOwner(path string) string {
	for _, item := range explicitRoutePrefixes {
		if path == item.Prefix || strings.HasPrefix(path, item.Prefix+"/") {
			return item.Owner
		}
	}
	return "unassigned"
}

func main() {
	root := flag.String("root", ".", "backend source directory")
	flag.Parse()
	rows, err := inventory(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err = e.Encode(rows); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func inventory(root string) ([]Entry, error) {
	rows := []Entry{}
	fs := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fs, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		addOwned := func(kind, symbol, detail, assigned string, pos token.Pos) {
			owner := "unassigned"
			switch kind {
			case "write_candidate":
				if assigned := explicitWriteOwners[symbol]; assigned != "" {
					owner = assigned
				}
			case "model":
				if assigned := explicitModelOwners[symbol]; assigned != "" {
					owner = assigned
				}
			case "goroutine", "timer":
				if siteOwner := explicitRuntimeSiteOwners[rel]; siteOwner != "" {
					owner = siteOwner
				}
			case "lifecycle":
				if lifecycleOwner := explicitLifecycleOwners[symbol]; lifecycleOwner != "" {
					owner = lifecycleOwner
				}
			}
			if assigned != "" {
				owner = assigned
			}
			rows = append(rows, Entry{kind, rel, fs.Position(pos).Line, symbol, detail, owner})
		}
		add := func(kind, symbol, detail string, pos token.Pos) { addOwned(kind, symbol, detail, "", pos) }
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.TypeSpec:
				if _, ok := x.Type.(*ast.StructType); ok && strings.Contains(rel, "/model/") {
					add("model", x.Name.Name, "", x.Pos())
				}
			case *ast.GoStmt:
				add("goroutine", "go", snippet(fs, x.Call.Fun), x.Pos())
			case *ast.CallExpr:
				name := snippet(fs, x.Fun)
				if name == "newRoute" || name == "pluginRoute" {
					owner := ""
					if len(x.Args) > 1 {
						if literal, ok := x.Args[1].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							owner = routeOwner(strings.Trim(literal.Value, "`\""))
						}
					}
					if name == "newRoute" && snippet(fs, x) == "newRoute(method, path, h.PluginGuard(fn))" {
						owner = "extensions"
					}
					if name == "pluginRoute" && owner == "" {
						owner = "extensions"
					}
					addOwned("route", name, snippet(fs, x), owner, x.Pos())
				}
				if name == "time.NewTicker" || name == "time.NewTimer" || name == "time.After" || name == "time.AfterFunc" || name == "time.Tick" {
					add("timer", name, snippet(fs, x), x.Pos())
				}
				if strings.HasPrefix(name, "h.Start") || strings.HasPrefix(name, "h.Close") {
					add("lifecycle", name, snippet(fs, x), x.Pos())
				}
				if strings.HasPrefix(rel, "internal/handler/") && !ignoredInMemoryMutations[name] && (explicitWriteOwners[name] != "" || persistentMutationCall(x.Fun)) {
					add("write_candidate", name, snippet(fs, x), x.Pos())
				}
			}
			return true
		})
		return nil
	})
	if err == nil {
		var contractRows []Entry
		contractRows, err = inventoryContracts(root)
		rows = append(rows, contractRows...)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Kind < b.Kind
	})
	return rows, err
}

func persistentMutationCall(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Create", "Delete", "Exec", "FirstOrCreate", "Save", "Update", "Updates", "UpdateColumn", "UpdateColumns":
		return true
	default:
		return false
	}
}

func snippet(fs *token.FileSet, n ast.Node) string {
	var b bytes.Buffer
	_ = format.Node(&b, fs, n)
	s := strings.Join(strings.Fields(b.String()), " ")
	if len(s) > 600 {
		s = s[:600] + "..."
	}
	return s
}
