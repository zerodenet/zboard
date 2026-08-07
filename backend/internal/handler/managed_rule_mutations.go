package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) AdminSubscriptionRuleSetContentHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseManagedRuleSetActionID(r.URL.Path, "/content")
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
	document, err := parseManagedRuleSource([]byte(req.Content), managedRuleSourceZeroRuleIR)
	if err != nil {
		BadRequestFields(w, "规则内容校验失败。", map[string]string{"content": err.Error()})
		return
	}
	normalized := encodeManagedCanonicalSource(document)
	if err := h.replaceManagedRuleContent(item, normalized, req.ExpectedRevision, claims); err != nil {
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

func (h *handlers) AdminSubscriptionRuleSetImportHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parseManagedRuleSetActionID(r.URL.Path, "/import")
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
		return createAuditLog(tx, claims, "subscription_rule_set.content.update", fmt.Sprintf("subscription_rule_set:%d", item.ID), fmt.Sprintf("bytes=%d revision=%d", len(content), locked.Revision+1))
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
			for _, resolved := range addresses {
				if !managedRuleImportAddressAllowed(resolved) {
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
