package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// New capabilities must remain usable by internal, HTTP and plugin adapters.
func TestCapabilitiesDoNotImportTransportOrPersistenceImplementations(t *testing.T) {
	err := filepath.WalkDir("../internal/capabilities", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, v := range f.Imports {
			p, _ := strconv.Unquote(v.Path.Value)
			for _, forbidden := range []string{"net/http", "gorm.io/", "/internal/handler", "/internal/model", "/internal/adapters", "/internal/plugins", "/pkg/pluginapi"} {
				if strings.Contains(p, forbidden) {
					t.Errorf("%s imports implementation %s", path, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDNSProviderPluginBoundaryIsVersionedFencedAndProviderAware(t *testing.T) {
	checks := map[string][]string{
		"../pkg/pluginapi/v1/control.proto": {
			"rpc VerifyDNSCredential(DNSCredentialRequest)",
			"rpc ApplyDNSRecord(DNSApplyRequest)",
			"uint64 generation",
			"uint64 config_revision",
		},
		"../internal/plugins/manifest.go": {
			"DNSProviderCapability",
			"DNS provider capability requires a configurable server and provider declarations",
			"DNS provider declarations require the DNS provider capability",
		},
		"../internal/plugins/dns_provider.go": {
			"checkRuntimeSnapshot(before)",
			"plugin_installations.generation",
			"plugin_authorizations.native_trusted",
			"Duplicate keys are deliberately omitted",
		},
		"../internal/capabilities/network/managed_dns.go": {
			"ProviderKey: execution.ProviderKey",
			"Credential:  token",
		},
	}
	for path, required := range checks {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(payload)
		for _, fragment := range required {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s lost DNS provider boundary %q", path, fragment)
			}
		}
	}
	payload, err := os.ReadFile("../internal/plugins/dns_provider.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "m.load(") {
		t.Error("DNS provider discovery regressed to per-installation loading")
	}
}

func TestCertificateProviderPluginBoundaryKeepsPrivateKeysOnNodes(t *testing.T) {
	checks := map[string][]string{
		"../pkg/pluginapi/v1/control.proto": {
			"rpc VerifyCertificateCredential(CertificateCredentialRequest)",
			"rpc IssueCertificate(CertificateIssueRequest)",
			"bytes csr_pem",
			"bytes fullchain_pem",
		},
		"../internal/plugins/manifest.go": {
			"CertificateProviderCapability",
			"certificate provider capability requires a configurable server and provider declarations",
		},
		"../internal/plugins/certificate_provider.go": {
			"checkRuntimeSnapshot(before)",
			"6*time.Minute",
			"len(result.FullchainPem) > 96<<10",
		},
		"../internal/handler/certificate_provider.go": {
			"buildCertificateCSRScript(certificate.ID, stageID, domains)",
			"validateProviderCertificate(fullchainPEM, csr, domains",
			"provider certificate does not match the node private key",
			"RunWithInput(buildCertificateInstallScript(certificate.ID, stageID), true, encoded)",
		},
	}
	for path, required := range checks {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(payload)
		for _, fragment := range required {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s lost certificate provider boundary %q", path, fragment)
			}
		}
	}
	payload, err := os.ReadFile("../internal/handler/certificate_provider.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "func buildCertificateInstallScript(certificateID uint, stageID string, fullchain") {
		t.Error("certificate PEM must be streamed over stdin, not interpolated into a shell script")
	}
}

func TestCapabilityInvocationRequiresBoundPrincipalAndAdmission(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/capabilities/catalog/catalog.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]struct {
		principal bool
		admission bool
	}{"List": {}, "Invoke": {}}
	for _, declaration := range f.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		state, ok := checks[fn.Name.Name]
		if !ok {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				state.principal = state.principal || callee.Name == "validPrincipal"
			case *ast.SelectorExpr:
				state.admission = state.admission || callee.Sel.Name == "Admit"
			}
			return true
		})
		checks[fn.Name.Name] = state
	}
	if !checks["List"].principal || !checks["Invoke"].principal || !checks["Invoke"].admission {
		t.Fatalf("catalog gate incomplete: %+v", checks)
	}

	for file, function := range map[string]string{
		"../internal/handler/integrations.go":        "integrationRegistry",
		"../internal/handler/plugin_capabilities.go": "pluginCapabilityCall",
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		bound := false
		for _, declaration := range f.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != function {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				packageName, packageOK := selector.X.(*ast.Ident)
				if packageOK && packageName.Name == "catalog" && selector.Sel.Name == "New" {
					bound = true
				}
				return true
			})
		}
		if !bound {
			t.Errorf("%s.%s does not construct a catalog with authority and admission", file, function)
		}
	}
}

func TestMigratedBusinessEntrypointsDoNotStartPrivateWorkers(t *testing.T) {
	for file, names := range map[string][]string{
		"../internal/handler/admin_task_worker.go":        {"StartAdminTaskWorker"},
		"../internal/handler/node_publish_worker.go":      {"StartNodePublishWorker", "startNodePublishWorker"},
		"../internal/handler/credential_expiry_worker.go": {"StartCredentialExpiryWorker"},
		"../internal/handler/zero_event_runtime.go":       {"ConfigureZeroEventSpool"},
		"../internal/handler/database_migration.go":       {"AdminDatabaseMigrationStartHandler"},
		"../internal/handler/admin_operations.go":         {"executeTaskItems", "executeTaskItemsContext"},
		"../internal/plugins/tasks_runtime.go":            {"syncTaskRegistrations", "SetTaskRuntime", "cancelPluginTasks"},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, name := range names {
				if fn.Name.Name != name {
					continue
				}
				found[name] = true
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if _, ok := n.(*ast.GoStmt); ok {
						t.Errorf("%s starts a private worker in %s", file, name)
					}
					if call, ok := n.(*ast.CallExpr); ok {
						if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "NewTicker" || selector.Sel.Name == "NewTimer" || selector.Sel.Name == "AfterFunc") {
							t.Errorf("%s starts a private timer in %s", file, name)
						}
					}
					return true
				})
			}
		}
		for _, name := range names {
			if !found[name] {
				t.Errorf("%s no longer contains audited entrypoint %s", file, name)
			}
		}
	}
}

func TestAdaptersDoNotConstructCoreRuntimesOrIdentityStores(t *testing.T) {
	for _, root := range []string{"../internal/handler", "../internal/plugins"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, v := range f.Imports {
				p, _ := strconv.Unquote(v.Path.Value)
				if strings.HasSuffix(p, "/adapters/persistence/identitystore") || strings.HasSuffix(p, "/adapters/persistence/jobstore") {
					t.Errorf("%s constructs persistence infrastructure outside application", path)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "NewRuntime" || selector.Sel.Name == "NewGatedRuntime") {
						t.Errorf("%s constructs a private runtime", path)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreparedSSHExecutionLivesInExternalAdapter(t *testing.T) {
	files := []string{
		"../internal/handler/ssh_execution.go",
		"../internal/handler/zero_remote_adapter.go",
		"../internal/handler/kernel_automation.go",
		"../internal/handler/node_config_publish.go",
		"../internal/handler/certificate_management.go",
		"../internal/handler/node_diagnostics_endpoint.go",
		"../internal/handler/ssh_terminal.go",
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "NewClientConn", "NewSession", "RequestPty":
				t.Errorf("%s retains low-level SSH execution through %s", path, selector.Sel.Name)
			case "CombinedOutput":
				if !strings.HasSuffix(path, "/kernel_automation.go") {
					t.Errorf("%s retains low-level SSH execution through %s", path, selector.Sel.Name)
				}
			}
			return true
		})
	}

	for _, obsolete := range []string{"uploadSSHFile", "runNodeSSHSession", "runNodeSSHSessionWithInput"} {
		if err := filepath.WalkDir("../internal/handler", func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			for _, declaration := range file.Decls {
				if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == obsolete {
					t.Errorf("obsolete handler SSH executor %s remains in %s", obsolete, path)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGenericJobStoreDoesNotOwnNetworkResourceModels(t *testing.T) {
	forbidden := map[string]bool{"CertificateOperation": true, "ProviderOperation": true, "ManagedCertificate": true, "ManagedDNSRecord": true, "ProviderAccount": true}
	err := filepath.WalkDir("../internal/adapters/persistence/jobstore", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if selector, ok := n.(*ast.SelectorExpr); ok && forbidden[selector.Sel.Name] {
				t.Errorf("%s owns resource model %s in generic job storage", path, selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCertificateLifecycleWritesDelegateToNetworkCapability(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/certificate_management.go", "CertificateLifecycle", []string{
		"ManagedCertificateUpdateHandler", "ManagedCertificateCreateHandler", "ManagedCertificateRenewalUpdateHandler",
		"startManagedCertificateOperation", "executeManagedCertificateOperationContext",
		"finishCertificateOperationFailure", "scanCertificateRenewals",
	})
}

func TestSubscriptionTemplateWritesDelegateToEntitlementCapability(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/subscription_templates.go", "SubscriptionTemplates", []string{
		"ReconcileSubscriptionTemplateDefaults", "saveSubscriptionTemplate", "AdminSubscriptionTemplateDeleteHandler",
	})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/subscription_client_renderers.go", "SubscriptionTemplates", []string{
		"ReconcileSubscriptionClientTemplateDefaults",
	})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/subscription_client_template_seed.go", "SubscriptionTemplates", []string{
		"SeedSubscriptionClientTemplateDefaults",
	})
}

func TestSubscriptionRuleSetWritesDelegateToEntitlementCapability(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/subscription_rule_sets.go", "subscriptionRuleSets", []string{
		"saveSubscriptionRuleSet", "AdminSubscriptionRuleSetDeleteHandler",
	})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/managed_rule_mutations.go", "subscriptionRuleSets", []string{
		"replaceManagedRuleContentAndSource",
	})
}

func TestHistoryRetentionWritesDelegateToObservabilityCapability(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/history_retention.go", "HistoryRetention", []string{
		"ReconcileHistoryRetentionDefaults", "runHistoryRetentionContext",
	})
}

func TestExperienceWritesDelegateToCapabilities(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/announcements.go", "Announcements", []string{
		"AccountAnnouncementReadHandler", "AdminAnnouncementCreateHandler", "AdminAnnouncementUpdateHandler", "AdminAnnouncementDeleteHandler",
	})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/tickets.go", "Tickets", []string{
		"TicketCreateHandler", "TicketReplyHandler", "changeTicketStatus",
	})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/site_customization_defaults.go", "SiteCustomizationDefaults", []string{
		"ReconcileSiteCustomizationDefaults",
	})
}

func TestAdministrativeBatchSubmissionDelegatesToJobsCapability(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/batch_operations.go", "BatchRequests", []string{"createOperationTask"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/node_system_actions.go", "BatchRequests", []string{"createNodeSystemActionTask"})
}

func TestRemainingHandlerWritesDelegateToCapabilities(t *testing.T) {
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/admin_operations.go", "SystemConfigDefaults", []string{"ReconcileSystemConfigDefaults"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/credential_expiry_worker.go", "CredentialExpiry", []string{"StartCredentialExpiryWorker", "runExpiredCredentialReconciliation"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/protocol_delivery_order.go", "ProtocolEndpointOrder", []string{"protocolEndpointOrderSnapshotHandler", "protocolEndpointOrderUpdateHandler"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/handlers.go", "NodeActivity", []string{"NodeConnectorHeartbeatHandler"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/zero_event_runtime.go", "NodeActivity", []string{"recordBufferedZeroConnectorReceipt"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/zero_events.go", "NodeActivity", []string{"recordZeroConnectorActivity"})
	assertHandlerFunctionsDelegateWithoutPersistence(t, "../internal/handler/ssh_execution.go", "SSHHostTrust", []string{"pinSSHHostKeyContext"})
	for file, names := range map[string][]string{
		"../internal/handler/email_templates.go":        {"AdminSMTPTestHandler"},
		"../internal/handler/node_proxy_pool_config.go": {"NodeProxyPoolConfigHandler", "NodeProxyPoolRuntimeHandler"},
		"../internal/handler/node_system_actions.go":    {"recordNodeSystemActionAudit"},
		"../internal/handler/ssh_terminal.go":           {"NodeSSHTerminalHandler"},
	} {
		assertHandlerFunctionsDelegateWithoutPersistence(t, file, "Audit", names)
	}
}

func assertHandlerFunctionsDelegateWithoutPersistence(t *testing.T, path, capability string, names []string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = false
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := wanted[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == capability {
				wanted[fn.Name.Name] = true
			}
			if receiver, ok := selector.X.(*ast.SelectorExpr); ok && receiver.Sel.Name == capability {
				return true
			}
			if receiver, ok := selector.X.(*ast.CallExpr); ok {
				if called, ok := receiver.Fun.(*ast.SelectorExpr); ok && called.Sel.Name == capability {
					return true
				}
			}
			switch selector.Sel.Name {
			case "Transaction", "Model", "Create", "FirstOrCreate", "Save", "Update", "Updates", "UpdateColumn", "UpdateColumns", "Delete", "Exec":
				t.Errorf("%s.%s owns persistence through %s", path, fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for name, delegated := range wanted {
		if !delegated {
			t.Errorf("%s.%s does not call %s", path, name, capability)
		}
	}
}

func TestProviderMutationEntrypointsDelegateAuthorityToCapabilities(t *testing.T) {
	for file, names := range map[string]map[string]bool{
		"../internal/handler/provider_management.go":  {"ProviderAccountCreateHandler": true, "ProviderAccountVerifyHandler": true, "ProviderAccountListHandler": true},
		"../internal/handler/node_delete_cascade.go":  {"NodeCascadeDeleteHandler": true},
		"../internal/handler/dns_deletion.go":         {"ManagedDNSDeleteHandler": true},
		"../internal/handler/certificate_deletion.go": {"ManagedCertificateDeleteHandler": true},
		"../internal/handler/provider_deletion.go":    {"ProviderAccountDeleteHandler": true},
		"../internal/handler/provider_update.go":      {"ProviderAccountUpdateHandler": true},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !names[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if selector, ok := n.(*ast.SelectorExpr); ok && selector.Sel.Name == "db" {
					t.Errorf("%s.%s accesses persistence instead of capability", file, fn.Name.Name)
				}
				return true
			})
		}
	}
}

func TestResourceBatchExecutionDelegatesDomainMutationToCapability(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/batch_operations.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	scopeDelegates := map[string]bool{"resolveNodeBatchScope": false, "resolveProtocolBatchScope": false, "groupProtocolEndpointsByNode": false}
	compatibilityDelegates := map[string]bool{"validateProtocolEndpointKernelSupport": false, "validateBatchProtocolActivation": false}
	publishesNodeDirectly := false
	explicitOperations := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "executeNodeDetectTask", "executeNodeReconcileTask", "executeNodeLifecycleTask", "executeProtocolDeployTask", "executeProtocolActiveTask":
			t.Errorf("resource mutation returned to handler: %s", fn.Name.Name)
		case "executeOperationTaskItem":
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if composite, ok := n.(*ast.CompositeLit); ok {
					if selector, ok := composite.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "BatchResourceOperations" {
						explicitOperations = true
					}
				}
				selector, ok := n.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "ResourceBatchExecution" {
					delegates = true
				}
				if ok && selector.Sel.Name == "db" {
					t.Error("resource batch dispatcher reads persistence directly")
				}
				return true
			})
		}
		if _, ok := scopeDelegates[fn.Name.Name]; ok {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "BatchScopes" {
					scopeDelegates[fn.Name.Name] = true
				}
				if selector.Sel.Name == "db" {
					t.Errorf("%s reads persistence instead of BatchScopes", fn.Name.Name)
				}
				return true
			})
		}
		if _, ok := compatibilityDelegates[fn.Name.Name]; ok {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "ProtocolCompatibility" {
					compatibilityDelegates[fn.Name.Name] = true
				}
				switch selector.Sel.Name {
				case "db", "loadProtocolEndpointNodes":
					t.Errorf("%s reads compatibility persistence through %s", fn.Name.Name, selector.Sel.Name)
				}
				return true
			})
		}
		if fn.Name.Name == "publishBatchNodeConfig" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "publishNodeConfigForNode" {
					publishesNodeDirectly = true
				}
				return true
			})
		}
	}
	if !delegates {
		t.Error("resource batch dispatcher does not call the application capability")
	}
	if !explicitOperations {
		t.Error("resource batch dispatcher passes transport authority instead of explicit operations")
	}
	for name, ok := range scopeDelegates {
		if !ok {
			t.Errorf("%s does not call BatchScopes", name)
		}
	}
	for name, ok := range compatibilityDelegates {
		if !ok {
			t.Errorf("%s does not call ProtocolCompatibility", name)
		}
	}
	if !publishesNodeDirectly {
		t.Error("publishBatchNodeConfig reloads the endpoint instead of using the admitted node target")
	}
}

func TestNodePublicationWorkerDelegatesQueueLifecycleToCapability(t *testing.T) {
	for file, names := range map[string]map[string]bool{
		"../internal/handler/node_publish_queue.go": {
			"claimNodeConfigPublish":  true,
			"finishNodeConfigPublish": true,
		},
		"../internal/handler/node_publish_worker.go": {
			"executeNodePublish": true,
		},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !names[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch selector.Sel.Name {
				case "Model", "Where", "Update", "Updates", "Delete":
					t.Errorf("%s.%s owns publication persistence through %s", file, fn.Name.Name, selector.Sel.Name)
				}
				return true
			})
		}
	}
}

func TestNodeGroupCredentialSyncDelegatesToEntitlements(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/protocol_credentials.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "reconcileNodeGroupCredentialsContext" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "GroupCredentialReconciliation" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "Model", "Where", "Update", "Updates", "Create", "Delete", "Transaction":
				t.Errorf("node-group credential sync owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("node-group credential sync does not call the entitlement application capability")
	}
}

func TestNodeRuntimeCompilationDoesNotReconcileCredentials(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/kernel_automation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	rendersExternally := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "compileNodeRuntimeConfigWithOptionsContext" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "RuntimeConfigurationSource" {
				delegates = true
			}
			if selector.Sel.Name == "Render" {
				rendersExternally = true
			}
			switch selector.Sel.Name {
			case "ensureCredentialsForSubscriptions", "ensureCredentialsForSubscriptionsWithMieru", "NodeCredentialReconciliation":
				t.Errorf("runtime config compilation performs entitlement reconciliation through %s", selector.Sel.Name)
			case "db", "loadManagedCertificatesForEndpoints", "activeEndpointCredentials", "runtimeCredentialContexts", "validateEndpointCredentialProjection", "appendNetworkEntryRuntime", "appendNetworkEntryRuntimeSnapshot", "runtimeInboundsForEndpoint", "runtimeInboundsForEndpointSnapshot":
				t.Errorf("runtime config compilation reads persistence through %s instead of its snapshot", selector.Sel.Name)
			case "Create", "Save", "Update", "Updates", "Delete", "Transaction":
				t.Errorf("runtime config compilation owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("runtime config compilation does not load RuntimeConfigurationSource")
	}
	if !rendersExternally {
		t.Error("runtime config compilation does not call the Zero runtime renderer")
	}
}

func TestHandlerDoesNotRetainZeroRuntimeRenderingImplementations(t *testing.T) {
	forbidden := map[string]struct{}{
		"activeEndpointCredentials":          {},
		"appendNetworkEntryRuntime":          {},
		"legacyRuntimeInboundsForEndpoint":   {},
		"managedAccessUserFields":            {},
		"managedMieruUsers":                  {},
		"managedShadowsocksRuntimeProtocol":  {},
		"runtimeCredentialContexts":          {},
		"runtimeInbound":                     {},
		"runtimeInboundsForEndpoint":         {},
		"runtimeInboundsForEndpointSnapshot": {},
	}
	err := filepath.WalkDir("../internal/handler", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if _, blocked := forbidden[fn.Name.Name]; blocked {
				t.Errorf("%s retains Zero runtime rendering function %s", path, fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProtocolEndpointMultiplierHandlerDelegatesWithoutPersistence(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/operation_logs.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ProtocolEndpointMultiplierHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "ProtocolEndpointMultiplier" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "db", "Model", "Updates", "Create", "Delete", "Transaction":
				t.Errorf("protocol multiplier handler owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("protocol multiplier handler does not call the network capability")
	}
}

func TestProxyPoolHTTPAdapterUsesNetworkCapabilitiesWithoutPersistence(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/node_proxy_pools.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	queries, mutations := false, false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "NodeProxyPoolsHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "ProxyPoolQueries":
				queries = true
			case "ProxyPoolMutations":
				mutations = true
			case "db", "Model", "Updates", "Create", "Delete", "Transaction":
				t.Errorf("proxy pool HTTP adapter owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !queries || !mutations {
		t.Errorf("proxy pool HTTP adapter capability delegation: queries=%v mutations=%v", queries, mutations)
	}
}

func TestProxyPoolSubscriptionAdapterDelegatesStateWithoutPersistence(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/node_proxy_pool_subscription.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{"syncNodeProxyPoolSubscription": false, "syncDueProxyPools": false}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := seen[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "ProxyPoolSubscriptions" {
				seen[fn.Name.Name] = true
			}
			switch selector.Sel.Name {
			case "db", "Model", "Updates", "Create", "Delete", "Save", "Transaction":
				t.Errorf("%s owns subscription persistence through %s", fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for function, delegates := range seen {
		if !delegates {
			t.Errorf("%s does not call ProxyPoolSubscriptions", function)
		}
	}
}

func TestNetworkEntryHTTPDelegatesQueriesAndCompositeTransaction(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/network_entries.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	queries, mutations := false, false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "NetworkEntriesHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "NetworkEntryQueries":
				queries = true
			case "NetworkEntryMutations":
				mutations = true
			}
			switch selector.Sel.Name {
			case "db", "Transaction", "Create", "CreateInBatches", "Updates", "Update", "Find", "First", "Table", "Joins", "Pluck":
				t.Errorf("network entry HTTP adapter owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !queries || !mutations {
		t.Errorf("network entry HTTP adapter delegation: queries=%v mutations=%v", queries, mutations)
	}
	obsolete := map[string]bool{"createNetworkEntry": true, "enqueueNetworkEntryPublishes": true, "applyNetworkEntryMembershipChanges": true, "networkEntryPortAvailable": true}
	if err := filepath.WalkDir("../internal/handler", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		candidate, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, decl := range candidate.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && obsolete[fn.Name.Name] {
				t.Errorf("obsolete network entry mutation helper remains: %s in %s", fn.Name.Name, path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNodeGroupHTTPMutationsDelegateCompositeTransaction(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{"NodeGroupCreateHandler": false, "NodeGroupUpdateHandler": false}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := seen[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "NodeGroupMutations" {
				seen[fn.Name.Name] = true
			}
			switch selector.Sel.Name {
			case "db", "Transaction", "CreateInBatches", "Updates", "Delete", "Find", "First", "Model", "Pluck":
				t.Errorf("%s owns persistence through %s", fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for function, delegates := range seen {
		if !delegates {
			t.Errorf("%s does not delegate to NodeGroupMutations", function)
		}
	}
	obsolete := map[string]bool{"replaceNodeGroupEndpoints": true, "replaceNodeGroupNetworkEntries": true}
	if err := filepath.WalkDir("../internal/handler", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		candidate, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, decl := range candidate.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && obsolete[fn.Name.Name] {
				t.Errorf("obsolete node group mutation helper remains: %s in %s", fn.Name.Name, path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolEndpointDeleteDelegatesCompositeRemoval(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ProtocolEndpointDeleteHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "ProtocolEndpointRemoval" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "db", "Transaction", "Model", "Create", "Updates", "Delete", "First", "Count":
				t.Errorf("protocol endpoint delete owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Fatal("protocol endpoint delete does not call ProtocolEndpointRemoval")
	}
}

func TestProtocolEndpointSaveDelegatesCompositeMutation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "saveProtocolEndpoint" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "db" {
				if root, ok := selector.X.(*ast.Ident); ok && root.Name == "h" {
					t.Error("protocol endpoint HTTP adapter still reads persistence through h.db")
				}
			}
			if selector.Sel.Name == "ProtocolEndpointMutations" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "Transaction", "Create", "CreateInBatches", "Updates", "Update", "Delete":
				t.Errorf("protocol endpoint HTTP adapter owns mutation persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Fatal("protocol endpoint save does not call ProtocolEndpointMutations")
	}
	obsolete := map[string]bool{
		"applyProtocolEndpointNodeGroupMembershipChanges": true,
		"validateProtocolEndpointDeactivationMemberships": true,
		"migrateProtocolEndpointCredentials":              true,
		"nodeGroupCredentialPublishTargets":               true,
		"validateProtocolParent":                          true,
		"loadUsableManagedCertificate":                    true,
		"protocolEndpointModel":                           true,
	}
	if err := filepath.WalkDir("../internal/handler", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		candidate, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, decl := range candidate.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && obsolete[fn.Name.Name] {
				t.Errorf("obsolete protocol endpoint mutation helper remains: %s in %s", fn.Name.Name, path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMieruEndpointConfigurationReconciliationDelegatesWrites(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/protocol_credentials.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates, queues := false, false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ReconcileMieruEndpointCredentials" && fn.Name.Name != "queueMieruEndpointPublications" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "MieruEndpointConfigurations" {
				delegates = true
			}
			if selector.Sel.Name == "PublicationRequests" {
				queues = true
			}
			if fn.Name.Name == "ReconcileMieruEndpointCredentials" && selector.Sel.Name == "scheduleNodeConfigPublish" {
				t.Error("Mieru endpoint reconciliation bypasses batched publication requests")
			}
			switch selector.Sel.Name {
			case "Transaction", "Create", "CreateInBatches", "Updates", "Update", "Delete", "Save":
				t.Errorf("Mieru endpoint reconciliation owns mutation persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates || !queues {
		t.Fatalf("Mieru endpoint reconciliation delegation: configurations=%v publications=%v", delegates, queues)
	}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == "prepareMieruEndpointConfigs" {
			t.Fatal("obsolete handler-owned Mieru endpoint persistence lookup remains")
		}
	}
}

func TestNodeAdministrationHandlersDelegateMutationOwnership(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{
		"NodeCreateHandler":                    false,
		"NodeUpdateHandler":                    false,
		"NodeSSHTestHandler":                   false,
		"NodeSSHConfigHandler":                 false,
		"NodeSSHHostKeyResetHandler":           false,
		"NodeConnectorCredentialRotateHandler": false,
		"NodeConnectorCredentialRevokeHandler": false,
		"NodeReportCredentialRotateHandler":    false,
		"NodeReportCredentialRevokeHandler":    false,
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := seen[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "NodeAdministration" {
				seen[fn.Name.Name] = true
			}
			if owner, ok := selector.X.(*ast.SelectorExpr); ok && owner.Sel.Name == "NodeAdministration" {
				return true
			}
			switch selector.Sel.Name {
			case "Transaction", "Create", "CreateInBatches", "Updates", "Update", "Delete", "Save":
				t.Errorf("%s owns node mutation persistence through %s", fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for function, delegated := range seen {
		if !delegated {
			t.Errorf("%s does not delegate to NodeAdministration", function)
		}
	}
}

func TestManagedDNSHandlersDelegatePersistenceAndExecutionOwnership(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/provider_management.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{
		"ManagedDNSListHandler":      false,
		"ManagedDNSCreateHandler":    false,
		"ManagedDNSUpdateHandler":    false,
		"ManagedDNSSyncHandler":      false,
		"startDNSOperation":          false,
		"executeDNSOperationContext": false,
		"observePublicDNS":           false,
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := seen[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "ManagedDNS" {
				seen[fn.Name.Name] = true
			}
			if owner, ok := selector.X.(*ast.CallExpr); ok {
				if method, ok := owner.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "ManagedDNS" {
					return true
				}
			}
			if selector.Sel.Name == "db" {
				t.Errorf("%s accesses persistence instead of ManagedDNS", fn.Name.Name)
			}
			switch selector.Sel.Name {
			case "Transaction", "Create", "CreateInBatches", "Updates", "Update", "Delete", "Save":
				t.Errorf("%s owns DNS mutation persistence through %s", fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for function, delegated := range seen {
		if !delegated {
			t.Errorf("%s does not delegate to ManagedDNS", function)
		}
	}
}

func TestNodePublicationAndKernelReconciliationUseNodeCredentialCapability(t *testing.T) {
	for file, function := range map[string]string{
		"../internal/handler/node_config_publish.go": "publishNodeConfigForNodeLocked",
		"../internal/handler/kernel_automation.go":   "PrepareActivation",
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		delegates := false
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != function {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "NodeCredentialReconciliation" {
					delegates = true
				}
				return true
			})
		}
		if !delegates {
			t.Errorf("%s does not call NodeCredentialReconciliation", function)
		}
	}
}

func TestKernelDetectionDelegatesStateLifecycleToCapability(t *testing.T) {
	delegates := map[string]bool{"NodeKernelDetectHandler": false, "detectBatchNode": false}
	for file, names := range map[string]map[string]bool{
		"../internal/handler/kernel_automation.go": {"NodeKernelDetectHandler": true},
		"../internal/handler/batch_operations.go":  {"detectBatchNode": true},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "completeKernelDetection" {
				t.Error("kernel detection persistence returned to handler")
			}
			if !names[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "KernelDetection" {
					delegates[fn.Name.Name] = true
				}
				switch selector.Sel.Name {
				case "beginKernelOperation", "completeKernelDetection", "failKernelOperation", "Model", "Create", "Save", "Updates", "Transaction":
					t.Errorf("%s.%s owns kernel detection state through %s", file, fn.Name.Name, selector.Sel.Name)
				}
				return true
			})
		}
	}
	for name, ok := range delegates {
		if !ok {
			t.Errorf("%s does not call KernelDetection capability", name)
		}
	}
}

func TestKernelStateQueryDelegatesToCapability(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/kernel_automation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "NodeKernelStateHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "KernelHistory" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "Model", "Where", "Find", "FirstOrCreate", "Create", "Updates", "Transaction":
				t.Errorf("NodeKernelStateHandler owns persistence through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("NodeKernelStateHandler does not call KernelHistory capability")
	}
}

func TestKernelReconciliationDelegatesStateLifecycleToCapability(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/batch_operations.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "beginKernelOperation", "setKernelOperationPhase", "finishKernelOperation", "failKernelOperation":
			t.Errorf("kernel reconciliation persistence returned to handler through %s", fn.Name.Name)
		}
		if fn.Name.Name != "reconcileBatchNode" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "KernelReconciliation" {
				delegates = true
			}
			switch selector.Sel.Name {
			case "Model", "Create", "Save", "Update", "Updates", "Transaction":
				t.Errorf("reconcileBatchNode owns reconciliation state through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("reconcileBatchNode does not call KernelReconciliation capability")
	}

	kernel, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/kernel_automation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	artifactDelegates := false
	for _, decl := range kernel.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "beginKernelOperation", "setKernelOperationPhase", "finishKernelOperation", "failKernelOperation", "waitForNodeConnectorEvent", "reconcileNodeKernel":
			t.Errorf("kernel reconciliation state or observation returned to handler through %s", fn.Name.Name)
		}
		if fn.Name.Name == "downloadZeroBinary" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "LoadBinary" {
					artifactDelegates = true
				}
				return true
			})
		}
	}
	if !artifactDelegates {
		t.Error("kernel artifact materialization does not delegate to the Zero adapter")
	}
	capability, err := os.ReadFile("../internal/capabilities/network/kernel_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"detecting", "resolving_release", "preparing_connector_credential", "downloading", "staging", "verifying", "waiting_connector_event", "rollbackAfterActivation"} {
		if !strings.Contains(string(capability), required) {
			t.Errorf("kernel reconciliation capability lost %q", required)
		}
	}
}

func TestKernelInstallAndRollbackDelegateToZeroAdapter(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/kernel_automation.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates := map[string]bool{"installNodeKernel": false, "rollbackNodeKernel": false}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "buildZeroInstallScript" || fn.Name.Name == "buildZeroRollbackScript" {
			t.Errorf("Zero kernel script implementation returned to handler through %s", fn.Name.Name)
		}
		if _, ok := delegates[fn.Name.Name]; !ok {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if fn.Name.Name == "installNodeKernel" && selector.Sel.Name == "Install" {
				delegates[fn.Name.Name] = true
			}
			if fn.Name.Name == "rollbackNodeKernel" && selector.Sel.Name == "Rollback" {
				delegates[fn.Name.Name] = true
			}
			switch selector.Sel.Name {
			case "dialNodeSSH", "dialNodeSSHContext", "runNodeSSHSession", "Upload":
				t.Errorf("%s executes Zero SSH lifecycle through %s", fn.Name.Name, selector.Sel.Name)
			}
			return true
		})
	}
	for name, ok := range delegates {
		if !ok {
			t.Errorf("%s does not delegate to the Zero adapter", name)
		}
	}
}

func TestPluginIdentityCommitBoundaryDoesNotExposeGORM(t *testing.T) {
	for _, file := range []string{
		"../internal/plugins/identity.go",
		"../internal/handler/external_auth.go",
		"../internal/handler/external_identity.go",
		"../internal/handler/external_registration.go",
	} {
		payload, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(payload)
		if strings.Contains(source, "gorm.io/gorm") || strings.Contains(source, "*gorm.DB") {
			t.Errorf("%s exposes GORM through the plugin identity commit boundary", file)
		}
	}
	payload, err := os.ReadFile("../internal/adapters/persistence/pluginstore/identity_transactions.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"PluginHostLease", "PluginInstallation", "clause.Locking", "External:", "InitialPasswords"} {
		if !strings.Contains(string(payload), required) {
			t.Errorf("plugin identity persistence fence lost %q", required)
		}
	}
}

func TestConfigurationPublicationDelegatesStateLifecycleToCapability(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/node_config_publish.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	delegates, external := false, false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "restoreGeneratedNodeCredential" || fn.Name.Name == "buildZeroConfigPublishScript" || fn.Name.Name == "buildZeroConfigRollbackScript" {
			t.Errorf("configuration publication implementation returned to handler through %s", fn.Name.Name)
		}
		if fn.Name.Name != "publishNodeConfigForNodeLocked" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "ConfigurationPublicationState" {
				delegates = true
			}
			if selector.Sel.Name == "Publish" {
				external = true
			}
			switch selector.Sel.Name {
			case "Model", "Create", "Save", "Update", "Updates", "Delete", "Transaction":
				t.Errorf("configuration publication owns persistence through %s", selector.Sel.Name)
			case "dialNodeSSH", "dialNodeSSHContext", "runNodeSSHSession", "Upload":
				t.Errorf("configuration publication owns Zero remote execution through %s", selector.Sel.Name)
			}
			return true
		})
	}
	if !delegates {
		t.Error("configuration publication does not call ConfigurationPublicationState capability")
	}
	if !external {
		t.Error("configuration publication does not call the Zero external adapter")
	}
}

// HTTP route registration must not be required to obtain a complete database.
func TestRouteRegistrationDoesNotPrepareSchema(t *testing.T) {
	file := "../internal/server/router.go"
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selector.Sel.Name
		if name == "RunMigrations" || name == "PrepareDatabaseSchema" || (strings.HasPrefix(name, "Reconcile") && strings.HasSuffix(name, "Schema")) {
			t.Errorf("HTTP registration changes schema through %s", name)
		}
		return true
	})
}

func TestMaintenanceAdapterDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/maintenance.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range f.Imports {
		path, _ := strconv.Unquote(item.Path.Value)
		if strings.Contains(path, "gorm.io") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("maintenance HTTP adapter imports persistence %s", path)
		}
	}
}

func TestMigrationSubmissionDoesNotOwnDatabaseTransactions(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/database_migration.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "AdminDatabaseMigrationStartHandler" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "db" {
				if root, ok := sel.X.(*ast.Ident); ok && root.Name == "h" {
					t.Error("migration submission still owns persistence")
				}
			}
			return true
		})
	}
}

func TestMigrationHTTPDoesNotOwnExecutionOrRecovery(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/database_migration.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch fn.Name.Name {
		case "runDatabaseMigration", "finishDatabaseMigration", "copyDatabase", "ReconcileInterruptedDatabaseMigrations":
			t.Errorf("migration lifecycle returned to HTTP adapter: %s", fn.Name.Name)
		}
	}
}

func TestMigrationHTTPDoesNotImportDatabaseImplementations(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/database_migration.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range f.Imports {
		path, _ := strconv.Unquote(item.Path.Value)
		for _, forbidden := range []string{"gorm.io", "/internal/model", "/internal/datastore", "/adapters/persistence"} {
			if strings.Contains(path, forbidden) {
				t.Errorf("migration transport imports %s", path)
			}
		}
	}
}

func TestSettingsQueryHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/system_config_queries.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range f.Imports {
		path, _ := strconv.Unquote(item.Path.Value)
		if strings.Contains(path, "gorm.io") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("settings query adapter imports %s", path)
		}
	}
}

func TestCommerceSKUCreationHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_sku_creation.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("commerce HTTP imports %s", path)
		}
	}
}

func TestCommerceSKUUpdateHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_sku_update.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("commerce update HTTP imports %s", path)
		}
	}
}

func TestCommercePlanUpdateHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_plan_update.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("commerce plan HTTP imports %s", path)
		}
	}
}

func TestCommercePlanCreationHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_plan_creation.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("commerce create HTTP imports %s", path)
		}
	}
}

func TestCommerceSKUQueriesHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_sku_queries.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("SKU queries HTTP imports %s", path)
		}
	}
}

func TestCommercePlanDetailsHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_plan_details.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("plan details HTTP imports %s", path)
		}
	}
}

func TestCommercePlanListingHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_plan_listing.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("plan listing HTTP imports %s", path)
		}
	}
}

func TestCommerceOrderCreationHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_order_creation.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("order creation HTTP imports %s", path)
		}
	}
}

func TestCommerceSettlementHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_order_settlement.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("settlement HTTP imports %s", path)
		}
	}
}

func TestCommerceOrderQueriesHTTPDoesNotOwnPersistence(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/commerce_order_queries.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("order queries HTTP imports %s", path)
		}
	}
}

func TestHTTPAdaptersDoNotCallOtherExportedHandlers(t *testing.T) {
	err := filepath.WalkDir("../internal/handler", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(selector.Sel.Name, "Handler") || !token.IsExported(selector.Sel.Name) {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if ok && receiver.Name == "h" {
				t.Errorf("%s calls exported HTTP adapter %s; share a private operation or capability instead", path, selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestJobReadinessUsesCapabilityRepositories(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/job_readiness.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
			t.Error("job readiness returned to handler-owned persistence")
		}
		return true
	})
}

func TestLegacyNodePublishSchedulerDoesNotReturn(t *testing.T) {
	for _, path := range []string{"../internal/handler/node_publish_worker.go", "../internal/handler/handlers.go"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, obsolete := range []string{"nodePublishScheduler", "publishScheduler"} {
			if strings.Contains(string(content), obsolete) {
				t.Errorf("%s restores obsolete %s", path, obsolete)
			}
		}
	}
}

func TestNodePublishWorkerDoesNotOwnPublicationQueries(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/node_publish_worker.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
			t.Error("node publication worker returned to handler-owned persistence")
		}
		return true
	})
}

func TestPeriodicJobDefinitionsStayInApplication(t *testing.T) {
	content, err := os.ReadFile("../internal/handler/background_jobs.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"jobs.Definition", ".RegisterWhen("} {
		if strings.Contains(string(content), forbidden) {
			t.Errorf("handler background adapter owns durable scheduling policy through %s", forbidden)
		}
	}
}

func TestRuntimeJobsHTTPUsesApplicationInspection(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/runtime_jobs.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		path, _ := strconv.Unquote(imported.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("runtime jobs HTTP imports persistence implementation %s", path)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
			t.Error("runtime jobs HTTP returned to direct database inspection")
		}
		return true
	})
}

func TestDashboardHTTPUsesObservabilityReadModel(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/dashboard_overview.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		path, _ := strconv.Unquote(imported.Path.Value)
		if strings.Contains(path, "gorm.io/") || strings.Contains(path, "/internal/model") || strings.Contains(path, "/adapters/persistence") {
			t.Errorf("dashboard HTTP imports persistence implementation %s", path)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
			t.Error("dashboard HTTP returned to direct database inspection")
		}
		return true
	})
	calendar, err := os.ReadFile("../internal/handler/system_calendar_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calendar), "loadDashboardOverview(r.Context(), period, now, location)") {
		t.Error("system-calendar dashboard bypasses the shared observability read model")
	}
	handlers, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range handlers.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "DashboardHandler" {
			continue
		}
		usesTotals := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
				t.Error("legacy dashboard returned to direct database inspection")
			}
			if selector.Sel.Name == "Totals" {
				usesTotals = true
			}
			return true
		})
		if !usesTotals {
			t.Error("legacy dashboard bypasses observability totals")
		}
		return
	}
	t.Error("DashboardHandler not found")
}

func TestInstallationMiddlewareUsesApplicationCache(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "InstallationMiddleware" {
			continue
		}
		usesState := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
				t.Error("installation middleware returned to a per-request database query")
			}
			if selector.Sel.Name == "InstallationState" {
				usesState = true
			}
			return true
		})
		if !usesState {
			t.Error("installation middleware bypasses the application cache")
		}
		return
	}
	t.Error("InstallationMiddleware not found")
}

func TestAccountReferenceAndOperationReadModelsStayOutOfHTTPPersistence(t *testing.T) {
	files := []string{
		"../internal/handler/admin_business_details.go",
		"../internal/handler/traffic_read_model.go",
		"../internal/handler/operation_logs.go",
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			name, _ := strconv.Unquote(imported.Path.Value)
			if strings.Contains(name, "gorm.io/") || strings.Contains(name, "/internal/model") || strings.Contains(name, "/adapters/persistence") {
				t.Errorf("%s imports persistence implementation %s", path, name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
				t.Errorf("%s returned to direct database access", path)
			}
			return true
		})
	}

	handlers, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/handlers.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range handlers.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "AdminUsersListHandler" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
					t.Error("admin account directory returned to direct database access")
				}
			}
			return true
		})
		return
	}
	t.Error("AdminUsersListHandler not found")
}

func TestCertificateTemplateAndExperienceReadsStayOutOfHTTPPersistence(t *testing.T) {
	for _, path := range []string{
		"../internal/handler/announcements.go",
		"../internal/handler/tickets.go",
		"../internal/handler/subscription_templates.go",
	} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			name, _ := strconv.Unquote(imported.Path.Value)
			if strings.Contains(name, "gorm.io/") || strings.Contains(name, "/adapters/persistence") {
				t.Errorf("%s imports persistence implementation %s", path, name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
					t.Errorf("%s returned to direct database access", path)
				}
			}
			return true
		})
	}
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/handler/certificate_management.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"loadManagedCertificateIDsForEndpoints": true, "ManagedCertificateListHandler": true, "ManagedCertificateGetHandler": true}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || !wanted[fn.Name.Name] {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" && selector.Sel.Name == "db" {
					t.Errorf("certificate read adapter %s returned to direct database access", fn.Name.Name)
				}
			}
			return true
		})
	}
}
