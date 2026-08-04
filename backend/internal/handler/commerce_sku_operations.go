package handler

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	skuBillingPeriodic = "periodic"
	skuBillingOneTime  = "one_time"

	skuOperationPurchase = "purchase"
	skuOperationRenew    = "renew"
	skuOperationChange   = "change"
	skuOperationAddon    = "addon"
)

var skuOperationOrder = map[string]int{
	skuOperationPurchase: 0,
	skuOperationRenew:    1,
	skuOperationChange:   2,
	skuOperationAddon:    3,
}

type commercePlanSKURequest struct {
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	SKUType           string   `json:"sku_type"` // Deprecated compatibility input.
	BillingMode       string   `json:"billing_mode"`
	AllowedOperations []string `json:"allowed_operations"`
	BillingUnit       string   `json:"billing_unit"`
	BillingValue      int      `json:"billing_value"`
	PriceCents        int64    `json:"price_cents"`
	Currency          string   `json:"currency"`
	GrantTrafficBytes int64    `json:"grant_traffic_bytes"`
	TrafficBytes      int64    `json:"traffic_bytes"`    // Deprecated alias for grant_traffic_bytes.
	DeviceLimit       int      `json:"device_limit"`     // Deprecated compatibility input; must be zero.
	SpeedLimitMbps    int      `json:"speed_limit_mbps"` // Deprecated compatibility input; must be zero.
	IsActive          *bool    `json:"is_active"`
	SortOrder         int      `json:"sort_order"`
}

type commercePlanCreateRequest struct {
	Name                   string                   `json:"name"`
	Slug                   string                   `json:"slug"`
	Summary                string                   `json:"summary"`
	Description            string                   `json:"description"`
	SortOrder              int                      `json:"sort_order"`
	IsActive               bool                     `json:"is_active"`
	SKUs                   []commercePlanSKURequest `json:"skus"`
	NodeGroupID            uint                     `json:"node_group_id"`
	TrafficBytes           int64                    `json:"traffic_bytes"`
	SpeedLimitMbps         int                      `json:"speed_limit_mbps"`
	MaxActiveSubscriptions int                      `json:"max_active_subscriptions"`
	IsRenewable            *bool                    `json:"is_renewable"`
	DeviceLimit            int                      `json:"device_limit"`
	FamilyLimit            int                      `json:"family_limit"`
	ResetPolicy            int16                    `json:"reset_policy"`
	TrafficCalcMode        int16                    `json:"traffic_calc_mode"`
}

type commerceOrderCreateRequest struct {
	PlanSKUID            uint   `json:"plan_sku_id"`
	OrderType            string `json:"order_type"` // Deprecated compatibility assertion.
	TargetSubscriptionID uint   `json:"target_subscription_id"`
	Channel              string `json:"channel"`
}

type normalizedCommerceSKU struct {
	SKU               model.PlanSKU
	BillingMode       string
	AllowedOperations []string
}

type commercePlanSKUItem struct {
	model.PlanSKU
	BillingMode       string   `json:"billing_mode"`
	AllowedOperations []string `json:"allowed_operations"`
	GrantTrafficBytes int64    `json:"grant_traffic_bytes"`
}

type planSKUBillingRow struct {
	ID          uint   `gorm:"column:id"`
	BillingMode string `gorm:"column:billing_mode"`
}

func legacySKUTypeOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "renewal":
		return skuOperationRenew
	case "upgrade":
		return skuOperationChange
	case "traffic_pack":
		return skuOperationAddon
	default:
		return skuOperationPurchase
	}
}

func compatibilitySKUType(billingMode string, operations []string) string {
	if billingMode == skuBillingOneTime || containsSKUOperation(operations, skuOperationAddon) {
		return "traffic_pack"
	}
	if containsSKUOperation(operations, skuOperationPurchase) {
		return "new"
	}
	if containsSKUOperation(operations, skuOperationRenew) {
		return "renewal"
	}
	return "upgrade"
}

func normalizeSKUOperations(values []string, legacyType string) ([]string, error) {
	if len(values) == 0 {
		values = []string{legacySKUTypeOperation(legacyType)}
	}
	seen := make(map[string]struct{}, len(values))
	operations := make([]string, 0, len(values))
	for _, value := range values {
		operation := strings.ToLower(strings.TrimSpace(value))
		if _, valid := skuOperationOrder[operation]; !valid {
			return nil, validationError("销售规格校验失败。", map[string]string{
				"allowed_operations": "可用场景只能包含新购、续费、套餐切换或附加购买。",
			})
		}
		if _, exists := seen[operation]; exists {
			continue
		}
		seen[operation] = struct{}{}
		operations = append(operations, operation)
	}
	if len(operations) == 0 {
		return nil, validationError("销售规格校验失败。", map[string]string{
			"allowed_operations": "请至少选择一个可用场景。",
		})
	}
	sort.SliceStable(operations, func(left, right int) bool {
		return skuOperationOrder[operations[left]] < skuOperationOrder[operations[right]]
	})
	return operations, nil
}

func normalizeCommercePlanSKU(planID uint, request commercePlanSKURequest) (normalizedCommerceSKU, error) {
	billingMode := strings.ToLower(strings.TrimSpace(request.BillingMode))
	if billingMode == "" {
		if strings.EqualFold(strings.TrimSpace(request.BillingUnit), "once") || strings.EqualFold(strings.TrimSpace(request.SKUType), "traffic_pack") {
			billingMode = skuBillingOneTime
		} else {
			billingMode = skuBillingPeriodic
		}
	}
	if billingMode != skuBillingPeriodic && billingMode != skuBillingOneTime {
		return normalizedCommerceSKU{}, validationError("销售规格校验失败。", map[string]string{
			"billing_mode": "计费方式只能是周期计费或一次性计费。",
		})
	}
	operations, err := normalizeSKUOperations(request.AllowedOperations, request.SKUType)
	if err != nil {
		return normalizedCommerceSKU{}, err
	}
	fields := map[string]string{}
	grantTrafficBytes := request.GrantTrafficBytes
	if grantTrafficBytes == 0 {
		grantTrafficBytes = request.TrafficBytes
	}
	billingUnit := strings.ToLower(strings.TrimSpace(request.BillingUnit))
	if billingMode == skuBillingOneTime {
		if billingUnit != "once" {
			fields["billing_unit"] = "一次性规格必须使用一次性计费单位。"
		}
		if len(operations) != 1 || operations[0] != skuOperationAddon {
			fields["allowed_operations"] = "一次性规格当前仅用于附加权益购买。"
		}
		if grantTrafficBytes <= 0 {
			fields["grant_traffic_bytes"] = "流量包的附加流量必须大于 0。"
		}
		if request.DeviceLimit != 0 || request.SpeedLimitMbps != 0 {
			fields["entitlements"] = "流量包只能增加流量，不能修改设备数或限速。"
		}
	} else {
		if billingUnit == "once" {
			fields["billing_unit"] = "周期规格不能使用一次性计费单位。"
		}
		if containsSKUOperation(operations, skuOperationAddon) {
			fields["allowed_operations"] = "附加权益请使用一次性规格。"
		}
		if request.GrantTrafficBytes != 0 || request.TrafficBytes != 0 || request.DeviceLimit != 0 || request.SpeedLimitMbps != 0 {
			fields["entitlements"] = "周期规格继承商品权益，不能单独配置流量、设备数或限速。"
		}
		grantTrafficBytes = 0
	}
	if len(fields) > 0 {
		return normalizedCommerceSKU{}, validationError("销售规格校验失败。", fields)
	}

	legacy := planSKUReq{
		Code: request.Code, Name: request.Name,
		SKUType:     compatibilitySKUType(billingMode, operations),
		BillingUnit: request.BillingUnit, BillingValue: request.BillingValue,
		PriceCents: request.PriceCents, Currency: request.Currency,
		TrafficBytes: grantTrafficBytes, DeviceLimit: 0,
		SpeedLimitMbps: 0, IsActive: request.IsActive,
		SortOrder: request.SortOrder,
	}
	sku, err := buildPlanSKU(planID, legacy)
	if err != nil {
		return normalizedCommerceSKU{}, err
	}
	return normalizedCommerceSKU{SKU: sku, BillingMode: billingMode, AllowedOperations: operations}, nil
}

func containsSKUOperation(operations []string, target string) bool {
	for _, operation := range operations {
		if operation == target {
			return true
		}
	}
	return false
}

func replacePlanSKUOperations(tx *gorm.DB, planSKUID uint, operations []string) error {
	if err := tx.Where("plan_sku_id = ?", planSKUID).Delete(&model.PlanSKUOperation{}).Error; err != nil {
		return err
	}
	rows := make([]model.PlanSKUOperation, 0, len(operations))
	for _, operation := range operations {
		rows = append(rows, model.PlanSKUOperation{PlanSKUID: planSKUID, Operation: operation})
	}
	if len(rows) == 0 {
		return errors.New("plan sku must have at least one allowed operation")
	}
	return tx.Create(&rows).Error
}

func loadPlanSKUCommerceMetadata(db *gorm.DB, skus []model.PlanSKU) (map[uint]string, map[uint][]string, error) {
	billingModes := make(map[uint]string, len(skus))
	operations := make(map[uint][]string, len(skus))
	if len(skus) == 0 {
		return billingModes, operations, nil
	}
	ids := make([]uint, 0, len(skus))
	for _, sku := range skus {
		ids = append(ids, sku.ID)
	}
	var billingRows []planSKUBillingRow
	if err := db.Table("plan_skus").Select("id, billing_mode").Where("id IN ?", ids).Scan(&billingRows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range billingRows {
		billingModes[row.ID] = row.BillingMode
	}
	var operationRows []model.PlanSKUOperation
	if err := db.Where("plan_sku_id IN ?", ids).Order("plan_sku_id asc, operation asc").Find(&operationRows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range operationRows {
		operations[row.PlanSKUID] = append(operations[row.PlanSKUID], row.Operation)
	}
	for _, sku := range skus {
		if billingModes[sku.ID] == "" {
			if sku.BillingUnit == "once" || sku.SKUType == "traffic_pack" {
				billingModes[sku.ID] = skuBillingOneTime
			} else {
				billingModes[sku.ID] = skuBillingPeriodic
			}
		}
		if len(operations[sku.ID]) == 0 {
			operations[sku.ID] = []string{legacySKUTypeOperation(sku.SKUType)}
		}
		sort.SliceStable(operations[sku.ID], func(left, right int) bool {
			return skuOperationOrder[operations[sku.ID][left]] < skuOperationOrder[operations[sku.ID][right]]
		})
	}
	return billingModes, operations, nil
}

func decoratePlanSKUs(db *gorm.DB, skus []model.PlanSKU) ([]commercePlanSKUItem, error) {
	billingModes, operations, err := loadPlanSKUCommerceMetadata(db, skus)
	if err != nil {
		return nil, err
	}
	items := make([]commercePlanSKUItem, 0, len(skus))
	for _, sku := range skus {
		grantTrafficBytes := int64(0)
		if billingModes[sku.ID] == skuBillingOneTime || containsSKUOperation(operations[sku.ID], skuOperationAddon) {
			grantTrafficBytes = sku.TrafficBytes
		}
		items = append(items, commercePlanSKUItem{
			PlanSKU: sku, BillingMode: billingModes[sku.ID], AllowedOperations: operations[sku.ID],
			GrantTrafficBytes: grantTrafficBytes,
		})
	}
	return items, nil
}

func applyPlanSKUOperationFilter(query *gorm.DB, operation string) (*gorm.DB, error) {
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "" {
		return query, nil
	}
	if _, valid := skuOperationOrder[operation]; !valid {
		return nil, errors.New("invalid operation")
	}
	return query.Where(
		"EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)",
		operation,
	), nil
}

func (h *handlers) PlanSKUListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var planCount int64
	if err := h.db.Model(&model.Plan{}).Where("id = ?", planID).Count(&planCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if planCount == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.PlanSKU{}).Where("plan_id = ?", planID)
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		if len(search) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		pattern := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(currency) LIKE ?", pattern, pattern, pattern)
	}
	if rawActive := strings.TrimSpace(r.URL.Query().Get("active")); rawActive != "" {
		active, parseErr := parseStrictBool(rawActive)
		if parseErr != nil {
			BadRequest(w, "invalid active")
			return
		}
		query = query.Where("is_active = ?", active)
	}
	query, err = applyPlanSKUOperationFilter(query, r.URL.Query().Get("operation"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.PlanSKU, 0)
	if err := query.Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	decorated, err := decoratePlanSKUs(h.db, items)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(decorated, total, offset, limit))
}

func parseStrictBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, errors.New("invalid boolean")
	}
}

func (h *handlers) PublicPlanSKUListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	planID, err := parsePathID(r.URL.Path, "/api/v1/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var planCount int64
	if err := h.db.Model(&model.Plan{}).Where("id = ? AND is_active = ?", planID, true).Count(&planCount).Error; err != nil {
		ServerError(w, err)
		return
	}
	if planCount == 0 {
		NotFound(w)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND is_active = ?", planID, true)
	if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
		if len(search) > 128 {
			BadRequest(w, "q must not exceed 128 bytes")
			return
		}
		pattern := "%" + search + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(currency) LIKE ?", pattern, pattern, pattern)
	}
	operation := strings.TrimSpace(r.URL.Query().Get("operation"))
	if operation == "" {
		if legacyType := strings.TrimSpace(r.URL.Query().Get("sku_type")); legacyType != "" {
			if !isValidOrderType(legacyType) {
				BadRequest(w, "invalid sku_type")
				return
			}
			operation = legacySKUTypeOperation(legacyType)
		}
	}
	query, err = applyPlanSKUOperationFilter(query, operation)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.PlanSKU, 0)
	if err := query.Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	decorated, err := decoratePlanSKUs(h.db, items)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, pagedData(decorated, total, offset, limit))
}

func (h *handlers) PlanSKUGetCommerceHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var sku model.PlanSKU
	if err := h.db.First(&sku, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	items, err := decoratePlanSKUs(h.db, []model.PlanSKU{sku})
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, items[0])
}

func (h *handlers) PlanSKUCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commercePlanSKURequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	normalized, err := normalizeCommercePlanSKU(planID, request)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var plan model.Plan
		if err := tx.First(&plan, planID).Error; err != nil {
			return err
		}
		if err := tx.Create(&normalized.SKU).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PlanSKU{}).Where("id = ?", normalized.SKU.ID).Update("billing_mode", normalized.BillingMode).Error; err != nil {
			return err
		}
		if err := replacePlanSKUOperations(tx, normalized.SKU.ID, normalized.AllowedOperations); err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plan.sku.create", fmt.Sprintf("plan_sku:%d", normalized.SKU.ID),
			fmt.Sprintf("plan=%d code=%s operations=%s", planID, normalized.SKU.Code, strings.Join(normalized.AllowedOperations, ",")))
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		BadRequest(w, err.Error())
		return
	}
	OK(w, commercePlanSKUItem{PlanSKU: normalized.SKU, BillingMode: normalized.BillingMode, AllowedOperations: normalized.AllowedOperations, GrantTrafficBytes: normalized.SKU.TrafficBytes})
}

func (h *handlers) PlanSKUUpdateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commercePlanSKURequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	var existing model.PlanSKU
	if err := h.db.First(&existing, id).Error; err != nil {
		NotFound(w)
		return
	}
	normalized, err := normalizeCommercePlanSKU(existing.PlanID, request)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	normalized.SKU.ID = existing.ID
	if !normalized.SKU.IsActive {
		var plan model.Plan
		if err := h.db.First(&plan, existing.PlanID).Error; err != nil {
			ServerError(w, err)
			return
		}
		if plan.IsActive {
			var otherActive int64
			if err := h.db.Model(&model.PlanSKU{}).Where("plan_id = ? AND id <> ? AND is_active = ?", existing.PlanID, existing.ID, true).Count(&otherActive).Error; err != nil || otherActive == 0 {
				BadRequestFields(w, "销售规格校验失败。", map[string]string{"is_active": "已发布商品必须保留至少一个可售 SKU。"})
				return
			}
		}
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"code": normalized.SKU.Code, "name": normalized.SKU.Name,
			"sku_type": normalized.SKU.SKUType, "billing_mode": normalized.BillingMode,
			"billing_unit": normalized.SKU.BillingUnit, "billing_value": normalized.SKU.BillingValue,
			"price_cents": normalized.SKU.PriceCents, "currency": normalized.SKU.Currency,
			"traffic_bytes": normalized.SKU.TrafficBytes, "device_limit": normalized.SKU.DeviceLimit,
			"speed_limit_mbps": normalized.SKU.SpeedLimitMbps,
			"is_active":        normalized.SKU.IsActive, "sort_order": normalized.SKU.SortOrder,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		if err := replacePlanSKUOperations(tx, existing.ID, normalized.AllowedOperations); err != nil {
			return err
		}
		return createAuditLog(tx, claims, "plan.sku.update", fmt.Sprintf("plan_sku:%d", existing.ID),
			fmt.Sprintf("code=%s operations=%s", normalized.SKU.Code, strings.Join(normalized.AllowedOperations, ",")))
	})
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, commercePlanSKUItem{PlanSKU: normalized.SKU, BillingMode: normalized.BillingMode, AllowedOperations: normalized.AllowedOperations, GrantTrafficBytes: normalized.SKU.TrafficBytes})
}

func (h *handlers) PlanCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var request commercePlanCreateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Slug = strings.ToLower(strings.TrimSpace(request.Slug))
	fields := map[string]string{}
	if request.Name == "" {
		fields["name"] = "请输入商品名称。"
	}
	if request.Slug == "" {
		fields["slug"] = "请输入商品 Slug。"
	}
	if len(request.SKUs) == 0 {
		fields["skus"] = "请至少配置一个销售规格。"
	}
	if request.NodeGroupID == 0 {
		fields["node_group_id"] = "请选择节点组。"
	}
	if len(fields) > 0 {
		BadRequestFields(w, "商品信息校验失败。", fields)
		return
	}
	normalizedSKUs := make([]normalizedCommerceSKU, 0, len(request.SKUs))
	legacySKUs := make([]planSKUReq, 0, len(request.SKUs))
	for index, skuRequest := range request.SKUs {
		normalized, normalizeErr := normalizeCommercePlanSKU(0, skuRequest)
		if normalizeErr != nil {
			BadRequestError(w, prefixValidationError(normalizeErr, fmt.Sprintf("skus.%d.", index)))
			return
		}
		normalizedSKUs = append(normalizedSKUs, normalized)
		legacySKUs = append(legacySKUs, planSKUReq{
			Code: normalized.SKU.Code, Name: normalized.SKU.Name, SKUType: normalized.SKU.SKUType,
			BillingUnit: normalized.SKU.BillingUnit, BillingValue: normalized.SKU.BillingValue,
			PriceCents: normalized.SKU.PriceCents, Currency: normalized.SKU.Currency,
			TrafficBytes: normalized.SKU.TrafficBytes, DeviceLimit: normalized.SKU.DeviceLimit,
			SpeedLimitMbps: normalized.SKU.SpeedLimitMbps, IsActive: &normalized.SKU.IsActive,
			SortOrder: normalized.SKU.SortOrder,
		})
	}
	legacyPlanRequest := planCreateReq{
		Name: request.Name, Slug: request.Slug, Summary: request.Summary, Description: request.Description,
		SortOrder: request.SortOrder, IsActive: request.IsActive, SKUs: legacySKUs,
		NodeGroupID: request.NodeGroupID, TrafficBytes: request.TrafficBytes,
		SpeedLimitMbps: request.SpeedLimitMbps, MaxActiveSubscriptions: request.MaxActiveSubscriptions,
		IsRenewable: request.IsRenewable, DeviceLimit: request.DeviceLimit, FamilyLimit: request.FamilyLimit,
		ResetPolicy: request.ResetPolicy, TrafficCalcMode: request.TrafficCalcMode,
	}
	policy, err := normalizePlanPolicy(legacyPlanRequest, legacySKUs[0])
	if err != nil {
		BadRequestError(w, err)
		return
	}
	plan := model.Plan{
		Name: request.Name, Slug: request.Slug, Summary: strings.TrimSpace(request.Summary),
		Description: strings.TrimSpace(request.Description), IsActive: request.IsActive,
		SortOrder: request.SortOrder, Revision: 1,
		TrafficBytes: policy.TrafficBytes, SpeedLimitMbps: policy.SpeedLimitMbps,
		MaxActiveSubscriptions: policy.MaxActiveSubscriptions, IsRenewable: policy.IsRenewable,
		DeviceLimit: policy.DeviceLimit, FamilyLimit: policy.FamilyLimit,
		ResetPolicy: policy.ResetPolicy, TrafficCalcMode: policy.TrafficCalcMode,
	}
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var group model.NodeGroup
		if err := tx.First(&group, request.NodeGroupID).Error; err != nil {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "所选节点组不存在。"})
		}
		if plan.IsActive && !group.IsEnabled {
			return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品必须选择已启用的节点组。"})
		}
		plan.NodeGroupID = group.ID
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		for index := range normalizedSKUs {
			normalizedSKUs[index].SKU.PlanID = plan.ID
			if err := tx.Create(&normalizedSKUs[index].SKU).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.PlanSKU{}).Where("id = ?", normalizedSKUs[index].SKU.ID).Update("billing_mode", normalizedSKUs[index].BillingMode).Error; err != nil {
				return err
			}
			if err := replacePlanSKUOperations(tx, normalizedSKUs[index].SKU.ID, normalizedSKUs[index].AllowedOperations); err != nil {
				return err
			}
		}
		if plan.IsActive {
			var activePurchaseSKUCount int64
			if err := tx.Table("plan_skus").
				Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", skuOperationPurchase).
				Where("plan_skus.plan_id = ? AND plan_skus.is_active = ?", plan.ID, true).
				Count(&activePurchaseSKUCount).Error; err != nil || activePurchaseSKUCount == 0 {
				return validationError("商品信息校验失败。", map[string]string{"skus": "已发布商品至少需要一个允许新购的可售 SKU。"})
			}
			var endpointCount int64
			if err := tx.Model(&model.NodeGroupEndpoint{}).
				Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
				Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", plan.NodeGroupID, true).
				Count(&endpointCount).Error; err != nil || endpointCount == 0 {
				return validationError("商品信息校验失败。", map[string]string{"node_group_id": "已发布商品的节点组至少需要一个已启用协议端点。"})
			}
		}
		return createAuditLog(tx, claims, "plan.create", fmt.Sprintf("plan:%d", plan.ID),
			fmt.Sprintf("skus=%d node_group=%d operation_model=v2", len(normalizedSKUs), plan.NodeGroupID))
	})
	if err != nil {
		BadRequestError(w, err)
		return
	}
	_ = h.db.Preload("SKUs", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order asc, id asc") }).First(&plan, plan.ID).Error
	OK(w, plan)
}

func loadPlanSKUCountsCommerce(db *gorm.DB, planIDs []uint, publicCatalog bool) (map[uint]planSKUCountRow, error) {
	if !publicCatalog {
		return loadPlanSKUCounts(db, planIDs)
	}
	counts := make(map[uint]planSKUCountRow, len(planIDs))
	if len(planIDs) == 0 {
		return counts, nil
	}
	rows := make([]planSKUCountRow, 0, len(planIDs))
	if err := db.Table("plan_skus").
		Select("plan_skus.plan_id, COUNT(*) AS sku_count, COUNT(*) AS active_sku_count").
		Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", skuOperationPurchase).
		Where("plan_skus.plan_id IN ? AND plan_skus.is_active = ?", planIDs, true).
		Group("plan_skus.plan_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.PlanID] = row
	}
	return counts, nil
}

func loadPrimaryPlanSKUsCommerce(db *gorm.DB, planIDs []uint) (map[uint]model.PlanSKU, error) {
	items := make(map[uint]model.PlanSKU, len(planIDs))
	if len(planIDs) == 0 {
		return items, nil
	}
	rows := make([]model.PlanSKU, 0, len(planIDs))
	if err := db.Table("plan_skus AS candidate").
		Joins("JOIN plan_sku_operations AS candidate_operation ON candidate_operation.plan_sku_id = candidate.id AND candidate_operation.operation = ?", skuOperationPurchase).
		Where("candidate.plan_id IN ? AND candidate.is_active = ?", planIDs, true).
		Where(`NOT EXISTS (
			SELECT 1
			FROM plan_skus AS earlier
			JOIN plan_sku_operations AS earlier_operation
			  ON earlier_operation.plan_sku_id = earlier.id
			 AND earlier_operation.operation = 'purchase'
			WHERE earlier.plan_id = candidate.plan_id
			  AND earlier.is_active = 1
			  AND (
			    earlier.price_cents < candidate.price_cents
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order < candidate.sort_order)
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order = candidate.sort_order AND earlier.id < candidate.id)
			  )
		)`).
		Order("candidate.plan_id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		items[row.PlanID] = row
	}
	return items, nil
}

func (h *handlers) PlanListCommerceHandler(w http.ResponseWriter, r *http.Request) {
	plans := make([]model.Plan, 0)
	claims, claimErr := h.authFromRequest(r)
	isAdmin := claimErr == nil && claims.IsAdmin
	paged := r.URL.Query().Get("paged") == "true"
	offset, limit := 0, 50
	var err error
	if paged {
		offset, limit, err = parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
	}
	query := h.db.Model(&model.Plan{}).Order("sort_order asc, id desc")
	if !isAdmin {
		query = query.Where("is_active = 1")
	} else if !parseBoolQuery(r.URL.Query().Get("include_inactive")) {
		query = query.Where("is_active = 1")
	}
	if paged {
		if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); search != "" {
			if len(search) > 128 {
				BadRequest(w, "q must not exceed 128 bytes")
				return
			}
			pattern := "%" + search + "%"
			query = query.Where("LOWER(plans.name) LIKE ? OR LOWER(plans.slug) LIKE ? OR LOWER(plans.summary) LIKE ?", pattern, pattern, pattern)
		}
	}
	if isAdmin {
		if rawActive := strings.TrimSpace(r.URL.Query().Get("active")); rawActive != "" {
			active, parseErr := parseStrictBool(rawActive)
			if parseErr != nil {
				BadRequest(w, "invalid active")
				return
			}
			query = query.Where("plans.is_active = ?", active)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	if paged {
		query = query.Offset(offset).Limit(limit).Preload("NodeGroup")
	} else {
		query = query.Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			if !isAdmin {
				db = db.Where("is_active = ?", true).
					Where("EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)", skuOperationPurchase)
			}
			return db.Order("sort_order asc, id asc")
		}).Preload("NodeGroup")
	}
	if err := query.Find(&plans).Error; err != nil {
		ServerError(w, err)
		return
	}
	if !paged {
		OK(w, plans)
		return
	}
	planIDs := make([]uint, 0, len(plans))
	for _, plan := range plans {
		planIDs = append(planIDs, plan.ID)
	}
	counts, err := loadPlanSKUCountsCommerce(h.db, planIDs, !isAdmin)
	if err != nil {
		ServerError(w, err)
		return
	}
	if isAdmin {
		items := make([]planSummaryItem, 0, len(plans))
		for _, plan := range plans {
			items = append(items, newPlanSummaryItem(plan, counts[plan.ID]))
		}
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	primarySKUs, err := loadPrimaryPlanSKUsCommerce(h.db, planIDs)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := make([]planCatalogItem, 0, len(plans))
	for _, plan := range plans {
		count := counts[plan.ID]
		var primarySKU *model.PlanSKU
		if item, ok := primarySKUs[plan.ID]; ok {
			copy := item
			primarySKU = &copy
		}
		items = append(items, newPlanCatalogItem(plan, count, primarySKU))
	}
	OK(w, pagedData(items, total, offset, limit))
}

func (h *handlers) PublicPlanDetailCommerceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var plan model.Plan
	if err := h.db.Preload("NodeGroup").Where("is_active = ?", true).First(&plan, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	counts, err := loadPlanSKUCountsCommerce(h.db, []uint{plan.ID}, true)
	if err != nil {
		ServerError(w, err)
		return
	}
	primarySKUs, err := loadPrimaryPlanSKUsCommerce(h.db, []uint{plan.ID})
	if err != nil {
		ServerError(w, err)
		return
	}
	var primarySKU *model.PlanSKU
	if item, ok := primarySKUs[plan.ID]; ok {
		copy := item
		primarySKU = &copy
	}
	OK(w, newPlanCatalogItem(plan, counts[plan.ID], primarySKU))
}

func deriveOrderTypeForSKU(planID uint, billingMode string, operations []string, target *model.Subscription) (string, error) {
	if target == nil {
		if billingMode != skuBillingPeriodic || !containsSKUOperation(operations, skuOperationPurchase) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许新购。"})
		}
		return "new", nil
	}
	if billingMode == skuBillingOneTime {
		if !containsSKUOperation(operations, skuOperationAddon) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许附加购买。"})
		}
		return "traffic_pack", nil
	}
	if target.PlanID == planID {
		if !containsSKUOperation(operations, skuOperationRenew) {
			return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许用于续费。"})
		}
		return "renewal", nil
	}
	if !containsSKUOperation(operations, skuOperationChange) {
		return "", validationError("订单创建失败。", map[string]string{"plan_sku_id": "该规格不允许用于套餐切换。"})
	}
	return "upgrade", nil
}

func (h *handlers) OrderCreateCommerceHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, err.Error())
		return
	}
	var request commerceOrderCreateRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if request.PlanSKUID == 0 {
		BadRequestFields(w, "订单创建失败。", map[string]string{"plan_sku_id": "请选择销售规格。"})
		return
	}
	var sku model.PlanSKU
	if err := h.db.Where("is_active = ?", true).First(&sku, request.PlanSKUID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			BadRequestFields(w, "订单创建失败。", map[string]string{"plan_sku_id": "销售规格不存在或已停止销售。"})
			return
		}
		ServerError(w, err)
		return
	}
	var plan model.Plan
	if err := h.db.Where("is_active = ?", true).First(&plan, sku.PlanID).Error; err != nil {
		BadRequestFields(w, "订单创建失败。", map[string]string{"plan_sku_id": "商品不可购买。"})
		return
	}
	billingModes, operationMap, err := loadPlanSKUCommerceMetadata(h.db, []model.PlanSKU{sku})
	if err != nil {
		ServerError(w, err)
		return
	}
	var target *model.Subscription
	var targetSubscriptionID *uint
	if request.TargetSubscriptionID != 0 {
		var subscription model.Subscription
		if err := h.db.Where("id = ? AND user_id = ?", request.TargetSubscriptionID, claims.UserID).First(&subscription).Error; err != nil {
			BadRequestFields(w, "订单创建失败。", map[string]string{"target_subscription_id": "目标订阅不存在。"})
			return
		}
		target = &subscription
		targetSubscriptionID = &subscription.ID
	}
	orderType, err := deriveOrderTypeForSKU(plan.ID, billingModes[sku.ID], operationMap[sku.ID], target)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	if orderType == "renewal" && !plan.IsRenewable {
		BadRequestFields(w, "订单创建失败。", map[string]string{"plan_sku_id": "该商品不支持续费。"})
		return
	}
	if assertion := strings.ToLower(strings.TrimSpace(request.OrderType)); assertion != "" && assertion != orderType {
		BadRequestFields(w, "订单创建失败。", map[string]string{
			"order_type": fmt.Sprintf("订单类型由购买上下文确定为 %s，客户端不能覆盖。", orderType),
		})
		return
	}
	channel := strings.TrimSpace(request.Channel)
	if channel == "" {
		channel = "manual"
	}
	trafficBytes := plan.TrafficBytes
	deviceLimit := plan.DeviceLimit
	speedLimitMbps := plan.SpeedLimitMbps
	if orderType == "traffic_pack" {
		trafficBytes = sku.TrafficBytes
		deviceLimit = 0
		speedLimitMbps = 0
	}
	order := model.Order{
		UserID: claims.UserID, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: uuid.NewString(), OrderType: orderType, TargetSubscriptionID: targetSubscriptionID,
		AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents, Currency: sku.Currency,
		Channel: channel, Status: orderStatusPending,
		PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit,
		BillingValue: sku.BillingValue, TrafficBytes: trafficBytes,
		DeviceLimit: deviceLimit, SpeedLimitMbps: speedLimitMbps,
	}
	if err := h.db.Create(&order).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, order)
}

// Keep time imported in this file as the commerce response types continue to
// expose the same timestamp contract as the legacy handlers.
var _ = time.Time{}
