package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	commerceErrorPlanSlugConflict             = "plan_slug_conflict"
	commerceErrorPlanSKUCodeConflict          = "plan_sku_code_conflict"
	commerceErrorIdentifierConflict           = "commerce_identifier_conflict"
	commerceErrorPlanSubscriptionLimitReached = "plan_subscription_limit_reached"
	commerceErrorPersistenceFailed            = "commerce_persistence_failed"
)

var errPlanSubscriptionLimitReached = errors.New("plan subscription limit reached")

type bufferedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header), status: http.StatusOK}
}

func (w *bufferedResponseWriter) Header() http.Header { return w.header }

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status != http.StatusOK || w.body.Len() > 0 {
		return
	}
	w.status = status
}

func (w *bufferedResponseWriter) Write(payload []byte) (int, error) {
	return w.body.Write(payload)
}

func flushBufferedResponse(w http.ResponseWriter, buffered *bufferedResponseWriter) {
	for key, values := range buffered.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(buffered.status)
	_, _ = w.Write(buffered.body.Bytes())
}

func writeCommerceError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSONResponse(w, status, message, nil, &APIError{Version: 1, Code: code, Fields: fields})
}

func writePlanSlugConflict(w http.ResponseWriter) {
	writeCommerceError(w, http.StatusBadRequest, commerceErrorPlanSlugConflict, "商品标识已被使用。", map[string]string{
		"slug": "该商品标识已被其他商品使用，请更换后重试。",
	})
}

func writePlanSKUCodeConflict(w http.ResponseWriter, field string) {
	if field == "" {
		field = "code"
	}
	writeCommerceError(w, http.StatusBadRequest, commerceErrorPlanSKUCodeConflict, "SKU 编码已被使用。", map[string]string{
		field: "该 SKU 编码已被其他销售规格使用，请更换后重试。",
	})
}

func writeIdentifierConflict(w http.ResponseWriter) {
	writeCommerceError(w, http.StatusBadRequest, commerceErrorIdentifierConflict, "商品或销售规格的标识已被使用，请检查商品标识和 SKU 编码。", nil)
}

func writePlanSubscriptionLimitReached(w http.ResponseWriter) {
	writeCommerceError(w, http.StatusConflict, commerceErrorPlanSubscriptionLimitReached, "该套餐的有效订阅数量已达到上限，暂时无法创建新的订阅订单。", nil)
}

func writeCommercePersistenceFailure(w http.ResponseWriter, message string) {
	writeCommerceError(w, http.StatusInternalServerError, commerceErrorPersistenceFailed, message, nil)
}

func responseMessage(payload []byte) string {
	var response APIResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return ""
	}
	return strings.TrimSpace(response.Message)
}

func isDuplicatePersistenceResponse(buffered *bufferedResponseWriter) bool {
	if buffered == nil || buffered.status < http.StatusBadRequest {
		return false
	}
	text := strings.ToLower(buffered.body.String())
	return strings.Contains(text, "duplicate entry") ||
		strings.Contains(text, "error 1062") ||
		strings.Contains(text, "sqlstate 23000") ||
		strings.Contains(text, "unique constraint")
}

func looksLikeDatabaseResponse(buffered *bufferedResponseWriter) bool {
	if buffered == nil || buffered.status < http.StatusInternalServerError {
		return false
	}
	text := strings.ToLower(buffered.body.String())
	return strings.Contains(text, "sqlstate") ||
		strings.Contains(text, "error 10") ||
		strings.Contains(text, "select ") ||
		strings.Contains(text, "insert into") ||
		strings.Contains(text, "update `")
}

func translateCommerceMutationResponse(w http.ResponseWriter, buffered *bufferedResponseWriter, defaultConflictField string) {
	if isDuplicatePersistenceResponse(buffered) {
		text := strings.ToLower(buffered.body.String())
		switch {
		case strings.Contains(text, "slug"):
			writePlanSlugConflict(w)
		case strings.Contains(text, "code"):
			writePlanSKUCodeConflict(w, defaultConflictField)
		default:
			writeIdentifierConflict(w)
		}
		return
	}
	if looksLikeDatabaseResponse(buffered) {
		writeCommercePersistenceFailure(w, "商品数据保存失败，请稍后重试。")
		return
	}
	flushBufferedResponse(w, buffered)
}

func (h *handlers) PlanCreateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var request commercePlanCreateRequest
	if err := decodeReusableCommerceBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}

	fields := make(map[string]string)
	slug := strings.ToLower(strings.TrimSpace(request.Slug))
	if slug != "" {
		var count int64
		if err := h.db.Model(&model.Plan{}).Where("slug = ?", slug).Count(&count).Error; err != nil {
			writeCommercePersistenceFailure(w, "商品标识校验失败，请稍后重试。")
			return
		}
		if count > 0 {
			fields["slug"] = "该商品标识已被其他商品使用，请更换后重试。"
		}
	}
	for index, sku := range request.SKUs {
		code := strings.ToLower(strings.TrimSpace(sku.Code))
		if code == "" {
			continue
		}
		var count int64
		if err := h.db.Model(&model.PlanSKU{}).Where("code = ?", code).Count(&count).Error; err != nil {
			writeCommercePersistenceFailure(w, "SKU 编码校验失败，请稍后重试。")
			return
		}
		if count > 0 {
			fields["skus."+strconv.Itoa(index)+".code"] = "该 SKU 编码已被其他销售规格使用，请更换后重试。"
		}
	}
	if len(fields) > 0 {
		writeCommerceError(w, http.StatusBadRequest, commerceErrorIdentifierConflict, "商品或销售规格的标识已被使用。", fields)
		return
	}

	buffered := newBufferedResponseWriter()
	h.PlanCreateCommerceHandler(buffered, r)
	translateCommerceMutationResponse(w, buffered, "skus.0.code")
}

func (h *handlers) PlanUpdateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	planID, err := parsePathID(r.URL.Path, "/api/v1/admin/plans/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request planUpdateReq
	if err := decodeReusableCommerceBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if request.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*request.Slug))
		if slug != "" {
			var count int64
			if err := h.db.Model(&model.Plan{}).Where("slug = ? AND id <> ?", slug, planID).Count(&count).Error; err != nil {
				writeCommercePersistenceFailure(w, "商品标识校验失败，请稍后重试。")
				return
			}
			if count > 0 {
				writePlanSlugConflict(w)
				return
			}
		}
	}
	buffered := newBufferedResponseWriter()
	h.PlanUpdateCommerceHandler(buffered, r)
	translateCommerceMutationResponse(w, buffered, "slug")
}

func (h *handlers) PlanSKUCreateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var request commercePlanSKURequest
	if err := decodeReusableCommerceBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if code := strings.ToLower(strings.TrimSpace(request.Code)); code != "" {
		var count int64
		if err := h.db.Model(&model.PlanSKU{}).Where("code = ?", code).Count(&count).Error; err != nil {
			writeCommercePersistenceFailure(w, "SKU 编码校验失败，请稍后重试。")
			return
		}
		if count > 0 {
			writePlanSKUCodeConflict(w, "code")
			return
		}
	}
	buffered := newBufferedResponseWriter()
	h.PlanSKUCreateCommerceHandler(buffered, r)
	translateCommerceMutationResponse(w, buffered, "code")
}

func (h *handlers) PlanSKUUpdateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	skuID, err := parsePathID(r.URL.Path, "/api/v1/admin/plan-skus/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var request commercePlanSKURequest
	if err := decodeReusableCommerceBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if code := strings.ToLower(strings.TrimSpace(request.Code)); code != "" {
		var count int64
		if err := h.db.Model(&model.PlanSKU{}).Where("code = ? AND id <> ?", code, skuID).Count(&count).Error; err != nil {
			writeCommercePersistenceFailure(w, "SKU 编码校验失败，请稍后重试。")
			return
		}
		if count > 0 {
			writePlanSKUCodeConflict(w, "code")
			return
		}
	}
	buffered := newBufferedResponseWriter()
	h.PlanSKUUpdateCommerceGuardedHandler(buffered, r)
	translateCommerceMutationResponse(w, buffered, "code")
}

func ensurePlanSubscriptionCapacity(db *gorm.DB, plan model.Plan, now time.Time) error {
	if plan.MaxActiveSubscriptions <= 0 {
		return nil
	}
	var activeCount int64
	if err := db.Model(&model.Subscription{}).
		Where("plan_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", plan.ID, subStatusActive, now).
		Count(&activeCount).Error; err != nil {
		return err
	}
	if activeCount >= int64(plan.MaxActiveSubscriptions) {
		return errPlanSubscriptionLimitReached
	}
	return nil
}

func (h *handlers) OrderCreateCommerceValidatedHandler(w http.ResponseWriter, r *http.Request) {
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

	var order model.Order
	err = h.db.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, claims.UserID).Error; err != nil {
			return err
		}
		var sku model.PlanSKU
		if err := tx.Where("is_active = ?", true).First(&sku, request.PlanSKUID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validationError("订单创建失败。", map[string]string{"plan_sku_id": "销售规格不存在或已停止销售。"})
			}
			return err
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("is_active = ?", true).First(&plan, sku.PlanID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validationError("订单创建失败。", map[string]string{"plan_sku_id": "商品不可购买。"})
			}
			return err
		}
		_, entitlementModes, operationMap, err := loadPlanSKUCommerceMetadata(tx, []model.PlanSKU{sku})
		if err != nil {
			return err
		}
		var target *model.Subscription
		var targetSubscriptionID *uint
		if request.TargetSubscriptionID != 0 {
			var subscription model.Subscription
			if err := tx.Where("id = ? AND user_id = ?", request.TargetSubscriptionID, claims.UserID).First(&subscription).Error; err != nil {
				return validationError("订单创建失败。", map[string]string{"target_subscription_id": "目标订阅不存在。"})
			}
			target = &subscription
			targetSubscriptionID = &subscription.ID
		}
		orderType, err := deriveOrderTypeForSKU(plan.ID, entitlementModes[sku.ID], operationMap[sku.ID], target)
		if err != nil {
			return err
		}
		if orderType == "renewal" && !plan.IsRenewable {
			return validationError("订单创建失败。", map[string]string{"plan_sku_id": "该商品不支持续费。"})
		}
		if assertion := strings.ToLower(strings.TrimSpace(request.OrderType)); assertion != "" && assertion != orderType {
			return validationError("订单创建失败。", map[string]string{"order_type": "订单类型与当前购买操作不一致，请返回套餐详情后重试。"})
		}
		if orderType == "new" {
			if err := ensurePlanSubscriptionCapacity(tx, plan, time.Now().UTC()); err != nil {
				return err
			}
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
		order = model.Order{
			UserID: claims.UserID, PlanID: plan.ID, PlanSKUID: sku.ID,
			TradeNo: uuid.NewString(), OrderType: orderType, TargetSubscriptionID: targetSubscriptionID,
			AmountCents: sku.PriceCents, PayableAmount: sku.PriceCents, Currency: sku.Currency,
			Channel: channel, Status: orderStatusPending,
			PlanName: plan.Name, SKUName: sku.Name, BillingUnit: sku.BillingUnit,
			BillingValue: sku.BillingValue, RenewalEffect: sku.RenewalEffect, TrafficBytes: trafficBytes,
			DeviceLimit: deviceLimit, SpeedLimitMbps: speedLimitMbps,
		}
		return tx.Create(&order).Error
	})
	if errors.Is(err, errPlanSubscriptionLimitReached) {
		writePlanSubscriptionLimitReached(w)
		return
	}
	if err != nil {
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, err)
			return
		}
		writeCommercePersistenceFailure(w, "订单创建失败，请稍后重试。")
		return
	}
	OK(w, order)
}

func isPlanSubscriptionLimitResponse(buffered *bufferedResponseWriter) bool {
	if buffered == nil || buffered.status < http.StatusBadRequest {
		return false
	}
	message := responseMessage(buffered.body.Bytes())
	return message == "plan subscription capacity is exhausted" || message == errPlanSubscriptionLimitReached.Error()
}

func (h *handlers) OrderPayCommerceHandler(w http.ResponseWriter, r *http.Request) {
	buffered := newBufferedResponseWriter()
	h.OrderPayHandler(buffered, r)
	if isPlanSubscriptionLimitResponse(buffered) {
		writePlanSubscriptionLimitReached(w)
		return
	}
	flushBufferedResponse(w, buffered)
}

func (h *handlers) OrderPayCallbackCommerceHandler(w http.ResponseWriter, r *http.Request) {
	buffered := newBufferedResponseWriter()
	h.OrderPayCallbackHandler(buffered, r)
	if isPlanSubscriptionLimitResponse(buffered) {
		writePlanSubscriptionLimitReached(w)
		return
	}
	flushBufferedResponse(w, buffered)
}
