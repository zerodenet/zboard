package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

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
	requestedFormat := strings.TrimSpace(r.URL.Query().Get("format"))
	usesUserAgent := requestedFormat == ""
	if requestedFormat == "" {
		requestedFormat = managedRuleFormatFromUserAgent(r.UserAgent())
	}
	format, err := normalizeManagedRuleArtifactFormat(requestedFormat)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	content, err := h.readManagedRuleSource(item.Tag)
	if err != nil {
		NotFound(w)
		return
	}
	document, err := parseManagedRuleSource(content, managedRuleSourceZeroRuleIR)
	if err != nil {
		ServerError(w, fmt.Errorf("parse managed Zero Rule IR: %w", err))
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

func publicManagedRuleSetTag(pathValue string) (string, error) {
	trimmed := strings.Trim(pathValue, "/")
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
	case strings.Contains(userAgent, "clash"):
		return managedRuleArtifactClashClassicalYAML
	case strings.Contains(userAgent, "sing-box"), strings.Contains(userAgent, "singbox"):
		return managedRuleArtifactSingBoxSource
	default:
		return managedRuleArtifactZeroRuleIR
	}
}

func (h *handlers) loadOrBuildManagedRuleArtifact(item model.SubscriptionRuleSet, document managedRuleDocument, digest [32]byte, format string) (managedRuleArtifactResponse, error) {
	if format == managedRuleArtifactZeroRuleIR {
		return managedRuleArtifactResponse{Body: encodeManagedCanonicalSource(document), ContentType: "application/json; charset=utf-8", Extension: ".json"}, nil
	}
	artifactDir, err := h.managedRuleSetDir(item.Tag)
	if err != nil {
		return managedRuleArtifactResponse{}, err
	}
	artifactDir = filepath.Join(artifactDir, "artifacts", hex.EncodeToString(digest[:]))
	response := managedRuleArtifactResponse{}
	switch format {
	case managedRuleArtifactClashClassicalYAML:
		response.ContentType, response.Extension = "application/yaml; charset=utf-8", ".yaml"
	case managedRuleArtifactClashClassicalText:
		response.ContentType, response.Extension = "text/plain; charset=utf-8", ".list"
	case managedRuleArtifactSingBoxSource:
		response.ContentType, response.Extension = "application/json; charset=utf-8", ".json"
	default:
		return managedRuleArtifactResponse{}, fmt.Errorf("unsupported rule artifact format %q", format)
	}
	pathValue := filepath.Join(artifactDir, format)
	if body, err := os.ReadFile(pathValue); err == nil {
		response.Body = body
		return response, nil
	}
	var body []byte
	switch format {
	case managedRuleArtifactClashClassicalYAML:
		body = encodeManagedRuleClashYAML(document)
	case managedRuleArtifactClashClassicalText:
		body = encodeManagedRuleClashText(document)
	case managedRuleArtifactSingBoxSource:
		body, err = encodeManagedRuleSingBox(document)
	}
	if err != nil {
		return managedRuleArtifactResponse{}, err
	}
	if err := os.MkdirAll(artifactDir, 0o750); err != nil {
		return managedRuleArtifactResponse{}, err
	}
	if err := os.WriteFile(pathValue, body, 0o640); err != nil {
		return managedRuleArtifactResponse{}, err
	}
	response.Body = body
	return response, nil
}

func managedRuleClashType(ruleType string) string {
	switch ruleType {
	case managedRuleTypeDomainExact:
		return "DOMAIN"
	case managedRuleTypeDomainSuffix:
		return "DOMAIN-SUFFIX"
	case managedRuleTypeDomainKeyword:
		return "DOMAIN-KEYWORD"
	case managedRuleTypeIPv4CIDR:
		return "IP-CIDR"
	case managedRuleTypeIPv6CIDR:
		return "IP-CIDR6"
	default:
		return ""
	}
}

func encodeManagedRuleClashText(document managedRuleDocument) []byte {
	var output strings.Builder
	for _, rule := range document.Rules {
		output.WriteString(managedRuleClashType(rule.Type))
		output.WriteByte(',')
		output.WriteString(rule.Value)
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func encodeManagedRuleClashYAML(document managedRuleDocument) []byte {
	var output strings.Builder
	output.WriteString("payload:\n")
	for _, rule := range document.Rules {
		value := managedRuleClashType(rule.Type) + "," + rule.Value
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
		switch item.Type {
		case managedRuleTypeDomainExact:
			field = "domain"
		case managedRuleTypeDomainSuffix:
			field = "domain_suffix"
		case managedRuleTypeDomainKeyword:
			field = "domain_keyword"
		case managedRuleTypeIPv4CIDR, managedRuleTypeIPv6CIDR:
			field = "ip_cidr"
		}
		values, _ := rule[field].([]string)
		rule[field] = append(values, item.Value)
	}
	rules := make([]map[string]interface{}, 0, 1)
	if len(rule) > 0 {
		rules = append(rules, rule)
	}
	content, err := json.MarshalIndent(map[string]interface{}{"version": 3, "rules": rules}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
