package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const commerceRequestBodyLimit = 1 << 20

func decodeReusableCommerceBody(r *http.Request, out interface{}) error {
	if r == nil || r.Body == nil {
		return errors.New("empty body")
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, commerceRequestBodyLimit+1))
	if err != nil {
		return err
	}
	if len(payload) > commerceRequestBodyLimit {
		return errors.New("request body is too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if err := json.Unmarshal(payload, out); err != nil {
		return err
	}
	return nil
}

// PlanUpdateCommerceHandler keeps the existing plan update contract while
// strengthening the publication invariant introduced by SKU operations. An
// active product must expose at least one active price that allows purchase.
func (h *handlers) PlanUpdateCommerceHandler(w http.ResponseWriter, r *http.Request) {
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
	var plan model.Plan
	if err := h.db.First(&plan, planID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	targetActive := plan.IsActive
	if request.IsActive != nil {
		targetActive = *request.IsActive
	}
	if targetActive {
		var purchasableSKUCount int64
		if err := h.db.Table("plan_skus").
			Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", skuOperationPurchase).
			Where("plan_skus.plan_id = ? AND plan_skus.is_active = ?", plan.ID, true).
			Count(&purchasableSKUCount).Error; err != nil {
			ServerError(w, err)
			return
		}
		if purchasableSKUCount == 0 {
			BadRequestFields(w, "商品信息校验失败。", map[string]string{
				"is_active": "已发布商品至少需要一个允许新购的可售 SKU。",
			})
			return
		}
	}
	h.PlanUpdateHandler(w, r)
}

// PlanSKUUpdateCommerceGuardedHandler prevents editing or disabling the last
// purchasable SKU of a published product. The underlying handler continues to
// own normalization, persistence and audit logging.
func (h *handlers) PlanSKUUpdateCommerceGuardedHandler(w http.ResponseWriter, r *http.Request) {
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
	var existing model.PlanSKU
	if err := h.db.First(&existing, skuID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	normalized, err := normalizeCommercePlanSKU(existing.PlanID, request)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	var plan model.Plan
	if err := h.db.First(&plan, existing.PlanID).Error; err != nil {
		ServerError(w, err)
		return
	}
	removesPurchaseAvailability := !normalized.SKU.IsActive ||
		!containsSKUOperation(normalized.AllowedOperations, skuOperationPurchase)
	if plan.IsActive && removesPurchaseAvailability {
		var otherPurchasableSKUCount int64
		if err := h.db.Table("plan_skus").
			Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", skuOperationPurchase).
			Where("plan_skus.plan_id = ? AND plan_skus.id <> ? AND plan_skus.is_active = ?", existing.PlanID, existing.ID, true).
			Count(&otherPurchasableSKUCount).Error; err != nil {
			ServerError(w, err)
			return
		}
		if otherPurchasableSKUCount == 0 {
			BadRequestFields(w, "销售规格校验失败。", map[string]string{
				"allowed_operations": "已发布商品必须保留至少一个允许新购的可售 SKU。",
			})
			return
		}
	}
	h.PlanSKUUpdateCommerceHandler(w, r)
}
