package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	managedRuleSetRenderer          = "managed"
	managedRuleSourceZeroRuleIR     = "zero_rule_ir"
	managedRuleSourceDomainList     = "domain_list"
	managedRuleSourceCIDRList       = "cidr_list"
	managedRuleSourceClashClassical = "clash_classical"
	managedRuleSetFormatCanonical   = managedRuleSourceZeroRuleIR
	managedRuleSourceFileName       = "source.json"
	managedRuleMaxSourceBytes       = 64 << 20
	managedRuleMaxRules             = 4_000_000
	managedRuleMaxValueBytes        = 4_096
	managedRuleMaxDisplayNameBytes  = 63
	managedRuleImportTimeout        = 20 * time.Second
	zeroRuleIRVersion               = 1
)

const (
	managedRuleTypeDomainExact   = "domain_exact"
	managedRuleTypeDomainSuffix  = "domain_suffix"
	managedRuleTypeDomainKeyword = "domain_keyword"
	managedRuleTypeIPv4CIDR      = "ipv4_cidr"
	managedRuleTypeIPv6CIDR      = "ipv6_cidr"
)

const (
	managedRuleArtifactZeroRuleIR         = managedRuleSourceZeroRuleIR
	managedRuleArtifactClashClassicalYAML = "clash-classical-yaml"
	managedRuleArtifactClashClassicalText = "clash-classical-text"
	managedRuleArtifactSingBoxSource      = "sing-box-source"
)

type managedRule struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type managedRuleDocument struct {
	Version uint32        `json:"version"`
	Name    *string       `json:"name,omitempty"`
	Rules   []managedRule `json:"rules"`
}

type managedRuleDocumentWire struct {
	Version uint32         `json:"version"`
	Name    *string        `json:"name,omitempty"`
	Rules   *[]managedRule `json:"rules"`
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
		return fmt.Errorf("Zero Rule IR exceeds %d bytes", managedRuleMaxSourceBytes)
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
	return os.RemoveAll(dir)
}

func encodeManagedCanonicalSource(document managedRuleDocument) []byte {
	content, _ := json.MarshalIndent(document, "", "  ")
	return append(content, '\n')
}

func managedRuleContentMetadata(content []byte) (string, int) {
	digest := sha256.Sum256(content)
	count := 0
	if document, err := decodeAndNormalizeZeroRuleIR(content); err == nil {
		count = len(document.Rules)
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
		presentation.PublicURL = managedRulePublicURL(siteURL, item.Tag, managedRuleArtifactZeroRuleIR)
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

func parseManagedRuleSetActionID(pathValue, suffix string) (uint, error) {
	trimmed := strings.TrimSuffix(pathValue, suffix)
	if trimmed == pathValue {
		return 0, errors.New("invalid rule set action path")
	}
	return parsePathID(trimmed, "/api/v1/admin/subscription-rule-sets/")
}
