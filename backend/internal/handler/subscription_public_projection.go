package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	maxSubscriptionFilterValues = 32
	maxSubscriptionFilterValue  = 80
	maxSubscriptionFilterQuery  = 100
)

var subscriptionFilterCodePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
var subscriptionFilterTagPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._:-][a-z0-9]+)*$`)

type subscriptionProjectionFilter struct {
	Plans       map[string]struct{}
	SKUs        map[string]struct{}
	NodeGroups  map[string]struct{}
	Protocols   map[string]struct{}
	Regions     map[string]struct{}
	Tags        map[string]struct{}
	ExcludeTags map[string]struct{}
	Query       string
}

type subscriptionProjectionSource struct {
	PlanSlug      string
	SKUCode       string
	NodeGroupCode string
}

func hasSubscriptionProjectionFilters(values url.Values) bool {
	for _, key := range []string{"plan", "sku", "node_group", "protocol", "region", "tag", "exclude_tag", "q"} {
		if _, exists := values[key]; exists {
			return true
		}
	}
	return false
}

func parseSubscriptionProjectionFilter(values url.Values, protocolSupported func(string) bool) (subscriptionProjectionFilter, error) {
	var result subscriptionProjectionFilter
	var err error
	if result.Plans, err = parseSubscriptionFilterSet(values, "plan", subscriptionFilterCodePattern); err != nil {
		return result, err
	}
	if result.SKUs, err = parseSubscriptionFilterSet(values, "sku", subscriptionFilterCodePattern); err != nil {
		return result, err
	}
	if result.NodeGroups, err = parseSubscriptionFilterSet(values, "node_group", subscriptionFilterCodePattern); err != nil {
		return result, err
	}
	if result.Protocols, err = parseSubscriptionFilterSet(values, "protocol", subscriptionFilterCodePattern); err != nil {
		return result, err
	}
	for protocol := range result.Protocols {
		if protocolSupported == nil || !protocolSupported(protocol) {
			return result, validationError("订阅链接筛选条件无效。", map[string]string{"protocol": fmt.Sprintf("不支持协议 %q。", protocol)})
		}
	}
	if result.Regions, err = parseSubscriptionRegionSet(values, "region"); err != nil {
		return result, err
	}
	if result.Tags, err = parseSubscriptionFilterSet(values, "tag", subscriptionFilterTagPattern); err != nil {
		return result, err
	}
	if result.ExcludeTags, err = parseSubscriptionFilterSet(values, "exclude_tag", subscriptionFilterTagPattern); err != nil {
		return result, err
	}
	result.Query = strings.ToLower(strings.TrimSpace(values.Get("q")))
	if len([]byte(result.Query)) > maxSubscriptionFilterQuery {
		return result, validationError("订阅链接筛选条件无效。", map[string]string{"q": "名称关键词不能超过 100 个 UTF-8 字节。"})
	}
	if containsControlRune(result.Query) {
		return result, validationError("订阅链接筛选条件无效。", map[string]string{"q": "名称关键词不能包含控制字符。"})
	}
	return result, nil
}

func parseSubscriptionFilterSet(values url.Values, key string, pattern *regexp.Regexp) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, raw := range values[key] {
		for _, item := range strings.Split(raw, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			if len([]byte(item)) > maxSubscriptionFilterValue || pattern == nil || !pattern.MatchString(item) {
				return nil, validationError("订阅链接筛选条件无效。", map[string]string{key: fmt.Sprintf("筛选值 %q 格式无效。", item)})
			}
			result[item] = struct{}{}
			if len(result) > maxSubscriptionFilterValues {
				return nil, validationError("订阅链接筛选条件无效。", map[string]string{key: fmt.Sprintf("同一筛选条件最多允许 %d 个值。", maxSubscriptionFilterValues)})
			}
		}
	}
	return result, nil
}

func parseSubscriptionRegionSet(values url.Values, key string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, raw := range values[key] {
		for _, item := range strings.Split(raw, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			if len([]byte(item)) > maxSubscriptionFilterValue || !utf8.ValidString(item) || containsControlRune(item) {
				return nil, validationError("订阅链接筛选条件无效。", map[string]string{key: fmt.Sprintf("区域值 %q 格式无效。", item)})
			}
			result[item] = struct{}{}
			if len(result) > maxSubscriptionFilterValues {
				return nil, validationError("订阅链接筛选条件无效。", map[string]string{key: fmt.Sprintf("同一筛选条件最多允许 %d 个值。", maxSubscriptionFilterValues)})
			}
		}
	}
	return result, nil
}

func containsControlRune(value string) bool {
	for _, current := range value {
		if unicode.IsControl(current) {
			return true
		}
	}
	return false
}

func (filter subscriptionProjectionFilter) matchesSource(source subscriptionProjectionSource) bool {
	return matchesSubscriptionFilterValue(filter.Plans, source.PlanSlug) &&
		matchesSubscriptionFilterValue(filter.SKUs, source.SKUCode) &&
		matchesSubscriptionFilterValue(filter.NodeGroups, source.NodeGroupCode)
}

func matchesSubscriptionFilterValue(values map[string]struct{}, value string) bool {
	if len(values) == 0 {
		return true
	}
	_, exists := values[strings.ToLower(strings.TrimSpace(value))]
	return exists
}

func (filter subscriptionProjectionFilter) matchesEndpoint(endpoint model.ProtocolEndpoint, node model.Node) bool {
	if !matchesSubscriptionFilterValue(filter.Protocols, endpoint.Protocol) || !matchesSubscriptionFilterValue(filter.Regions, node.Region) {
		return false
	}
	if filter.Query != "" && !strings.Contains(strings.ToLower(endpoint.Name), filter.Query) {
		return false
	}
	tags := normalizedProtocolEndpointTags(endpoint.Tags)
	if len(filter.Tags) > 0 && !setsIntersect(filter.Tags, tags) {
		return false
	}
	return len(filter.ExcludeTags) == 0 || !setsIntersect(filter.ExcludeTags, tags)
}

func normalizedProtocolEndpointTags(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return result
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return result
	}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func setsIntersect(left, right map[string]struct{}) bool {
	for value := range left {
		if _, exists := right[value]; exists {
			return true
		}
	}
	return false
}

func filterSubscriptionsForProjection(subscriptions []model.Subscription, sources map[uint]subscriptionProjectionSource, filter subscriptionProjectionFilter) []model.Subscription {
	result := make([]model.Subscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if filter.matchesSource(sources[subscription.ID]) {
			result = append(result, subscription)
		}
	}
	return result
}

func validateSubscriptionProjectionSources(filter subscriptionProjectionFilter, sources map[uint]subscriptionProjectionSource) error {
	availablePlans := make(map[string]struct{})
	availableSKUs := make(map[string]struct{})
	availableGroups := make(map[string]struct{})
	for _, source := range sources {
		availablePlans[strings.ToLower(source.PlanSlug)] = struct{}{}
		availableSKUs[strings.ToLower(source.SKUCode)] = struct{}{}
		availableGroups[strings.ToLower(source.NodeGroupCode)] = struct{}{}
	}
	for field, pair := range map[string][2]map[string]struct{}{
		"plan":       {filter.Plans, availablePlans},
		"sku":        {filter.SKUs, availableSKUs},
		"node_group": {filter.NodeGroups, availableGroups},
	} {
		for requested := range pair[0] {
			if _, exists := pair[1][requested]; !exists {
				return validationError("订阅链接筛选条件无效。", map[string]string{field: "一个或多个筛选值不属于当前令牌的有效订阅范围。"})
			}
		}
	}
	return nil
}

func (h *handlers) loadSubscriptionProjectionSources(subscriptions []model.Subscription) (map[uint]subscriptionProjectionSource, error) {
	result := make(map[uint]subscriptionProjectionSource, len(subscriptions))
	ids := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		ids = append(ids, subscription.ID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	type sourceRow struct {
		SubscriptionID uint
		PlanSlug       string
		SKUCode        string
		NodeGroupCode  string
	}
	var rows []sourceRow
	if err := h.db.Table("subscriptions").
		Select("subscriptions.id AS subscription_id, plans.slug AS plan_slug, plan_skus.code AS sku_code, node_groups.code AS node_group_code").
		Joins("JOIN plans ON plans.id = subscriptions.plan_id").
		Joins("JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
		Joins("JOIN node_groups ON node_groups.id = subscriptions.node_group_id").
		Where("subscriptions.id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.SubscriptionID] = subscriptionProjectionSource{
			PlanSlug: row.PlanSlug, SKUCode: row.SKUCode, NodeGroupCode: row.NodeGroupCode,
		}
	}
	return result, nil
}

func (h *handlers) FilteredClientSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if !hasSubscriptionProjectionFilters(r.URL.Query()) {
		h.ClientSubscriptionHandler(w, r)
		return
	}

	rawToken := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/client/subscription/"))
	if rawToken == "" || strings.Contains(rawToken, "/") {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	var access model.SubscriptionToken
	if err := h.db.Where("token_hash = ? AND revoked_at IS NULL", hashSubscriptionToken(rawToken)).First(&access).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	var user model.User
	if err := h.db.Where("id = ? AND status = ?", access.UserID, userStatusActive).First(&user).Error; err != nil {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}

	filter, err := parseSubscriptionProjectionFilter(r.URL.Query(), h.isProtocolSupported)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	now := time.Now().UTC()
	if err := expireSubscriptions(h.db, access.UserID, now); err != nil {
		ServerError(w, err)
		return
	}
	allSubscriptions := make([]model.Subscription, 0)
	if err := h.db.Where(
		"user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total",
		access.UserID, subStatusActive, now,
	).Order("end_at asc, id asc").Find(&allSubscriptions).Error; err != nil {
		ServerError(w, err)
		return
	}
	if len(allSubscriptions) == 0 {
		Forbidden(w, "subscription is inactive, expired, or out of traffic")
		return
	}
	sources, err := h.loadSubscriptionProjectionSources(allSubscriptions)
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := validateSubscriptionProjectionSources(filter, sources); err != nil {
		BadRequestError(w, err)
		return
	}
	subscriptions := filterSubscriptionsForProjection(allSubscriptions, sources, filter)
	if len(subscriptions) > 0 {
		if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
			ServerError(w, err)
			return
		}
	}

	manifestNodes, err := h.buildProjectedSubscriptionManifestNodes(subscriptions, filter, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	if err := h.sortSubscriptionManifestNodes(subscriptions, manifestNodes); err != nil {
		ServerError(w, fmt.Errorf("resolve subscription delivery order: %w", err))
		return
	}

	var total, used int64
	var expiresAt time.Time
	for _, subscription := range allSubscriptions {
		total += subscription.FlowTotal
		used += subscription.FlowUsed
		if subscription.EndAt.After(expiresAt) {
			expiresAt = subscription.EndAt
		}
	}
	_ = h.db.Model(&access).Update("last_used_at", now).Error
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", used, total, expiresAt.Unix()))
	manifest := subscriptionManifest{
		Version:     "zboard.subscription/v1",
		GeneratedAt: now.Format(time.RFC3339),
		Subscription: subscriptionManifestSummary{
			ExpiresAt: expiresAt.Format(time.RFC3339), FlowTotal: total, FlowUsed: used, FlowRemaining: total - used,
		},
		ProtocolEndpoints: manifestNodes,
	}
	h.writeProjectedSubscription(w, r, manifest)
}

func (h *handlers) buildProjectedSubscriptionManifestNodes(subscriptions []model.Subscription, filter subscriptionProjectionFilter, now time.Time) ([]subscriptionManifestNode, error) {
	manifestNodes := make([]subscriptionManifestNode, 0)
	if len(subscriptions) == 0 {
		return manifestNodes, nil
	}
	subscriptionIDs := make([]uint, 0, len(subscriptions))
	nodeGroupIDs := make([]uint, 0, len(subscriptions))
	subscriptionRank := make(map[uint]int, len(subscriptions))
	subscriptionGroup := make(map[uint]uint, len(subscriptions))
	for index, subscription := range subscriptions {
		subscriptionIDs = append(subscriptionIDs, subscription.ID)
		nodeGroupIDs = append(nodeGroupIDs, subscription.NodeGroupID)
		subscriptionRank[subscription.ID] = index
		subscriptionGroup[subscription.ID] = subscription.NodeGroupID
	}

	type membershipRow struct {
		NodeGroupID        uint
		ProtocolEndpointID uint
	}
	var membershipRows []membershipRow
	if err := h.db.Table("node_group_endpoints").
		Select("node_group_id, protocol_endpoint_id").
		Where("node_group_id IN ?", uniqueUintIDs(nodeGroupIDs)).
		Scan(&membershipRows).Error; err != nil {
		return nil, err
	}
	memberships := make(map[uint]map[uint]struct{})
	for _, row := range membershipRows {
		if memberships[row.NodeGroupID] == nil {
			memberships[row.NodeGroupID] = make(map[uint]struct{})
		}
		memberships[row.NodeGroupID][row.ProtocolEndpointID] = struct{}{}
	}

	var credentials []model.ProtocolCredential
	if err := h.db.Where("subscription_id IN ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", uniqueUintIDs(subscriptionIDs), protocolCredentialStatusActive, now).
		Find(&credentials).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(credentials, func(left, right int) bool {
		leftRank := subscriptionRank[credentials[left].SubscriptionID]
		rightRank := subscriptionRank[credentials[right].SubscriptionID]
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if credentials[left].ProtocolEndpointID != credentials[right].ProtocolEndpointID {
			return credentials[left].ProtocolEndpointID < credentials[right].ProtocolEndpointID
		}
		return credentials[left].ID < credentials[right].ID
	})
	seenEndpoints := make(map[uint]struct{})
	for _, credential := range credentials {
		groupID := subscriptionGroup[credential.SubscriptionID]
		if _, member := memberships[groupID][credential.ProtocolEndpointID]; !member {
			continue
		}
		if _, seen := seenEndpoints[credential.ProtocolEndpointID]; seen {
			continue
		}
		var endpoint model.ProtocolEndpoint
		if err := h.db.Where("id = ? AND is_active = ?", credential.ProtocolEndpointID, true).First(&endpoint).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if !h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		var node model.Node
		if err := h.db.Select("id", "region", "last_seen_at", "is_enabled").First(&node, endpoint.NodeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if !node.IsEnabled || node.LastSeenAt == nil || node.LastSeenAt.Before(now.Add(-nodeOnlineWindow)) || !filter.matchesEndpoint(endpoint, node) {
			continue
		}
		if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, node); !supported {
			continue
		}
		clientConfig, err := h.credentialClientConfig(endpoint, credential)
		if err != nil {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: credential.SubscriptionID,
			CredentialID: credential.CredentialID, Name: endpoint.Name, Region: node.Region,
			Address: endpoint.Address, Port: credential.ListenPort, PublicPort: credential.PublicPort,
			Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli, Config: clientConfig,
		})
		seenEndpoints[endpoint.ID] = struct{}{}
	}

	var endpoints []model.ProtocolEndpoint
	if err := h.db.Model(&model.ProtocolEndpoint{}).
		Select("DISTINCT protocol_endpoints.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id").
		Joins("JOIN nodes ON nodes.id = protocol_endpoints.node_id").
		Where("node_group_endpoints.node_group_id IN ? AND protocol_endpoints.is_active = ? AND nodes.last_seen_at >= ? AND nodes.is_enabled = ? AND protocol_endpoints.client_config <> ''", uniqueUintIDs(nodeGroupIDs), true, now.Add(-nodeOnlineWindow), true).
		Order("protocol_endpoints.sort_order asc, protocol_endpoints.id asc").Find(&endpoints).Error; err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		if _, seen := seenEndpoints[endpoint.ID]; seen || h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		var node model.Node
		if err := h.db.Select("id", "region").First(&node, endpoint.NodeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if !filter.matchesEndpoint(endpoint, node) {
			continue
		}
		if supported, _ := h.protocolKernelSupportForNode(endpoint.Protocol, node); !supported {
			continue
		}
		clientConfig, err := h.endpointSubscriptionClientConfig(endpoint)
		if err != nil {
			continue
		}
		manifestNodes = append(manifestNodes, subscriptionManifestNode{
			ID: endpoint.ID, NodeID: endpoint.NodeID, Name: endpoint.Name, Region: node.Region,
			Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort, Protocol: endpoint.Protocol,
			MultiplierMilli: endpoint.MultiplierMilli, Config: clientConfig,
		})
		seenEndpoints[endpoint.ID] = struct{}{}
	}
	return manifestNodes, nil
}

func (h *handlers) writeProjectedSubscription(w http.ResponseWriter, r *http.Request, manifest subscriptionManifest) {
	delivery := resolveSubscriptionDelivery(r.URL.Query().Get("template"), r.UserAgent())
	if delivery.UsesUserAgent {
		w.Header().Add("Vary", "User-Agent")
	}
	if delivery.TemplateSlug != "" {
		var err error
		if len(manifest.ProtocolEndpoints) == 0 {
			err = h.writeEmptySubscriptionTemplate(r, w, delivery.TemplateSlug, manifest)
		} else {
			err = h.writeSubscriptionTemplate(r.Context(), w, delivery.TemplateSlug, manifest)
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if delivery.UsesUserAgent {
					if err := writeBase64SubscriptionManifest(w, manifest, subscriptionDeliveryNative); err != nil {
						ServerError(w, err)
					}
					return
				}
				NotFound(w)
				return
			}
			ServerError(w, fmt.Errorf("render subscription template: %w", err))
		}
		return
	}
	if err := writeBase64SubscriptionManifest(w, manifest, delivery.Format); err != nil {
		ServerError(w, err)
	}
}

func (h *handlers) writeEmptySubscriptionTemplate(r *http.Request, w http.ResponseWriter, slug string, manifest subscriptionManifest) error {
	var item model.SubscriptionTemplate
	if err := h.db.Where("slug = ? AND is_active = ?", slug, true).First(&item).Error; err != nil {
		return err
	}
	customization, _, err := normalizeSubscriptionCustomization(item.Renderer, item.Customization)
	if err != nil {
		return err
	}
	customization.PolicyGroups = nil
	customization.RuleSets = nil
	customization.Final = subscriptionTargetDirect
	customization.AdvancedSource = ""
	data := subscriptionTemplateData{
		Version: manifest.Version, GeneratedAt: manifest.GeneratedAt, Subscription: manifest.Subscription,
		ProtocolEndpoints: []subscriptionTemplateEndpoint{},
	}
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err == nil {
		data.SiteName = installation.SiteName
	}
	definition, ok := subscriptionRenderer(item.Renderer)
	if !ok {
		return fmt.Errorf("unsupported subscription renderer %q", item.Renderer)
	}
	rendered, err := definition.render(data, customization)
	if err != nil {
		return err
	}
	if err := h.validateZeroSubscriptionPreview(r.Context(), item.Renderer, rendered); err != nil {
		return err
	}
	rendered, contentType, deliveryFormat := encodeSubscriptionTemplateDelivery(item.Renderer, rendered, definition.contentType)
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("X-Zboard-Subscription-Template", item.Slug)
	w.Header().Set("X-Zboard-Subscription-Format", deliveryFormat)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(rendered))
	return err
}
