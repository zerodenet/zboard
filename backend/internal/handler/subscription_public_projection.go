package handler

import (
	"context"
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

func (h *handlers) loadSubscriptionProjectionSources(ctx context.Context, subscriptions []model.Subscription) (map[uint]subscriptionProjectionSource, error) {
	result := make(map[uint]subscriptionProjectionSource, len(subscriptions))
	ids := make([]uint, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		ids = append(ids, subscription.ID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := h.services.SubscriptionProjection.Sources(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.SubscriptionID] = subscriptionProjectionSource{
			PlanSlug: row.PlanSlug, SKUCode: row.SKUCode, NodeGroupCode: row.NodeGroupCode,
		}
	}
	return result, nil
}

func (h *handlers) buildProjectedSubscriptionManifestNodes(ctx context.Context, subscriptions []model.Subscription, filter subscriptionProjectionFilter, now time.Time) ([]subscriptionManifestNode, error) {
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

	projection, err := h.services.SubscriptionProjection.Load(ctx, uniqueUintIDs(subscriptionIDs), uniqueUintIDs(nodeGroupIDs), now)
	if err != nil {
		return nil, err
	}
	memberships := make(map[uint]map[uint]struct{})
	for groupID, endpointIDs := range projection.Memberships {
		if memberships[groupID] == nil {
			memberships[groupID] = make(map[uint]struct{})
		}
		for _, endpointID := range endpointIDs {
			memberships[groupID][endpointID] = struct{}{}
		}
	}
	credentials := make([]model.ProtocolCredential, 0, len(projection.Credentials))
	for _, row := range projection.Credentials {
		credentials = append(credentials, model.ProtocolCredential{
			ID: row.ID, SubscriptionID: row.SubscriptionID, UserID: row.UserID, ProtocolEndpointID: row.ProtocolEndpointID, NodeID: row.NodeID,
			CredentialID: row.CredentialID, PrincipalKey: row.PrincipalKey, Secret: row.SecretCiphertext,
			ListenPort: row.ListenPort, PublicPort: row.PublicPort, Status: row.Status, ExpiresAt: row.ExpiresAt,
			LastUsedAt: row.LastUsedAt, RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
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
	endpoints := make([]model.ProtocolEndpoint, 0, len(projection.Endpoints))
	endpointByID := make(map[uint]model.ProtocolEndpoint, len(projection.Endpoints))
	for _, row := range projection.Endpoints {
		endpoint := model.ProtocolEndpoint{
			ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Address: row.Address,
			Port: row.Port, PublicPort: row.PublicPort, MultiplierMilli: row.MultiplierMilli,
			ManagedPrincipalReady: row.ManagedPrincipalReady, MieruPrincipalReady: row.MieruPrincipalReady,
			ServerConfig: row.ServerConfig, ClientConfig: row.ClientConfig, Tags: row.Tags, IsActive: true, SortOrder: row.SortOrder,
		}
		endpoints = append(endpoints, endpoint)
		endpointByID[endpoint.ID] = endpoint
	}
	nodes := make(map[uint]model.Node, len(projection.Nodes))
	for id, row := range projection.Nodes {
		nodes[id] = model.Node{ID: row.ID, Region: row.Region, IsEnabled: row.IsEnabled, LastSeenAt: row.LastSeenAt}
	}
	seenEndpoints := make(map[uint]struct{})
	for _, credential := range credentials {
		groupID := subscriptionGroup[credential.SubscriptionID]
		if _, member := memberships[groupID][credential.ProtocolEndpointID]; !member {
			continue
		}
		if _, seen := seenEndpoints[credential.ProtocolEndpointID]; seen {
			continue
		}
		endpoint, exists := endpointByID[credential.ProtocolEndpointID]
		if !exists {
			continue
		}
		if !h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		node, exists := nodes[endpoint.NodeID]
		if !exists {
			continue
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

	for _, endpoint := range endpoints {
		if _, seen := seenEndpoints[endpoint.ID]; seen || h.endpointDeliversSubscriptionCredential(endpoint) {
			continue
		}
		node, exists := nodes[endpoint.NodeID]
		if !exists || !node.IsEnabled || node.LastSeenAt == nil || node.LastSeenAt.Before(now.Add(-nodeOnlineWindow)) || strings.TrimSpace(endpoint.ClientConfig) == "" {
			continue
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
	fronts, err := h.buildAuthorizedNetworkEntries(ctx, subscriptions, filter, now)
	if err != nil {
		return nil, err
	}
	return append(manifestNodes, fronts...), nil
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
	source, err := h.services.SubscriptionTemplates.RenderSource(r.Context(), slug)
	if err != nil {
		return err
	}
	item := subscriptionTemplateModel(source.Template)
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
	data.SiteName = source.SiteName
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
