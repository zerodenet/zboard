package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

func normalizeManagedRuleSourceFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = managedRuleSourceZeroRuleIR
	}
	switch value {
	case managedRuleSourceZeroRuleIR, "zero-rule-ir", "zero_rule_ir_v1", "canonical":
		return managedRuleSourceZeroRuleIR, nil
	case managedRuleSourceDomainList, managedRuleSourceCIDRList, managedRuleSourceClashClassical:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported source_format %q", value)
	}
}

func normalizeManagedRuleArtifactFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", managedRuleArtifactZeroRuleIR, "zero-rule-ir", "zero_rule_ir_v1", "canonical":
		return managedRuleArtifactZeroRuleIR, nil
	case managedRuleArtifactClashClassicalYAML, "clash", "clash-yaml":
		return managedRuleArtifactClashClassicalYAML, nil
	case managedRuleArtifactClashClassicalText, "clash-text":
		return managedRuleArtifactClashClassicalText, nil
	case managedRuleArtifactSingBoxSource, "sing-box", "singbox":
		return managedRuleArtifactSingBoxSource, nil
	default:
		return "", fmt.Errorf("unsupported rule artifact format %q", value)
	}
}

func parseManagedRuleSource(raw []byte, sourceFormat string) (managedRuleDocument, error) {
	if len(raw) > managedRuleMaxSourceBytes {
		return managedRuleDocument{}, fmt.Errorf("rule source exceeds %d bytes", managedRuleMaxSourceBytes)
	}
	format, err := normalizeManagedRuleSourceFormat(sourceFormat)
	if err != nil {
		return managedRuleDocument{}, err
	}
	if format == managedRuleSourceZeroRuleIR {
		return decodeAndNormalizeZeroRuleIR(raw)
	}

	rules := make([]managedRule, 0)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), managedRuleMaxValueBytes+4096)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		var rule managedRule
		switch format {
		case managedRuleSourceClashClassical:
			rule, err = parseManagedClashClassicalRule(line)
		case managedRuleSourceDomainList:
			rule, err = parseManagedDomainListRule(line)
		case managedRuleSourceCIDRList:
			rule, err = parseManagedCIDRListRule(line)
		}
		if err != nil {
			return managedRuleDocument{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		rules = append(rules, rule)
		if len(rules) > managedRuleMaxRules {
			return managedRuleDocument{}, fmt.Errorf("rule source exceeds %d rules", managedRuleMaxRules)
		}
	}
	if err := scanner.Err(); err != nil {
		return managedRuleDocument{}, err
	}
	return normalizeManagedRuleDocument(managedRuleDocument{Version: zeroRuleIRVersion, Rules: rules})
}

func decodeAndNormalizeZeroRuleIR(raw []byte) (managedRuleDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire managedRuleDocumentWire
	if err := decoder.Decode(&wire); err != nil {
		return managedRuleDocument{}, fmt.Errorf("invalid Zero Rule IR JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return managedRuleDocument{}, fmt.Errorf("invalid Zero Rule IR JSON: %w", err)
	}
	if wire.Version != zeroRuleIRVersion {
		return managedRuleDocument{}, fmt.Errorf("unsupported Zero Rule IR version %d; expected %d", wire.Version, zeroRuleIRVersion)
	}
	if wire.Rules == nil {
		return managedRuleDocument{}, errors.New("Zero Rule IR field rules is required and cannot be null")
	}
	return normalizeManagedRuleDocument(managedRuleDocument{Version: wire.Version, Name: wire.Name, Rules: *wire.Rules})
}

func normalizeManagedRuleDocument(document managedRuleDocument) (managedRuleDocument, error) {
	if document.Version == 0 {
		document.Version = zeroRuleIRVersion
	}
	if document.Version != zeroRuleIRVersion {
		return managedRuleDocument{}, fmt.Errorf("unsupported Zero Rule IR version %d; expected %d", document.Version, zeroRuleIRVersion)
	}
	if document.Name != nil {
		name := *document.Name
		if name == "" {
			return managedRuleDocument{}, errors.New("Zero Rule IR name cannot be empty")
		}
		if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
			return managedRuleDocument{}, errors.New("Zero Rule IR name must be valid UTF-8 and cannot contain NUL")
		}
		if len(name) > managedRuleMaxDisplayNameBytes {
			return managedRuleDocument{}, fmt.Errorf("Zero Rule IR name exceeds %d UTF-8 bytes", managedRuleMaxDisplayNameBytes)
		}
	}
	if len(document.Rules) == 0 {
		return managedRuleDocument{}, errors.New("Zero Rule IR rules cannot be empty")
	}
	if len(document.Rules) > managedRuleMaxRules {
		return managedRuleDocument{}, fmt.Errorf("Zero Rule IR contains more than %d rules", managedRuleMaxRules)
	}

	normalized := make([]managedRule, 0, len(document.Rules))
	for index, rule := range document.Rules {
		value, err := normalizeManagedRuleValue(rule.Type, rule.Value)
		if err != nil {
			return managedRuleDocument{}, fmt.Errorf("rule %d: %w", index, err)
		}
		normalized = append(normalized, managedRule{Type: rule.Type, Value: value})
	}
	document.Rules = normalizeManagedRuleOrder(normalized)
	return document, nil
}

func normalizeManagedRuleValue(ruleType, value string) (string, error) {
	if ruleType == "" {
		return "", errors.New("rule type is required")
	}
	if !utf8.ValidString(value) {
		return "", errors.New("rule value must be valid UTF-8")
	}
	if len(value) > managedRuleMaxValueBytes {
		return "", fmt.Errorf("rule value exceeds %d UTF-8 bytes", managedRuleMaxValueBytes)
	}
	switch ruleType {
	case managedRuleTypeDomainExact, managedRuleTypeDomainSuffix:
		trimmed := strings.TrimSuffix(strings.TrimSpace(value), ".")
		if trimmed == "" {
			return "", errors.New("domain is empty")
		}
		ascii, err := idna.Lookup.ToASCII(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid domain %q: %w", value, err)
		}
		ascii = strings.ToLower(ascii)
		if len(ascii) > 253 {
			return "", errors.New("domain exceeds 253 ASCII bytes")
		}
		for _, label := range strings.Split(ascii, ".") {
			if label == "" || len(label) > 63 {
				return "", errors.New("domain contains an empty label or a label longer than 63 bytes")
			}
		}
		return ascii, nil
	case managedRuleTypeDomainKeyword:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", errors.New("keyword is empty")
		}
		for _, char := range []byte(trimmed) {
			if char == 0 {
				return "", errors.New("keyword contains a NUL byte")
			}
			if char > 0x7f {
				return "", errors.New("keyword must contain ASCII characters only")
			}
		}
		return strings.ToLower(trimmed), nil
	case managedRuleTypeIPv4CIDR, managedRuleTypeIPv6CIDR:
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("invalid CIDR %q", value)
		}
		if ruleType == managedRuleTypeIPv4CIDR && !prefix.Addr().Is4() {
			return "", errors.New("ipv4_cidr requires an IPv4 prefix")
		}
		if ruleType == managedRuleTypeIPv6CIDR && !prefix.Addr().Is6() {
			return "", errors.New("ipv6_cidr requires an IPv6 prefix")
		}
		return prefix.Masked().String(), nil
	default:
		return "", fmt.Errorf("unsupported rule type %q", ruleType)
	}
}

func normalizeManagedRuleOrder(rules []managedRule) []managedRule {
	order := map[string]int{
		managedRuleTypeDomainExact: 0, managedRuleTypeDomainSuffix: 1,
		managedRuleTypeDomainKeyword: 2, managedRuleTypeIPv4CIDR: 3, managedRuleTypeIPv6CIDR: 4,
	}
	sort.Slice(rules, func(i, j int) bool {
		left, right := order[rules[i].Type], order[rules[j].Type]
		if left != right {
			return left < right
		}
		return rules[i].Value < rules[j].Value
	})

	deduplicated := make([]managedRule, 0, len(rules))
	for _, rule := range rules {
		if len(deduplicated) > 0 {
			previous := deduplicated[len(deduplicated)-1]
			if previous.Type == rule.Type && previous.Value == rule.Value {
				continue
			}
		}
		deduplicated = append(deduplicated, rule)
	}

	suffixes := make([]string, 0)
	for _, rule := range deduplicated {
		if rule.Type == managedRuleTypeDomainSuffix {
			suffixes = append(suffixes, rule.Value)
		}
	}
	sort.Slice(suffixes, func(i, j int) bool {
		leftLabels := strings.Count(suffixes[i], ".")
		rightLabels := strings.Count(suffixes[j], ".")
		if leftLabels != rightLabels {
			return leftLabels < rightLabels
		}
		return suffixes[i] < suffixes[j]
	})
	acceptedSuffixes := make(map[string]struct{}, len(suffixes))
	for _, suffix := range suffixes {
		if !managedDomainCoveredBySuffix(suffix, acceptedSuffixes) {
			acceptedSuffixes[suffix] = struct{}{}
		}
	}

	result := make([]managedRule, 0, len(deduplicated))
	for _, rule := range deduplicated {
		switch rule.Type {
		case managedRuleTypeDomainSuffix:
			if _, accepted := acceptedSuffixes[rule.Value]; !accepted {
				continue
			}
		case managedRuleTypeDomainExact:
			if managedDomainCoveredBySuffix(rule.Value, acceptedSuffixes) {
				continue
			}
		}
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := order[result[i].Type], order[result[j].Type]
		if left != right {
			return left < right
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func managedDomainCoveredBySuffix(domain string, suffixes map[string]struct{}) bool {
	candidate := domain
	for {
		if _, exists := suffixes[candidate]; exists {
			return true
		}
		dot := strings.IndexByte(candidate, '.')
		if dot < 0 {
			return false
		}
		candidate = candidate[dot+1:]
	}
}

func parseManagedClashClassicalRule(line string) (managedRule, error) {
	parts := strings.SplitN(line, ",", 3)
	if len(parts) < 2 {
		return managedRule{}, errors.New("classical rule must use TYPE,value syntax")
	}
	var ruleType string
	switch strings.ToUpper(strings.TrimSpace(parts[0])) {
	case "DOMAIN":
		ruleType = managedRuleTypeDomainExact
	case "DOMAIN-SUFFIX":
		ruleType = managedRuleTypeDomainSuffix
	case "DOMAIN-KEYWORD":
		ruleType = managedRuleTypeDomainKeyword
	case "IP-CIDR":
		ruleType = managedRuleTypeIPv4CIDR
	case "IP-CIDR6":
		ruleType = managedRuleTypeIPv6CIDR
	default:
		return managedRule{}, fmt.Errorf("unsupported classical rule type %q", parts[0])
	}
	value, err := normalizeManagedRuleValue(ruleType, strings.TrimSpace(parts[1]))
	return managedRule{Type: ruleType, Value: value}, err
}

func parseManagedDomainListRule(line string) (managedRule, error) {
	line = strings.TrimSpace(line)
	lower := strings.ToLower(line)
	ruleType, value := managedRuleTypeDomainExact, line
	switch {
	case strings.HasPrefix(lower, "full:"):
		value = strings.TrimSpace(line[len("full:"):])
	case strings.HasPrefix(lower, "domain:"):
		ruleType, value = managedRuleTypeDomainSuffix, strings.TrimSpace(line[len("domain:"):])
	case strings.HasPrefix(lower, "keyword:"):
		ruleType, value = managedRuleTypeDomainKeyword, strings.TrimSpace(line[len("keyword:"):])
	case strings.HasPrefix(lower, "regexp:"):
		return managedRule{}, errors.New("regexp domain rules are not supported by Zero Rule IR v1")
	case strings.HasPrefix(line, "+."):
		ruleType, value = managedRuleTypeDomainSuffix, strings.TrimPrefix(line, "+.")
	case strings.HasPrefix(line, "."):
		ruleType, value = managedRuleTypeDomainSuffix, strings.TrimPrefix(line, ".")
	}
	normalized, err := normalizeManagedRuleValue(ruleType, value)
	return managedRule{Type: ruleType, Value: normalized}, err
}

func parseManagedCIDRListRule(line string) (managedRule, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(line))
	if err != nil {
		return managedRule{}, errors.New("invalid CIDR")
	}
	ruleType := managedRuleTypeIPv6CIDR
	if prefix.Addr().Is4() {
		ruleType = managedRuleTypeIPv4CIDR
	}
	value, err := normalizeManagedRuleValue(ruleType, prefix.String())
	return managedRule{Type: ruleType, Value: value}, err
}
