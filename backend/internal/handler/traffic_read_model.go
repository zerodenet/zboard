package handler

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	entityReferenceLimit = 200
	trafficTrendMaxDays  = 366
)

type entityReference = observability.EntityReference

type entityReferenceResponse struct {
	Users             map[string]entityReference `json:"users"`
	Subscriptions     map[string]entityReference `json:"subscriptions"`
	Nodes             map[string]entityReference `json:"nodes"`
	ProtocolEndpoints map[string]entityReference `json:"protocol_endpoints"`
	Plans             map[string]entityReference `json:"plans"`
	PlanSKUs          map[string]entityReference `json:"plan_skus"`
	Orders            map[string]entityReference `json:"orders"`
	Targets           map[string]entityReference `json:"targets"`
}

type trafficTrendAggregateRow = metering.TrafficTrendAggregate
type trafficTrendPoint = metering.TrafficTrendPoint

type trafficTrendResponse struct {
	From                  string              `json:"from"`
	To                    string              `json:"to"`
	Points                []trafficTrendPoint `json:"points"`
	RecordCount           int64               `json:"record_count"`
	ConnectionSampleCount int64               `json:"connection_sample_count"`
	PeakConnections       *int64              `json:"peak_connections"`
	Truncated             bool                `json:"truncated"`
	Subscriptions         []entityReference   `json:"subscriptions"`
	AsOf                  time.Time           `json:"as_of"`
}

type requestedAuditTarget struct {
	Raw  string
	Kind string
	ID   uint
}

func entityKey(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func entityKindLabel(kind string) string {
	switch normalizeEntityKind(kind) {
	case "user":
		return "用户"
	case "subscription":
		return "订阅"
	case "node":
		return "节点"
	case "protocol_endpoint":
		return "协议端点"
	case "plan":
		return "套餐"
	case "plan_sku":
		return "套餐规格"
	case "order":
		return "订单"
	default:
		value := strings.TrimSpace(strings.ReplaceAll(kind, "_", " "))
		if value == "" {
			return "操作目标"
		}
		return value
	}
}

func missingEntityReference(kind string, id uint) entityReference {
	normalized := normalizeEntityKind(kind)
	label := entityKindLabel(normalized)
	return entityReference{
		ID:          id,
		Kind:        normalized,
		DisplayName: "已删除的" + label,
		Secondary:   "名称不可用",
		Missing:     true,
	}
}

func normalizeEntityKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "users", "user":
		return "user"
	case "subscriptions", "subscription":
		return "subscription"
	case "nodes", "node":
		return "node"
	case "protocol-endpoints", "protocol_endpoints", "protocol_endpoint", "endpoint", "endpoints":
		return "protocol_endpoint"
	case "plans", "plan":
		return "plan"
	case "plan-skus", "plan_skus", "plan_sku", "sku", "skus":
		return "plan_sku"
	case "orders", "order":
		return "order"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func parseRequestedTarget(raw string) requestedAuditTarget {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return requestedAuditTarget{Raw: raw, Kind: normalizeEntityKind(raw)}
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || parsed == 0 {
		return requestedAuditTarget{Raw: raw, Kind: normalizeEntityKind(parts[0])}
	}
	return requestedAuditTarget{Raw: raw, Kind: normalizeEntityKind(parts[0]), ID: uint(parsed)}
}

func appendEntityIDs(values url.Values, key string, target map[uint]struct{}) error {
	for _, raw := range values[key] {
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parsed, err := strconv.ParseUint(item, 10, 64)
			if err != nil || parsed == 0 {
				return fmt.Errorf("%s contains an invalid id", key)
			}
			target[uint(parsed)] = struct{}{}
			if len(target) > entityReferenceLimit {
				return fmt.Errorf("%s accepts at most %d ids", key, entityReferenceLimit)
			}
		}
	}
	return nil
}

func sortedEntityIDs(values map[uint]struct{}) []uint {
	result := make([]uint, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func prefillEntityReferences(kind string, ids map[uint]struct{}) map[string]entityReference {
	result := make(map[string]entityReference, len(ids))
	for id := range ids {
		result[entityKey(id)] = missingEntityReference(kind, id)
	}
	return result
}

func (h *handlers) AdminEntityReferencesHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}

	sets := map[string]map[uint]struct{}{
		"user":              {},
		"subscription":      {},
		"node":              {},
		"protocol_endpoint": {},
		"plan":              {},
		"plan_sku":          {},
		"order":             {},
	}
	keys := map[string]string{
		"user_ids":              "user",
		"subscription_ids":      "subscription",
		"node_ids":              "node",
		"protocol_endpoint_ids": "protocol_endpoint",
		"plan_ids":              "plan",
		"plan_sku_ids":          "plan_sku",
		"order_ids":             "order",
	}
	for queryKey, kind := range keys {
		if err := appendEntityIDs(r.URL.Query(), queryKey, sets[kind]); err != nil {
			BadRequest(w, err.Error())
			return
		}
	}

	targets := make([]requestedAuditTarget, 0)
	for _, raw := range r.URL.Query()["targets"] {
		for _, item := range strings.Split(raw, ",") {
			if strings.TrimSpace(item) == "" {
				continue
			}
			target := parseRequestedTarget(item)
			targets = append(targets, target)
			if len(targets) > entityReferenceLimit {
				BadRequest(w, fmt.Sprintf("targets accepts at most %d values", entityReferenceLimit))
				return
			}
			if target.ID > 0 {
				if set, exists := sets[target.Kind]; exists {
					set[target.ID] = struct{}{}
				}
			}
		}
	}

	response := entityReferenceResponse{
		Users:             prefillEntityReferences("user", sets["user"]),
		Subscriptions:     prefillEntityReferences("subscription", sets["subscription"]),
		Nodes:             prefillEntityReferences("node", sets["node"]),
		ProtocolEndpoints: prefillEntityReferences("protocol_endpoint", sets["protocol_endpoint"]),
		Plans:             prefillEntityReferences("plan", sets["plan"]),
		PlanSKUs:          prefillEntityReferences("plan_sku", sets["plan_sku"]),
		Orders:            prefillEntityReferences("order", sets["order"]),
		Targets:           make(map[string]entityReference, len(targets)),
	}
	if err := h.services.EntityReferences.Resolve(r.Context(), observability.EntityReferenceRequest{
		Users: sortedEntityIDs(sets["user"]), Subscriptions: sortedEntityIDs(sets["subscription"]),
		Nodes: sortedEntityIDs(sets["node"]), ProtocolEndpoints: sortedEntityIDs(sets["protocol_endpoint"]),
		Plans: sortedEntityIDs(sets["plan"]), PlanSKUs: sortedEntityIDs(sets["plan_sku"]), Orders: sortedEntityIDs(sets["order"]),
	}, observability.EntityReferenceData{
		Users: response.Users, Subscriptions: response.Subscriptions, Nodes: response.Nodes,
		ProtocolEndpoints: response.ProtocolEndpoints, Plans: response.Plans, PlanSKUs: response.PlanSKUs, Orders: response.Orders,
	}); err != nil {
		ServerError(w, err)
		return
	}

	for _, target := range targets {
		if target.Raw == "" {
			continue
		}
		if target.ID == 0 {
			response.Targets[target.Raw] = entityReference{Kind: target.Kind, DisplayName: entityKindLabel(target.Kind), Secondary: target.Raw}
			continue
		}
		var resolved entityReference
		switch target.Kind {
		case "user":
			resolved = response.Users[entityKey(target.ID)]
		case "subscription":
			resolved = response.Subscriptions[entityKey(target.ID)]
		case "node":
			resolved = response.Nodes[entityKey(target.ID)]
		case "protocol_endpoint":
			resolved = response.ProtocolEndpoints[entityKey(target.ID)]
		case "plan":
			resolved = response.Plans[entityKey(target.ID)]
		case "plan_sku":
			resolved = response.PlanSKUs[entityKey(target.ID)]
		case "order":
			resolved = response.Orders[entityKey(target.ID)]
		default:
			resolved = entityReference{ID: target.ID, Kind: target.Kind, DisplayName: entityKindLabel(target.Kind), Secondary: target.Raw}
		}
		response.Targets[target.Raw] = resolved
	}

	OK(w, response)
}

func parseTrafficTrendRange(values url.Values, now time.Time) (time.Time, time.Time, int, error) {
	today := now.UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -6)
	to := today
	var err error
	if raw := strings.TrimSpace(values.Get("from")); raw != "" {
		from, err = time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, 0, fmt.Errorf("from must use YYYY-MM-DD")
		}
	}
	if raw := strings.TrimSpace(values.Get("to")); raw != "" {
		to, err = time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, 0, fmt.Errorf("to must use YYYY-MM-DD")
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("to must not be earlier than from")
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if days > trafficTrendMaxDays {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("traffic trend range cannot exceed %d days", trafficTrendMaxDays)
	}
	return from, to, days, nil
}

func positiveQueryID(values url.Values, key string) (uint, error) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return uint(parsed), nil
}

func buildTrafficTrendPoints(from time.Time, days int, rows []trafficTrendAggregateRow) ([]trafficTrendPoint, int64) {
	return metering.BuildTrafficTrendPoints(from, days, rows)
}
