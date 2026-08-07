package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blevesearch/vellum"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	managedRuleSetRenderer          = "managed"
	managedRuleSourceCanonical      = "canonical"
	managedRuleSourceDomainList     = "domain_list"
	managedRuleSourceCIDRList       = "cidr_list"
	managedRuleSourceClashClassical = "clash_classical"
	managedRuleSetFormatCanonical   = "canonical"
	managedRuleSourceFileName       = "source.rules"
	managedRuleMaxSourceBytes       = 16 << 20
	managedRuleMaxRules             = 4_000_000
	managedRuleMaxValueBytes        = 4_096
	managedRuleImportTimeout        = 20 * time.Second
)

const (
	managedRuleKindDomainExact   = "DOMAIN"
	managedRuleKindDomainSuffix  = "DOMAIN-SUFFIX"
	managedRuleKindDomainKeyword = "DOMAIN-KEYWORD"
	managedRuleKindIPv4CIDR      = "IP-CIDR"
	managedRuleKindIPv6CIDR      = "IP-CIDR6"
)

const (
	managedRuleArtifactZRS                = "zrs"
	managedRuleArtifactClashClassicalYAML = "clash-classical-yaml"
	managedRuleArtifactClashClassicalText = "clash-classical-text"
	managedRuleArtifactSingBoxSource      = "sing-box-source"
	managedRuleArtifactCanonical          = "canonical"
)

type managedRule struct {
	Kind  string
	Value string
}

type managedRuleDocument struct {
	Rules []managedRule
}

type managedRuleSetContentWriteReq struct {
	Content          string  `json:"content"`
	ExpectedRevision *uint64 `json:"expected_revision"`
}

type managedRuleSetImportReq struct {
	SourceURL        string  `json:"source_url"`
	SourceFormat     string  `json:"source_format"`
	ExpectedRevision *uint64 `json:"expected_revision"`
}

type managedRuleSetContentResponse struct {
	ID            uint   `json:"id"`
	Tag           string `json:"tag"`
	Content       string `json:"content"`
	RuleCount     int    `json:"rule_count"`
	ContentBytes  int    `json:"content_bytes"`
	ContentSHA256 string `json:"content_sha256"`
	Revision      uint64 `json:"revision"`
}

type managedRuleArtifactResponse struct {
	Body        []byte
	ContentType string
	Extension   string
}

type managedRuleSetPresentation struct {
	model.SubscriptionRuleSet
	Managed       bool   `json:"managed"`
	SourceURL     string `json:"source_url,omitempty"`
	SourceFormat  string `json:"source_format,omitempty"`
	RuleCount     int    `json:"rule_count"`
	ContentBytes  int    `json:"content_bytes"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
	PublicURL     string `json:"public_url,omitempty"`
}

func (h *handlers) managedRuleRoot() (string, error) {
	root := strings.TrimSpace(h.zeroArtifactDir)
	if root == "" {
		return "", errors.New("ZERO_ARTIFACT_DIR is required for managed rule sets")
	}
	return filepath.Join(root, "rules"), nil
}

func managedRuleTagPath(tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	if !subscriptionRuleSetTagPattern.MatchString(tag) || tag == "." || tag == ".." {
		return "", errors.New("invalid managed rule set tag")
	}
	return tag, nil
}

func (h *handlers) managedRuleSetDir(tag string) (string, error) {
	root, err := h.managedRuleRoot()
	if err != nil {
		return "", err
	}
	safeTag, err := managedRuleTagPath(tag)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, safeTag), nil
}

func (h *handlers) managedRuleSourcePath(tag string) (string, error) {
	dir, err := h.managedRuleSetDir(tag)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, managedRuleSourceFileName), nil
}

func (h *handlers) readManagedRuleSource(tag string) ([]byte, error) {
	path, err := h.managedRuleSourcePath(tag)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, gorm.ErrRecordNotFound
	}
	return content, err
}

func (h *handlers) writeManagedRuleSource(tag string, content []byte) error {
	if len(content) > managedRuleMaxSourceBytes {
		return fmt.Errorf("rule source exceeds %d bytes", managedRuleMaxSourceBytes)
	}
	path, err := h.managedRuleSourcePath(tag)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".source-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o640); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(filepath.Dir(path), "artifacts"))
}

func (h *handlers) removeManagedRuleSetFiles(tag string) error {
	dir, err := h.managedRuleSetDir(tag)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}

func normalizeManagedRuleSourceFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = managedRuleSourceCanonical
	}
	switch value {
	case managedRuleSourceCanonical, managedRuleSourceDomainList, managedRuleSourceCIDRList, managedRuleSourceClashClassical:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported source_format %q", value)
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
	rules := make([]managedRule, 0)
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), managedRuleMaxValueBytes+1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		var rule managedRule
		switch format {
		case managedRuleSourceCanonical, managedRuleSourceClashClassical:
			rule, err = parseManagedCanonicalRule(line)
		case managedRuleSourceDomainList:
			rule, err = parseManagedDomainListRule(line)
		case managedRuleSourceCIDRList:
			rule, err = parseManagedCIDRListRule(line)
		}
		if err != nil {
			return managedRuleDocument{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		key := rule.Kind + "\x00" + rule.Value
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		rules = append(rules, rule)
		if len(rules) > managedRuleMaxRules {
			return managedRuleDocument{}, fmt.Errorf("rule source exceeds %d rules", managedRuleMaxRules)
		}
	}
	if err := scanner.Err(); err != nil {
		return managedRuleDocument{}, err
	}
	return managedRuleDocument{Rules: rules}, nil
}

func parseManagedCanonicalRule(line string) (managedRule, error) {
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return managedRule{}, errors.New("rule must use TYPE,value syntax")
	}
	kind := strings.ToUpper(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	return normalizeManagedRule(kind, value)
}

func parseManagedDomainListRule(line string) (managedRule, error) {
	line = strings.TrimSpace(line)
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "full:"):
		return normalizeManagedRule(managedRuleKindDomainExact, strings.TrimSpace(line[len("full:"):]))
	case strings.HasPrefix(lower, "domain:"):
		return normalizeManagedRule(managedRuleKindDomainSuffix, strings.TrimSpace(line[len("domain:"):]))
	case strings.HasPrefix(lower, "keyword:"):
		return normalizeManagedRule(managedRuleKindDomainKeyword, strings.TrimSpace(line[len("keyword:"):]))
	case strings.HasPrefix(lower, "regexp:"):
		return managedRule{}, errors.New("regexp domain rules are not supported")
	case strings.HasPrefix(line, "+."):
		return normalizeManagedRule(managedRuleKindDomainSuffix, strings.TrimPrefix(line, "+."))
	case strings.HasPrefix(line, "."):
		return normalizeManagedRule(managedRuleKindDomainSuffix, strings.TrimPrefix(line, "."))
	default:
		return normalizeManagedRule(managedRuleKindDomainExact, line)
	}
}

func parseManagedCIDRListRule(line string) (managedRule, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(line))
	if err != nil {
		return managedRule{}, errors.New("invalid CIDR")
	}
	kind := managedRuleKindIPv6CIDR
	if prefix.Addr().Is4() {
		kind = managedRuleKindIPv4CIDR
	}
	return normalizeManagedRule(kind, prefix.String())
}

func normalizeManagedRule(kind, value string) (managedRule, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	value = strings.TrimSpace(value)
	if value == "" {
		return managedRule{}, errors.New("rule value is empty")
	}
	if len(value) > managedRuleMaxValueBytes || !utf8.ValidString(value) {
		return managedRule{}, fmt.Errorf("rule value must be valid UTF-8 and at most %d bytes", managedRuleMaxValueBytes)
	}
	switch kind {
	case managedRuleKindDomainExact, managedRuleKindDomainSuffix:
		value = strings.ToLower(strings.TrimSuffix(value, "."))
		if value == "" || strings.ContainsAny(value, " ,\t\r\n/") {
			return managedRule{}, errors.New("invalid domain rule")
		}
	case managedRuleKindDomainKeyword:
		value = strings.ToLower(value)
		if strings.ContainsAny(value, "\r\n") {
			return managedRule{}, errors.New("invalid domain keyword")
		}
	case managedRuleKindIPv4CIDR, managedRuleKindIPv6CIDR:
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return managedRule{}, errors.New("invalid CIDR rule")
		}
		if kind == managedRuleKindIPv4CIDR && !prefix.Addr().Is4() {
			return managedRule{}, errors.New("IP-CIDR requires an IPv4 prefix")
		}
		if kind == managedRuleKindIPv6CIDR && !prefix.Addr().Is6() {
			return managedRule{}, errors.New("IP-CIDR6 requires an IPv6 prefix")
		}
		value = prefix.Masked().String()
	default:
		return managedRule{}, fmt.Errorf("unsupported rule type %q", kind)
	}
	return managedRule{Kind: kind, Value: value}, nil
}

func encodeManagedCanonicalSource(document managedRuleDocument) []byte {
	var output strings.Builder
	for _, rule := range document.Rules {
		output.WriteString(rule.Kind)
		output.WriteByte(',')
		output.WriteString(rule.Value)
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func managedRuleContentMetadata(content []byte) (string, int) {
	digest := sha256.Sum256(content)
	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return hex.EncodeToString(digest[:]), count
}

func (h *handlers) presentManagedRuleSet(item model.SubscriptionRuleSet) managedRuleSetPresentation {
	presentation := managedRuleSetPresentation{SubscriptionRuleSet: item, Managed: item.Renderer == managedRuleSetRenderer}
	if !presentation.Managed {
		return presentation
	}
	presentation.SourceURL = item.URL
	presentation.SourceFormat = item.Behavior
	if content, err := h.readManagedRuleSource(item.Tag); err == nil {
		presentation.ContentBytes = len(content)
		presentation.ContentSHA256, presentation.RuleCount = managedRuleContentMetadata(content)
	}
	if siteURL, err := h.managedRuleSiteURL(); err == nil {
		presentation.PublicURL = managedRulePublicURL(siteURL, item.Tag, "")
	}
	return presentation
}

func (h *handlers) managedRuleSiteURL() (string, error) {
	var installation model.Installation
	if err := h.db.Select("site_url").First(&installation, 1).Error; err != nil {
		return "", err
	}
	siteURL := strings.TrimRight(strings.TrimSpace(installation.SiteURL), "/")
	if siteURL == "" {
		return "", errors.New("site URL is not configured")
	}
	return siteURL, nil
}

func managedRulePublicURL(siteURL, tag, format string) string {
	base := strings.TrimRight(strings.TrimSpace(siteURL), "/") + "/api/v1/rules/" + url.PathEscape(tag)
	if strings.TrimSpace(format) == "" {
		return base
	}
	return base + "?format=" + url.QueryEscape(format)
}

func (h *handlers) AdminSubscriptionRuleSetContentHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var item model.SubscriptionRuleSet
	if err := h.db.First(&item, id).Error; err != nil || item.Renderer != managedRuleSetRenderer {
		NotFound(w)
		return
	}
	if r.Method == http.MethodGet {
		content, err := h.readManagedRuleSource(item.Tag)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				content = []byte{}
			} else {
				ServerError(w, err)
				return
			}
		}
		digest, count := managedRuleContentMetadata(content)
		OK(w, managedRuleSetContentResponse{ID: item.ID, Tag: item.Tag, Content: string(content), RuleCount: count, ContentBytes: len(content), ContentSHA256: digest, Revision: item.Revision})
		return
	}
	var req managedRuleSetContentWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	document, err := parseManagedRuleSource([]byte(req.Content), managedRuleSourceCanonical)
	if err != nil {
		BadRequestFields(w, "规则内容校验失败。", map[string]string{"content": err.Error()})
		return
	}
	normalized := encodeManagedCanonicalSource(document)
	if err := h.replaceManagedRuleContent(item, normalized, req.ExpectedRevision, authClaimsFromRequestIgnoringError(h, r)); err != nil {
		h.writeManagedRuleMutationError(w, err)
		return
	}
	if err := h.db.First(&item, item.ID).Error; err != nil {
		ServerError(w, err)
		return
	}
	digest, count := managedRuleContentMetadata(normalized)
	OK(w, managedRuleSetContentResponse{ID: item.ID, Tag: item.Tag, Content: string(normalized), RuleCount: count, ContentBytes: len(normalized), ContentSHA256: digest, Revision: item.Revision})
}

func authClaimsFromRequestIgnoringError(h *handlers, r *http.Request) authClaims {
	claims, _ := h.authFromRequest(r)
	return claims
}

func (h *handlers) AdminSubscriptionRuleSetImportHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var item model.SubscriptionRuleSet
	if err := h.db.First(&item, id).Error; err != nil || item.Renderer != managedRuleSetRenderer {
		NotFound(w)
		return
	}
	var req managedRuleSetImportReq
	if err := decodeBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
		BadRequest(w, err.Error())
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" {
		req.SourceURL = item.URL
	}
	if strings.TrimSpace(req.SourceFormat) == "" {
		req.SourceFormat = item.Behavior
	}
	format, err := normalizeManagedRuleSourceFormat(req.SourceFormat)
	if err != nil {
		BadRequestFields(w, "远端规则导入失败。", map[string]string{"source_format": err.Error()})
		return
	}
	raw, err := fetchManagedRuleSource(r.Context(), req.SourceURL)
	if err != nil {
		BadRequestFields(w, "远端规则导入失败。", map[string]string{"source_url": err.Error()})
		return
	}
	document, err := parseManagedRuleSource(raw, format)
	if err != nil {
		BadRequestFields(w, "远端规则导入失败。", map[string]string{"source_format": err.Error()})
		return
	}
	normalized := encodeManagedCanonicalSource(document)
	item.URL = strings.TrimSpace(req.SourceURL)
	item.Behavior = format
	if err := h.replaceManagedRuleContentAndSource(item, normalized, req.ExpectedRevision, claims); err != nil {
		h.writeManagedRuleMutationError(w, err)
		return
	}
	if err := h.db.First(&item, item.ID).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, h.presentManagedRuleSet(item))
}

func (h *handlers) replaceManagedRuleContent(item model.SubscriptionRuleSet, content []byte, expected *uint64, claims authClaims) error {
	return h.replaceManagedRuleContentAndSource(item, content, expected, claims)
}

func (h *handlers) replaceManagedRuleContentAndSource(item model.SubscriptionRuleSet, content []byte, expected *uint64, claims authClaims) error {
	previous, previousErr := h.readManagedRuleSource(item.Tag)
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var locked model.SubscriptionRuleSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, item.ID).Error; err != nil {
			return err
		}
		if locked.Renderer != managedRuleSetRenderer {
			return gorm.ErrRecordNotFound
		}
		if expected != nil && locked.Revision != *expected {
			return fmt.Errorf("%w:%d", errSubscriptionRuleSetRevisionConflict, locked.Revision)
		}
		if err := h.writeManagedRuleSource(locked.Tag, content); err != nil {
			return err
		}
		updates := map[string]interface{}{"revision": locked.Revision + 1}
		if item.URL != locked.URL || item.Behavior != locked.Behavior {
			updates["url"] = item.URL
			updates["behavior"] = item.Behavior
		}
		if err := tx.Model(&locked).Updates(updates).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "subscription_rule_set.content.update", fmt.Sprintf("subscription_rule_set:%d", item.ID), fmt.Sprintf("rules=%d revision=%d", len(content), locked.Revision+1))
	})
	if err != nil {
		if previousErr == nil {
			_ = h.writeManagedRuleSource(item.Tag, previous)
		} else if errors.Is(previousErr, gorm.ErrRecordNotFound) {
			path, pathErr := h.managedRuleSourcePath(item.Tag)
			if pathErr == nil {
				_ = os.Remove(path)
			}
		}
	}
	return err
}

func (h *handlers) writeManagedRuleMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSubscriptionRuleSetRevisionConflict) {
		current := uint64(0)
		parts := strings.Split(err.Error(), ":")
		if len(parts) > 1 {
			current, _ = strconv.ParseUint(parts[len(parts)-1], 10, 64)
		}
		writeJSON(w, http.StatusConflict, "规则集已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": current})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	ServerError(w, err)
}

func fetchManagedRuleSource(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := validateManagedRuleImportURL(rawURL)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if !managedRuleImportAddressAllowed(address) {
					return nil, errors.New("source URL resolves to a private or local address")
				}
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   managedRuleImportTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			_, err := validateManagedRuleImportURL(request.URL.String())
			return err
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "zboard-rule-import/1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, managedRuleMaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > managedRuleMaxSourceBytes {
		return nil, fmt.Errorf("source exceeds %d bytes", managedRuleMaxSourceBytes)
	}
	return content, nil
}

func validateManagedRuleImportURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("source_url must be a complete HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("source_url cannot contain credentials or fragments")
	}
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && !managedRuleImportAddressAllowed(address) {
		return nil, errors.New("source_url cannot use a private or local address")
	}
	return parsed, nil
}

func managedRuleImportAddressAllowed(address netip.Addr) bool {
	return address.IsValid() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func (h *handlers) PublicManagedRuleSetHandler(w http.ResponseWriter, r *http.Request) {
	tag, err := publicManagedRuleSetTag(r.URL.Path)
	if err != nil {
		NotFound(w)
		return
	}
	var item model.SubscriptionRuleSet
	if err := h.db.Where("renderer = ? AND tag = ? AND is_active = ?", managedRuleSetRenderer, tag, true).First(&item).Error; err != nil {
		NotFound(w)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	usesUserAgent := false
	if format == "" {
		format = managedRuleFormatFromUserAgent(r.UserAgent())
		usesUserAgent = true
	}
	if format == "" {
		BadRequest(w, "format is required when the client cannot be detected")
		return
	}
	content, err := h.readManagedRuleSource(item.Tag)
	if err != nil {
		NotFound(w)
		return
	}
	document, err := parseManagedRuleSource(content, managedRuleSourceCanonical)
	if err != nil {
		ServerError(w, fmt.Errorf("parse managed rule source: %w", err))
		return
	}
	digest := sha256.Sum256(content)
	etag := `"` + hex.EncodeToString(digest[:]) + "-" + format + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	artifact, err := h.loadOrBuildManagedRuleArtifact(item, document, digest, format)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if usesUserAgent {
		w.Header().Add("Vary", "User-Agent")
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", artifact.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", item.Tag+artifact.Extension))
	w.Header().Set("X-Zboard-Rule-Format", format)
	w.Header().Set("X-Zboard-Rule-Revision", strconv.FormatUint(item.Revision, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Body)
}

func publicManagedRuleSetTag(path string) (string, error) {
	trimmed := strings.Trim(path, "/")
	const prefix = "api/v1/rules/"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", errors.New("invalid rule path")
	}
	tag, err := url.PathUnescape(strings.TrimPrefix(trimmed, prefix))
	if err != nil || strings.Contains(tag, "/") {
		return "", errors.New("invalid rule tag")
	}
	_, err = managedRuleTagPath(tag)
	return tag, err
}

func managedRuleFormatFromUserAgent(userAgent string) string {
	userAgent = strings.ToLower(userAgent)
	switch {
	case strings.Contains(userAgent, "znet-sink"), strings.Contains(userAgent, "zerodenet"):
		return managedRuleArtifactZRS
	case strings.Contains(userAgent, "clash"):
		return managedRuleArtifactClashClassicalYAML
	case strings.Contains(userAgent, "sing-box"), strings.Contains(userAgent, "singbox"):
		return managedRuleArtifactSingBoxSource
	default:
		return ""
	}
}

func (h *handlers) loadOrBuildManagedRuleArtifact(item model.SubscriptionRuleSet, document managedRuleDocument, digest [32]byte, format string) (managedRuleArtifactResponse, error) {
	artifactDir, err := h.managedRuleSetDir(item.Tag)
	if err != nil {
		return managedRuleArtifactResponse{}, err
	}
	artifactDir = filepath.Join(artifactDir, "artifacts", hex.EncodeToString(digest[:]))
	response := managedRuleArtifactResponse{}
	switch format {
	case managedRuleArtifactZRS:
		response.ContentType, response.Extension = "application/vnd.zerodenet.zrs", ".zrs"
	case managedRuleArtifactClashClassicalYAML:
		response.ContentType, response.Extension = "application/yaml; charset=utf-8", ".yaml"
	case managedRuleArtifactClashClassicalText:
		response.ContentType, response.Extension = "text/plain; charset=utf-8", ".list"
	case managedRuleArtifactSingBoxSource:
		response.ContentType, response.Extension = "application/json; charset=utf-8", ".json"
	case managedRuleArtifactCanonical:
		response.ContentType, response.Extension = "text/plain; charset=utf-8", ".rules"
	default:
		return managedRuleArtifactResponse{}, fmt.Errorf("unsupported rule artifact format %q", format)
	}
	path := filepath.Join(artifactDir, format)
	if body, err := os.ReadFile(path); err == nil {
		response.Body = body
		return response, nil
	}
	var body []byte
	switch format {
	case managedRuleArtifactZRS:
		body, err = encodeManagedRuleZRS(item.Name, document)
	case managedRuleArtifactClashClassicalYAML:
		body = encodeManagedRuleClashYAML(document)
	case managedRuleArtifactClashClassicalText, managedRuleArtifactCanonical:
		body = encodeManagedCanonicalSource(document)
	case managedRuleArtifactSingBoxSource:
		body, err = encodeManagedRuleSingBox(document)
	}
	if err != nil {
		return managedRuleArtifactResponse{}, err
	}
	if err := os.MkdirAll(artifactDir, 0o750); err != nil {
		return managedRuleArtifactResponse{}, err
	}
	if err := os.WriteFile(path, body, 0o640); err != nil {
		return managedRuleArtifactResponse{}, err
	}
	response.Body = body
	return response, nil
}

func encodeManagedRuleClashYAML(document managedRuleDocument) []byte {
	var output strings.Builder
	output.WriteString("payload:\n")
	for _, rule := range document.Rules {
		value := rule.Kind + "," + rule.Value
		output.WriteString("  - '")
		output.WriteString(strings.ReplaceAll(value, "'", "''"))
		output.WriteString("'\n")
	}
	return []byte(output.String())
}

func encodeManagedRuleSingBox(document managedRuleDocument) ([]byte, error) {
	rule := map[string]interface{}{}
	for _, item := range document.Rules {
		field := ""
		switch item.Kind {
		case managedRuleKindDomainExact:
			field = "domain"
		case managedRuleKindDomainSuffix:
			field = "domain_suffix"
		case managedRuleKindDomainKeyword:
			field = "domain_keyword"
		case managedRuleKindIPv4CIDR, managedRuleKindIPv6CIDR:
			field = "ip_cidr"
		}
		values, _ := rule[field].([]string)
		rule[field] = append(values, item.Value)
	}
	rules := make([]map[string]interface{}, 0, 1)
	if len(rule) > 0 {
		rules = append(rules, rule)
	}
	return json.MarshalIndent(map[string]interface{}{"version": 3, "rules": rules}, "", "  ")
}

type managedIPv4Range struct{ Start, End uint32 }
type managedIPv6Range struct{ StartHi, StartLo, EndHi, EndLo uint64 }

func encodeManagedRuleZRS(displayName string, document managedRuleDocument) ([]byte, error) {
	exact := make([]string, 0)
	suffix := make([]string, 0)
	keywords := make([]string, 0)
	ipv4 := make([]managedIPv4Range, 0)
	ipv6 := make([]managedIPv6Range, 0)
	for _, rule := range document.Rules {
		switch rule.Kind {
		case managedRuleKindDomainExact:
			exact = append(exact, rule.Value)
		case managedRuleKindDomainSuffix:
			suffix = append(suffix, rule.Value)
		case managedRuleKindDomainKeyword:
			keywords = append(keywords, rule.Value)
		case managedRuleKindIPv4CIDR:
			prefix, _ := netip.ParsePrefix(rule.Value)
			ipv4 = append(ipv4, managedIPv4PrefixRange(prefix))
		case managedRuleKindIPv6CIDR:
			prefix, _ := netip.ParsePrefix(rule.Value)
			ipv6 = append(ipv6, managedIPv6PrefixRange(prefix))
		}
	}
	sort.Strings(exact)
	sort.Strings(suffix)
	sort.Strings(keywords)
	ipv4 = mergeManagedIPv4Ranges(ipv4)
	ipv6 = mergeManagedIPv6Ranges(ipv6)
	exactFST, err := encodeManagedFST(exact)
	if err != nil {
		return nil, err
	}
	suffixFST, err := encodeManagedFST(suffix)
	if err != nil {
		return nil, err
	}
	sections := []struct {
		kind, encoding uint16
		body           []byte
	}{
		{1, 1, exactFST},
		{2, 1, suffixFST},
		{3, 2, encodeManagedStringTable(keywords)},
		{4, 3, encodeManagedIPv4Ranges(ipv4)},
		{5, 4, encodeManagedIPv6Ranges(ipv6)},
	}
	const headerSize, sectionEntrySize = 128, 24
	output := make([]byte, alignManagedRule8(headerSize+len(sections)*sectionEntrySize))
	entries := make([][4]uint64, 0, len(sections))
	for _, section := range sections {
		if len(output)%8 != 0 {
			output = append(output, make([]byte, 8-len(output)%8)...)
		}
		offset := len(output)
		output = append(output, section.body...)
		entries = append(entries, [4]uint64{uint64(section.kind), uint64(section.encoding), uint64(offset), uint64(len(section.body))})
	}
	copy(output[0:4], []byte("ZRS!"))
	binary.LittleEndian.PutUint16(output[4:6], 0)
	binary.LittleEndian.PutUint16(output[6:8], 1)
	binary.LittleEndian.PutUint16(output[8:10], headerSize)
	binary.LittleEndian.PutUint16(output[10:12], uint16(len(sections)))
	binary.LittleEndian.PutUint64(output[16:24], uint64(len(output)))
	name := truncateManagedUTF8(displayName, 63)
	copy(output[32:96], []byte(name))
	for index, entry := range entries {
		cursor := headerSize + index*sectionEntrySize
		binary.LittleEndian.PutUint16(output[cursor:cursor+2], uint16(entry[0]))
		binary.LittleEndian.PutUint16(output[cursor+2:cursor+4], uint16(entry[1]))
		binary.LittleEndian.PutUint32(output[cursor+4:cursor+8], 1)
		binary.LittleEndian.PutUint64(output[cursor+8:cursor+16], entry[2])
		binary.LittleEndian.PutUint64(output[cursor+16:cursor+24], entry[3])
	}
	binary.LittleEndian.PutUint32(output[24:28], crc32.ChecksumIEEE(output[headerSize:]))
	return output, nil
}

func encodeManagedFST(values []string) ([]byte, error) {
	var output bytes.Buffer
	builder, err := vellum.New(&output, nil)
	if err != nil {
		return nil, err
	}
	previous := ""
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		if err := builder.Insert([]byte(value), 0); err != nil {
			return nil, err
		}
		previous = value
	}
	if err := builder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodeManagedStringTable(values []string) []byte {
	values = uniqueManagedStrings(values)
	indexSize := 8 + (len(values)+1)*8
	output := make([]byte, indexSize)
	binary.LittleEndian.PutUint32(output[0:4], uint32(len(values)))
	cursor := uint64(0)
	for index, value := range values {
		binary.LittleEndian.PutUint64(output[8+index*8:16+index*8], cursor)
		output = append(output, []byte(value)...)
		cursor += uint64(len(value))
	}
	end := 8 + len(values)*8
	binary.LittleEndian.PutUint64(output[end:end+8], cursor)
	return output
}

func managedIPv4PrefixRange(prefix netip.Prefix) managedIPv4Range {
	prefix = prefix.Masked()
	bytes := prefix.Addr().As4()
	start := binary.BigEndian.Uint32(bytes[:])
	hostBits := 32 - prefix.Bits()
	end := start
	if hostBits == 32 {
		end = math.MaxUint32
	} else if hostBits > 0 {
		end = start | uint32((uint64(1)<<hostBits)-1)
	}
	return managedIPv4Range{Start: start, End: end}
}

func managedIPv6PrefixRange(prefix netip.Prefix) managedIPv6Range {
	prefix = prefix.Masked()
	bytes := prefix.Addr().As16()
	hi := binary.BigEndian.Uint64(bytes[:8])
	lo := binary.BigEndian.Uint64(bytes[8:])
	hostBits := 128 - prefix.Bits()
	endHi, endLo := hi, lo
	switch {
	case hostBits >= 128:
		endHi, endLo = math.MaxUint64, math.MaxUint64
	case hostBits >= 64:
		endLo = math.MaxUint64
		bits := hostBits - 64
		if bits == 64 {
			endHi = math.MaxUint64
		} else if bits > 0 {
			endHi = hi | (uint64(1)<<bits - 1)
		}
	case hostBits > 0:
		endLo = lo | (uint64(1)<<hostBits - 1)
	}
	return managedIPv6Range{StartHi: hi, StartLo: lo, EndHi: endHi, EndLo: endLo}
}

func mergeManagedIPv4Ranges(ranges []managedIPv4Range) []managedIPv4Range {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start || ranges[i].Start == ranges[j].Start && ranges[i].End < ranges[j].End })
	merged := make([]managedIPv4Range, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		adjacent := last.End != math.MaxUint32 && current.Start == last.End+1
		if current.Start <= last.End || adjacent {
			if current.End > last.End {
				last.End = current.End
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func mergeManagedIPv6Ranges(ranges []managedIPv6Range) []managedIPv6Range {
	less := func(leftHi, leftLo, rightHi, rightLo uint64) bool { return leftHi < rightHi || leftHi == rightHi && leftLo < rightLo }
	lessEqual := func(leftHi, leftLo, rightHi, rightLo uint64) bool { return !less(rightHi, rightLo, leftHi, leftLo) }
	sort.Slice(ranges, func(i, j int) bool { return less(ranges[i].StartHi, ranges[i].StartLo, ranges[j].StartHi, ranges[j].StartLo) })
	merged := make([]managedIPv6Range, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		adjacentHi, adjacentLo := last.EndHi, last.EndLo
		canAdvance := adjacentHi != math.MaxUint64 || adjacentLo != math.MaxUint64
		if canAdvance {
			if adjacentLo == math.MaxUint64 {
				adjacentHi++
				adjacentLo = 0
			} else {
				adjacentLo++
			}
		}
		if lessEqual(current.StartHi, current.StartLo, last.EndHi, last.EndLo) || canAdvance && current.StartHi == adjacentHi && current.StartLo == adjacentLo {
			if less(last.EndHi, last.EndLo, current.EndHi, current.EndLo) {
				last.EndHi, last.EndLo = current.EndHi, current.EndLo
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func encodeManagedIPv4Ranges(ranges []managedIPv4Range) []byte {
	output := make([]byte, 8, 8+len(ranges)*8)
	binary.LittleEndian.PutUint32(output[0:4], uint32(len(ranges)))
	for _, value := range ranges {
		var encoded [8]byte
		binary.LittleEndian.PutUint32(encoded[0:4], value.Start)
		binary.LittleEndian.PutUint32(encoded[4:8], value.End)
		output = append(output, encoded[:]...)
	}
	return output
}

func encodeManagedIPv6Ranges(ranges []managedIPv6Range) []byte {
	output := make([]byte, 8, 8+len(ranges)*32)
	binary.LittleEndian.PutUint32(output[0:4], uint32(len(ranges)))
	for _, value := range ranges {
		var encoded [32]byte
		binary.LittleEndian.PutUint64(encoded[0:8], value.StartLo)
		binary.LittleEndian.PutUint64(encoded[8:16], value.StartHi)
		binary.LittleEndian.PutUint64(encoded[16:24], value.EndLo)
		binary.LittleEndian.PutUint64(encoded[24:32], value.EndHi)
		output = append(output, encoded[:]...)
	}
	return output
}

func uniqueManagedStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:0]
	previous := ""
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		result = append(result, value)
		previous = value
	}
	return result
}

func alignManagedRule8(value int) int { return (value + 7) &^ 7 }

func truncateManagedUTF8(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	for len(value) > maximum {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
