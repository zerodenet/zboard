package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	entityReferenceLimit = 200
	trafficTrendMaxDays  = 366
)

type entityReference struct {
	ID          uint   `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Secondary   string `json:"secondary,omitempty"`
	Status      string `json:"status,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

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

type trafficTrendAggregateRow struct {
	Day           string `gorm:"column:day"`
	UploadBytes   int64  `gorm:"column:upload_bytes"`
	DownloadBytes int64  `gorm:"column:download_bytes"`
	UsedBytes     int64  `gorm:"column:used_bytes"`
	RecordCount   int64  `gorm:"column:record_count"`
}

type trafficTrendPoint struct {
	Date            string `json:"date"`
	Label           string `json:"label"`
	UploadBytes     int64  `json:"upload_bytes"`
	DownloadBytes   int64  `json:"download_bytes"`
	UsedBytes       int64  `json:"used_bytes"`
	PeakConnections *int64 `json:"peak_connections"`
	RecordCount     int64  `json:"record_count"`
}

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

	if err := resolveUserReferences(h.db.WithContext(r.Context()), response.Users, sortedEntityIDs(sets["user"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolveSubscriptionReferences(h.db.WithContext(r.Context()), response.Subscriptions, sortedEntityIDs(sets["subscription"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolveNodeReferences(h.db.WithContext(r.Context()), response.Nodes, sortedEntityIDs(sets["node"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolveProtocolEndpointReferences(h.db.WithContext(r.Context()), response.ProtocolEndpoints, sortedEntityIDs(sets["protocol_endpoint"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolvePlanReferences(h.db.WithContext(r.Context()), response.Plans, sortedEntityIDs(sets["plan"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolvePlanSKUReferences(h.db.WithContext(r.Context()), response.PlanSKUs, sortedEntityIDs(sets["plan_sku"])); err != nil {
		ServerError(w, err)
		return
	}
	if err := resolveOrderReferences(h.db.WithContext(r.Context()), response.Orders, sortedEntityIDs(sets["order"])); err != nil {
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

func resolveUserReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.User
	if err := db.Unscoped().Select("id, account_name, email, status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		displayName := strings.TrimSpace(item.AccountName)
		secondary := strings.TrimSpace(item.Email)
		if displayName == "" {
			displayName = secondary
			secondary = ""
		}
		if displayName == "" {
			displayName = "用户"
		}
		result[entityKey(item.ID)] = entityReference{ID: item.ID, Kind: "user", DisplayName: displayName, Secondary: secondary, Status: item.Status}
	}
	return nil
}

type subscriptionReferenceRow struct {
	ID       uint   `gorm:"column:id"`
	Status   string `gorm:"column:status"`
	PlanName string `gorm:"column:plan_name"`
	SKUName  string `gorm:"column:sku_name"`
}

func resolveSubscriptionReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []subscriptionReferenceRow
	if err := db.Table("subscriptions").
		Select("subscriptions.id AS id, subscriptions.status AS status, plans.name AS plan_name, plan_skus.name AS sku_name").
		Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").
		Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
		Where("subscriptions.id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		displayName := strings.TrimSpace(row.PlanName)
		if displayName == "" {
			displayName = "订阅"
		}
		result[entityKey(row.ID)] = entityReference{ID: row.ID, Kind: "subscription", DisplayName: displayName, Secondary: strings.TrimSpace(row.SKUName), Status: row.Status}
	}
	return nil
}

func resolveNodeReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Node
	if err := db.Select("id, name, region, lifecycle_status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		displayName := strings.TrimSpace(item.Name)
		if displayName == "" {
			displayName = "节点"
		}
		result[entityKey(item.ID)] = entityReference{ID: item.ID, Kind: "node", DisplayName: displayName, Secondary: strings.TrimSpace(item.Region), Status: item.LifecycleStatus}
	}
	return nil
}

type protocolEndpointReferenceRow struct {
	ID       uint   `gorm:"column:id"`
	Name     string `gorm:"column:name"`
	Protocol string `gorm:"column:protocol"`
	NodeName string `gorm:"column:node_name"`
	IsActive bool   `gorm:"column:is_active"`
}

func resolveProtocolEndpointReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []protocolEndpointReferenceRow
	if err := db.Table("protocol_endpoints").
		Select("protocol_endpoints.id AS id, protocol_endpoints.name AS name, protocol_endpoints.protocol AS protocol, protocol_endpoints.is_active AS is_active, nodes.name AS node_name").
		Joins("LEFT JOIN nodes ON nodes.id = protocol_endpoints.node_id").
		Where("protocol_endpoints.id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		displayName := strings.TrimSpace(row.Name)
		if displayName == "" {
			displayName = strings.TrimSpace(row.NodeName)
		}
		if displayName == "" {
			displayName = "协议端点"
		}
		secondaryParts := make([]string, 0, 2)
		if strings.TrimSpace(row.Protocol) != "" {
			secondaryParts = append(secondaryParts, strings.TrimSpace(row.Protocol))
		}
		if strings.TrimSpace(row.NodeName) != "" && strings.TrimSpace(row.NodeName) != displayName {
			secondaryParts = append(secondaryParts, strings.TrimSpace(row.NodeName))
		}
		status := "inactive"
		if row.IsActive {
			status = "active"
		}
		result[entityKey(row.ID)] = entityReference{ID: row.ID, Kind: "protocol_endpoint", DisplayName: displayName, Secondary: strings.Join(secondaryParts, " · "), Status: status}
	}
	return nil
}

func resolvePlanReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Plan
	if err := db.Select("id, name, slug, is_active").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		displayName := strings.TrimSpace(item.Name)
		if displayName == "" {
			displayName = "套餐"
		}
		status := "inactive"
		if item.IsActive {
			status = "active"
		}
		result[entityKey(item.ID)] = entityReference{ID: item.ID, Kind: "plan", DisplayName: displayName, Secondary: strings.TrimSpace(item.Slug), Status: status}
	}
	return nil
}

type planSKUReferenceRow struct {
	ID       uint   `gorm:"column:id"`
	Name     string `gorm:"column:name"`
	PlanName string `gorm:"column:plan_name"`
	IsActive bool   `gorm:"column:is_active"`
}

func resolvePlanSKUReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []planSKUReferenceRow
	if err := db.Table("plan_skus").
		Select("plan_skus.id AS id, plan_skus.name AS name, plan_skus.is_active AS is_active, plans.name AS plan_name").
		Joins("LEFT JOIN plans ON plans.id = plan_skus.plan_id").
		Where("plan_skus.id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		displayName := strings.TrimSpace(row.Name)
		if displayName == "" {
			displayName = "套餐规格"
		}
		status := "inactive"
		if row.IsActive {
			status = "active"
		}
		result[entityKey(row.ID)] = entityReference{ID: row.ID, Kind: "plan_sku", DisplayName: displayName, Secondary: strings.TrimSpace(row.PlanName), Status: status}
	}
	return nil
}

func resolveOrderReferences(db *gorm.DB, result map[string]entityReference, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var items []model.Order
	if err := db.Select("id, plan_name, sku_name, trade_no, status").Where("id IN ?", ids).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		displayName := strings.TrimSpace(item.PlanName)
		if displayName == "" {
			displayName = "订单"
		}
		secondaryParts := make([]string, 0, 2)
		if strings.TrimSpace(item.SKUName) != "" {
			secondaryParts = append(secondaryParts, strings.TrimSpace(item.SKUName))
		}
		if strings.TrimSpace(item.TradeNo) != "" {
			secondaryParts = append(secondaryParts, strings.TrimSpace(item.TradeNo))
		}
		result[entityKey(item.ID)] = entityReference{ID: item.ID, Kind: "order", DisplayName: displayName, Secondary: strings.Join(secondaryParts, " · "), Status: item.Status}
	}
	return nil
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
	byDay := make(map[string]trafficTrendAggregateRow, len(rows))
	var recordCount int64
	for _, row := range rows {
		key := strings.TrimSpace(row.Day)
		if len(key) >= 10 {
			key = key[:10]
		}
		byDay[key] = row
		recordCount += row.RecordCount
	}
	points := make([]trafficTrendPoint, 0, days)
	for index := 0; index < days; index++ {
		date := from.AddDate(0, 0, index)
		key := date.Format("2006-01-02")
		row := byDay[key]
		points = append(points, trafficTrendPoint{
			Date:            key,
			Label:           fmt.Sprintf("%d/%d", int(date.Month()), date.Day()),
			UploadBytes:     row.UploadBytes,
			DownloadBytes:   row.DownloadBytes,
			UsedBytes:       row.UsedBytes,
			PeakConnections: nil,
			RecordCount:     row.RecordCount,
		})
	}
	return points, recordCount
}

func applyTrafficTrendIDFilter(query *gorm.DB, values url.Values, key, column string) (*gorm.DB, error) {
	id, err := positiveQueryID(values, key)
	if err != nil {
		return nil, err
	}
	if id > 0 {
		query = query.Where(column+" = ?", id)
	}
	return query, nil
}

func (h *handlers) TrafficTrendsHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	adminRequest := strings.HasPrefix(r.URL.Path, "/api/v1/admin/")
	if adminRequest && !claims.IsAdmin {
		Forbidden(w, "admin access required")
		return
	}

	from, to, days, err := parseTrafficTrendRange(r.URL.Query(), time.Now().UTC())
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	db := h.db.WithContext(r.Context())
	query := db.Model(&model.TrafficRecord{}).
		Where("record_at >= ? AND record_at < ?", from, to.AddDate(0, 0, 1))

	var facetUserID uint
	if adminRequest {
		facetUserID, err = positiveQueryID(r.URL.Query(), "user_id")
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		if facetUserID > 0 {
			query = query.Where("user_id = ?", facetUserID)
		}
	} else {
		facetUserID = claims.UserID
		query = query.Where("user_id = ?", claims.UserID)
	}

	for _, filter := range []struct {
		Key    string
		Column string
	}{
		{Key: "subscription_id", Column: "subscription_id"},
		{Key: "node_id", Column: "node_id"},
		{Key: "protocol_endpoint_id", Column: "protocol_endpoint_id"},
	} {
		query, err = applyTrafficTrendIDFilter(query, r.URL.Query(), filter.Key, filter.Column)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}

	var rows []trafficTrendAggregateRow
	if err := query.
		Select("DATE(record_at) AS day, COALESCE(SUM(upload_bytes), 0) AS upload_bytes, COALESCE(SUM(download_bytes), 0) AS download_bytes, COALESCE(SUM(used_bytes), 0) AS used_bytes, COUNT(*) AS record_count").
		Group("DATE(record_at)").
		Order("day ASC").
		Scan(&rows).Error; err != nil {
		ServerError(w, err)
		return
	}
	points, recordCount := buildTrafficTrendPoints(from, days, rows)

	subscriptions := make([]entityReference, 0)
	if facetUserID > 0 && r.URL.Query().Get("include_subscriptions") == "true" {
		var subscriptionRows []subscriptionReferenceRow
		if err := db.Table("subscriptions").
			Select("subscriptions.id AS id, subscriptions.status AS status, plans.name AS plan_name, plan_skus.name AS sku_name").
			Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").
			Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").
			Where("subscriptions.user_id = ?", facetUserID).
			Order("subscriptions.created_at DESC, subscriptions.id DESC").
			Scan(&subscriptionRows).Error; err != nil {
			ServerError(w, err)
			return
		}
		for _, row := range subscriptionRows {
			displayName := strings.TrimSpace(row.PlanName)
			if displayName == "" {
				displayName = "订阅"
			}
			subscriptions = append(subscriptions, entityReference{ID: row.ID, Kind: "subscription", DisplayName: displayName, Secondary: strings.TrimSpace(row.SKUName), Status: row.Status})
		}
	}

	OK(w, trafficTrendResponse{
		From:                  from.Format("2006-01-02"),
		To:                    to.Format("2006-01-02"),
		Points:                points,
		RecordCount:           recordCount,
		ConnectionSampleCount: 0,
		PeakConnections:       nil,
		Truncated:             false,
		Subscriptions:         subscriptions,
		AsOf:                  time.Now().UTC(),
	})
}
