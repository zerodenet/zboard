package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const ownershipLedgerPath = "architecture/capability-ownership.json"

type ownedContract struct {
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Source string `json:"source"`
}

type hostOperationContract struct {
	ownedContract
	PluginCapability string `json:"plugin_capability"`
	ExternalHTTP     bool   `json:"external_http"`
	Note             string `json:"note,omitempty"`
}

type transactionContract struct {
	ID           string   `json:"id"`
	Owner        string   `json:"owner"`
	Source       string   `json:"source"`
	Symbol       string   `json:"symbol"`
	Participants []string `json:"participants"`
	Atomicity    string   `json:"atomicity"`
	Compensation string   `json:"compensation"`
	Outbox       string   `json:"outbox"`
}

type ownershipLedger struct {
	Version                 int                     `json:"version"`
	PluginRPCs              []ownedContract         `json:"plugin_rpcs"`
	PluginCapabilities      []ownedContract         `json:"plugin_capabilities"`
	HostOperations          []hostOperationContract `json:"host_operations"`
	CrossDomainTransactions []transactionContract   `json:"cross_domain_transactions"`
}

type contractEvidence struct {
	Name     string
	Source   string
	Line     int
	Owner    string
	Function string
}

type pluginOperationEvidence struct {
	Operation  string
	Capability string
	Source     string
	Line       int
}

func inventoryContracts(root string) ([]Entry, error) {
	ledger, present, err := loadOwnershipLedger(root)
	if err != nil {
		return nil, err
	}
	if present {
		if err := validateOwnershipLedger(ledger); err != nil {
			return nil, err
		}
	}

	rows := []Entry{}
	rpcs, err := scanPluginRPCs(root)
	if err != nil {
		return nil, err
	}
	capabilities, constants, err := scanPluginCapabilities(root)
	if err != nil {
		return nil, err
	}
	hostOperations, err := scanHostOperations(root)
	if err != nil {
		return nil, err
	}
	pluginOperations, err := scanPluginOperationMappings(root, constants)
	if err != nil {
		return nil, err
	}
	externalRegistrations, err := scanExternalRegistrations(root)
	if err != nil {
		return nil, err
	}

	rpcLedger := indexOwnedContracts(ledger.PluginRPCs)
	capabilityLedger := indexOwnedContracts(ledger.PluginCapabilities)
	hostLedger := make(map[string]hostOperationContract, len(ledger.HostOperations))
	for _, item := range ledger.HostOperations {
		hostLedger[item.Name] = item
	}

	seenRPC := map[string]bool{}
	for _, item := range rpcs {
		seenRPC[item.Name] = true
		owner := matchedOwner(rpcLedger[item.Name], item.Source, "")
		rows = append(rows, Entry{Kind: "plugin_rpc", File: item.Source, Line: item.Line, Symbol: item.Name, Owner: owner})
	}
	appendStaleOwned(&rows, "plugin_rpc", ledger.PluginRPCs, seenRPC)

	seenCapability := map[string]bool{}
	for _, item := range capabilities {
		seenCapability[item.Name] = true
		owner := matchedOwner(capabilityLedger[item.Name], item.Source, "")
		rows = append(rows, Entry{Kind: "plugin_capability", File: item.Source, Line: item.Line, Symbol: item.Name, Owner: owner})
	}
	appendStaleOwned(&rows, "plugin_capability", ledger.PluginCapabilities, seenCapability)

	seenHost := map[string]bool{}
	for _, item := range hostOperations {
		seenHost[item.Name] = true
		contract, ok := hostLedger[item.Name]
		owner := "unassigned"
		if ok && contract.Source == item.Source && contract.Owner == item.Owner {
			owner = contract.Owner
		}
		detail := "descriptor_owner=" + item.Owner
		if ok && contract.Note != "" {
			detail += "; " + contract.Note
		}
		rows = append(rows, Entry{Kind: "host_operation", File: item.Source, Line: item.Line, Symbol: item.Name, Detail: detail, Owner: owner})
		if externalRegistrations[item.Function] {
			externalOwner := "unassigned"
			if owner != "unassigned" && contract.ExternalHTTP {
				externalOwner = owner
			}
			rows = append(rows, Entry{Kind: "external_http_scope", File: item.Source, Line: item.Line, Symbol: item.Name, Detail: "POST /api/v1/integrations/capabilities/:name/invoke", Owner: externalOwner})
		}
	}
	for _, item := range ledger.HostOperations {
		if !seenHost[item.Name] {
			rows = append(rows, staleEntry("host_operation", item.Source, item.Name))
			continue
		}
		actual := findContractEvidence(hostOperations, item.Name)
		if item.ExternalHTTP && !externalRegistrations[actual.Function] {
			rows = append(rows, staleEntry("external_http_scope", item.Source, item.Name))
		}
	}

	seenPluginOperations := map[string]bool{}
	for _, item := range pluginOperations {
		seenPluginOperations[item.Operation] = true
		contract, ok := hostLedger[item.Operation]
		owner := "unassigned"
		if ok && contract.PluginCapability == item.Capability && seenCapability[item.Capability] && capabilityLedger[item.Capability].Owner != "" {
			owner = contract.Owner
		}
		rows = append(rows, Entry{Kind: "plugin_host_operation", File: item.Source, Line: item.Line, Symbol: item.Operation, Detail: "requires=" + item.Capability, Owner: owner})
	}
	for _, item := range ledger.HostOperations {
		if item.PluginCapability != "" && !seenPluginOperations[item.Name] {
			rows = append(rows, staleEntry("plugin_host_operation", item.Source, item.Name))
		}
	}

	for _, item := range ledger.CrossDomainTransactions {
		line, found, err := findFunctionSymbol(root, item.Source, item.Symbol)
		if err != nil {
			return nil, err
		}
		owner := item.Owner
		if !found {
			owner = "unassigned"
		}
		detail, _ := json.Marshal(map[string]any{
			"participants": item.Participants,
			"atomicity":    item.Atomicity,
			"compensation": item.Compensation,
			"outbox":       item.Outbox,
		})
		rows = append(rows, Entry{Kind: "cross_domain_transaction", File: item.Source, Line: line, Symbol: item.ID, Detail: string(detail), Owner: owner})
	}
	return rows, nil
}

func loadOwnershipLedger(root string) (ownershipLedger, bool, error) {
	path := filepath.Join(root, filepath.FromSlash(ownershipLedgerPath))
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ownershipLedger{}, false, nil
	}
	if err != nil {
		return ownershipLedger{}, false, err
	}
	var ledger ownershipLedger
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return ownershipLedger{}, false, fmt.Errorf("decode %s: %w", ownershipLedgerPath, err)
	}
	return ledger, true, nil
}

func validateOwnershipLedger(ledger ownershipLedger) error {
	if ledger.Version != 1 {
		return fmt.Errorf("unsupported ownership ledger version %d", ledger.Version)
	}
	seen := map[string]string{}
	check := func(kind, name, owner, source string) error {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(source) == "" {
			return fmt.Errorf("incomplete %s ledger entry %q", kind, name)
		}
		key := kind + "\x00" + name
		if previous := seen[key]; previous != "" {
			return fmt.Errorf("duplicate %s ledger entry %q (%s and %s)", kind, name, previous, source)
		}
		seen[key] = source
		return nil
	}
	for _, item := range ledger.PluginRPCs {
		if err := check("plugin_rpc", item.Name, item.Owner, item.Source); err != nil {
			return err
		}
	}
	for _, item := range ledger.PluginCapabilities {
		if err := check("plugin_capability", item.Name, item.Owner, item.Source); err != nil {
			return err
		}
	}
	for _, item := range ledger.HostOperations {
		if err := check("host_operation", item.Name, item.Owner, item.Source); err != nil {
			return err
		}
	}
	for _, item := range ledger.CrossDomainTransactions {
		if err := check("cross_domain_transaction", item.ID, item.Owner, item.Source); err != nil {
			return err
		}
		if item.Symbol == "" || len(item.Participants) < 2 || item.Atomicity == "" || item.Compensation == "" || item.Outbox == "" {
			return fmt.Errorf("incomplete cross-domain transaction %q", item.ID)
		}
		ownerParticipates := false
		participantSet := map[string]bool{}
		for _, participant := range item.Participants {
			if participant == "" || participantSet[participant] {
				return fmt.Errorf("invalid participants for cross-domain transaction %q", item.ID)
			}
			participantSet[participant] = true
			ownerParticipates = ownerParticipates || participant == item.Owner
		}
		if !ownerParticipates {
			return fmt.Errorf("owner does not participate in cross-domain transaction %q", item.ID)
		}
	}
	return nil
}

func indexOwnedContracts(items []ownedContract) map[string]ownedContract {
	result := make(map[string]ownedContract, len(items))
	for _, item := range items {
		result[item.Name] = item
	}
	return result
}

func matchedOwner(item ownedContract, source, declaredOwner string) string {
	if item.Name == "" || item.Source != source || (declaredOwner != "" && item.Owner != declaredOwner) {
		return "unassigned"
	}
	return item.Owner
}

func appendStaleOwned(rows *[]Entry, kind string, ledger []ownedContract, seen map[string]bool) {
	for _, item := range ledger {
		if !seen[item.Name] {
			*rows = append(*rows, staleEntry(kind, item.Source, item.Name))
		}
	}
}

func staleEntry(kind, source, symbol string) Entry {
	return Entry{Kind: "ledger_stale_" + kind, File: source, Symbol: symbol, Detail: "ledger evidence is missing from source", Owner: "unassigned"}
}

func findContractEvidence(items []contractEvidence, name string) contractEvidence {
	for _, item := range items {
		if item.Name == name {
			return item
		}
	}
	return contractEvidence{}
}

var rpcPattern = regexp.MustCompile(`\brpc\s+([A-Za-z][A-Za-z0-9_]*)\s*\(`)
var pluginCapabilityPattern = regexp.MustCompile(`^zboard(?:\.[a-z][a-z0-9_-]*)+\.v[1-9][0-9]*$`)

func scanPluginRPCs(root string) ([]contractEvidence, error) {
	rel := "pkg/pluginapi/v1/control.proto"
	payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := []contractEvidence{}
	text := string(payload)
	for _, match := range rpcPattern.FindAllStringSubmatchIndex(text, -1) {
		line := 1 + strings.Count(text[:match[0]], "\n")
		result = append(result, contractEvidence{Name: "PluginControl." + text[match[2]:match[3]], Source: rel, Line: line})
	}
	return result, nil
}

func scanPluginCapabilities(root string) ([]contractEvidence, map[string]string, error) {
	result := []contractEvidence{}
	constants := map[string]string{}
	seenCapabilities := map[string]bool{}
	base := filepath.Join(root, "internal", "plugins")
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
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
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, specification := range gen.Specs {
				value, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, expression := range value.Values {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING || index >= len(value.Names) {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil {
						return err
					}
					constants[value.Names[index].Name] = text
					if pluginCapabilityPattern.MatchString(text) {
						result = append(result, contractEvidence{Name: text, Source: rel, Line: fs.Position(value.Pos()).Line})
						seenCapabilities[text] = true
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err == nil && pluginCapabilityPattern.MatchString(text) && !seenCapabilities[text] {
				result = append(result, contractEvidence{Name: text, Source: rel, Line: fs.Position(literal.Pos()).Line})
				seenCapabilities[text] = true
			}
			return true
		})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, constants, nil
	}
	return result, constants, err
}

func scanHostOperations(root string) ([]contractEvidence, error) {
	result := []contractEvidence{}
	base := filepath.Join(root, "internal")
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isCatalogDescriptor(literal.Type) {
					return true
				}
				name, nameOK := compositeStringField(literal, "Name")
				owner, _ := compositeStringField(literal, "Owner")
				if !nameOK {
					name = "unresolved:" + compositeFieldSnippet(fs, literal, "Name")
				}
				result = append(result, contractEvidence{Name: name, Owner: owner, Source: rel, Line: fs.Position(literal.Pos()).Line, Function: fn.Name.Name})
				return true
			})
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return result, err
}

func isCatalogDescriptor(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		pkg, ok := value.X.(*ast.Ident)
		return ok && pkg.Name == "catalog" && value.Sel.Name == "Descriptor"
	case *ast.Ident:
		return value.Name == "Descriptor"
	default:
		return false
	}
}

func compositeStringField(literal *ast.CompositeLit, field string) (string, bool) {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		value, valueOK := pair.Value.(*ast.BasicLit)
		if !ok || !valueOK || key.Name != field || value.Kind != token.STRING {
			continue
		}
		text, _ := strconv.Unquote(value.Value)
		return text, true
	}
	return "", false
}

func compositeFieldSnippet(fs *token.FileSet, literal *ast.CompositeLit, field string) string {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if ok && key.Name == field {
			return snippet(fs, pair.Value)
		}
	}
	return "missing"
}

func scanPluginOperationMappings(root string, constants map[string]string) ([]pluginOperationEvidence, error) {
	rel := "internal/plugins/capabilities.go"
	path := filepath.Join(root, filepath.FromSlash(rel))
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, path, nil, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := []pluginOperationEvidence{}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || (fn.Name.Name != "pluginOperationCapability" && fn.Name.Name != "OperationCapability") || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok || len(clause.List) == 0 {
				return true
			}
			capability := ""
			for _, statement := range clause.Body {
				ret, ok := statement.(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				switch value := ret.Results[0].(type) {
				case *ast.Ident:
					capability = constants[value.Name]
				case *ast.BasicLit:
					capability, _ = strconv.Unquote(value.Value)
				}
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				operation, _ := strconv.Unquote(literal.Value)
				result = append(result, pluginOperationEvidence{Operation: operation, Capability: capability, Source: rel, Line: fs.Position(clause.Pos()).Line})
			}
			return true
		})
	}
	return result, nil
}

func scanExternalRegistrations(root string) (map[string]bool, error) {
	rel := "internal/handler/integrations.go"
	path := filepath.Join(root, filepath.FromSlash(rel))
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "integrationRegistry" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && strings.HasPrefix(selector.Sel.Name, "Register") && strings.HasSuffix(selector.Sel.Name, "Capabilities") {
				result[selector.Sel.Name] = true
			}
			return true
		})
	}
	return result, nil
}

func findFunctionSymbol(root, source, wanted string) (int, bool, error) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, filepath.Join(root, filepath.FromSlash(source)), nil, 0)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if functionSymbol(fn) == wanted {
			return fs.Position(fn.Pos()).Line, true, nil
		}
	}
	return 0, false, nil
}

func functionSymbol(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typeName := fn.Recv.List[0].Type
	if pointer, ok := typeName.(*ast.StarExpr); ok {
		typeName = pointer.X
	}
	if ident, ok := typeName.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}
