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
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func (h *handlers) PublicManagedRuleSetHandler(w http.ResponseWriter, r *http.Request) {
	tag, pathFormat, err := publicManagedRuleSetTarget(r.URL.Path)
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
	usesUserAgent := requestedFormat == "" && pathFormat == ""
	if pathFormat != "" {
		if requestedFormat != "" && !strings.EqualFold(requestedFormat, pathFormat) {
			BadRequest(w, "rule path format conflicts with format query")
			return
		}
		requestedFormat = pathFormat
	}
	if requestedFormat == "" {
		requestedFormat = managedRuleFormatFromUserAgent(r.UserAgent())
	}
	format := managedRuleArtifactZRS
	if !strings.EqualFold(requestedFormat, managedRuleArtifactZRS) {
		format, err = normalizeManagedRuleArtifactFormat(requestedFormat)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	if format == managedRuleArtifactZeroRuleIR {
		BadRequest(w, "Zero Rule IR is an internal source format and is not publicly published")
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
	if format == managedRuleArtifactZRS && len(document.ClientRules) > 0 {
		BadRequest(w, managedRuleClientCompatibilityMessage)
		return
	}
	digest := sha256.Sum256(content)
	artifact, err := h.loadOrBuildManagedRuleArtifact(item, document, digest, format)
	if err != nil {
		ServerError(w, err)
		return
	}
	artifactDigest := sha256.Sum256(artifact.Body)
	etag := `"` + hex.EncodeToString(artifactDigest[:]) + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
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

func publicManagedRuleSetTarget(pathValue string) (string, string, error) {
	trimmed := strings.Trim(pathValue, "/")
	const prefix = "api/v1/rules/"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", "", errors.New("invalid rule path")
	}
	rawTag := strings.TrimPrefix(trimmed, prefix)
	pathFormat := ""
	if strings.HasSuffix(rawTag, ".zrs") {
		rawTag = strings.TrimSuffix(rawTag, ".zrs")
		pathFormat = managedRuleArtifactZRS
	}
	tag, err := url.PathUnescape(rawTag)
	if err != nil || strings.Contains(tag, "/") {
		return "", "", errors.New("invalid rule tag")
	}
	_, err = managedRuleTagPath(tag)
	return tag, pathFormat, err
}

func publicManagedRuleSetTag(pathValue string) (string, error) {
	tag, _, err := publicManagedRuleSetTarget(pathValue)
	return tag, err
}

func managedRuleFormatFromUserAgent(userAgent string) string {
	userAgent = strings.ToLower(userAgent)
	switch {
	case strings.Contains(userAgent, "clash"), strings.Contains(userAgent, "mihomo"):
		return managedRuleArtifactClashClassicalYAML
	case strings.Contains(userAgent, "sing-box"), strings.Contains(userAgent, "singbox"):
		return managedRuleArtifactSingBoxSource
	default:
		return managedRuleArtifactZRS
	}
}

func (h *handlers) loadOrBuildManagedRuleArtifact(item model.SubscriptionRuleSet, document managedRuleDocument, digest [32]byte, format string) (managedRuleArtifactResponse, error) {
	response := managedRuleArtifactResponse{}
	switch format {
	case managedRuleArtifactZRS:
		response.ContentType, response.Extension = "application/octet-stream", ".zrs"
	case managedRuleArtifactClashClassicalYAML:
		response.ContentType, response.Extension = "application/yaml; charset=utf-8", ".yaml"
	case managedRuleArtifactClashClassicalText:
		response.ContentType, response.Extension = "text/plain; charset=utf-8", ".list"
	case managedRuleArtifactSingBoxSource:
		response.ContentType, response.Extension = "application/json; charset=utf-8", ".json"
	default:
		return managedRuleArtifactResponse{}, fmt.Errorf("unsupported rule artifact format %q", format)
	}
	pathValue, err := h.managedRuleArtifactPath(item.Tag, digest, format)
	if err != nil {
		return managedRuleArtifactResponse{}, err
	}
	if body, err := os.ReadFile(pathValue); err == nil {
		response.Body = body
		return response, nil
	} else if format == managedRuleArtifactZRS {
		return managedRuleArtifactResponse{}, fmt.Errorf("read precompiled ZRS artifact: %w", err)
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
	if err := writeManagedRuleFileAtomic(pathValue, body); err != nil {
		return managedRuleArtifactResponse{}, err
	}
	response.Body = body
	return response, nil
}

func managedRuleClashType(ruleType string) string {
	switch ruleType {
	case managedRuleTypeProcessName:
		return "PROCESS-NAME"
	case managedRuleTypeProcessPath:
		return "PROCESS-PATH"
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
	for _, rule := range managedRuleAllRules(document) {
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
	for _, rule := range managedRuleAllRules(document) {
		value := managedRuleClashType(rule.Type) + "," + rule.Value
		output.WriteString("  - '")
		output.WriteString(strings.ReplaceAll(value, "'", "''"))
		output.WriteString("'\n")
	}
	return []byte(output.String())
}

func encodeManagedRuleSingBox(document managedRuleDocument) ([]byte, error) {
	fields := []string{"domain", "domain_suffix", "domain_keyword", "ip_cidr", "process_name", "process_path"}
	values := make(map[string][]string)
	for _, item := range managedRuleAllRules(document) {
		field := map[string]string{
			managedRuleTypeDomainExact: "domain", managedRuleTypeDomainSuffix: "domain_suffix",
			managedRuleTypeDomainKeyword: "domain_keyword", managedRuleTypeIPv4CIDR: "ip_cidr",
			managedRuleTypeIPv6CIDR: "ip_cidr", managedRuleTypeProcessName: "process_name", managedRuleTypeProcessPath: "process_path",
		}[item.Type]
		if field == "" {
			return nil, fmt.Errorf("sing-box 不支持规则类型 %q", item.Type)
		}
		values[field] = append(values[field], item.Value)
	}
	// Separate rule objects preserve the source set's OR semantics. Combining
	// process, domain and address fields in one object introduces AND conditions.
	rules := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		if len(values[field]) > 0 {
			rules = append(rules, map[string]interface{}{field: values[field]})
		}
	}
	content, err := json.MarshalIndent(map[string]interface{}{"version": 3, "rules": rules}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
