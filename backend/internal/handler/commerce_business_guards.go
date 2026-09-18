package handler

import (
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

const (
	commerceErrorPlanSlugConflict             = "plan_slug_conflict"
	commerceErrorPlanSKUCodeConflict          = "plan_sku_code_conflict"
	commerceErrorIdentifierConflict           = "commerce_identifier_conflict"
	commerceErrorPlanSubscriptionLimitReached = "plan_subscription_limit_reached"
	commerceErrorPersistenceFailed            = "commerce_persistence_failed"
)

var errPlanSubscriptionLimitReached = commerce.ErrSubscriptionCapacity

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

func writePlanSubscriptionLimitReached(w http.ResponseWriter) {
	writeCommerceError(w, http.StatusConflict, commerceErrorPlanSubscriptionLimitReached, "该套餐的有效订阅数量已达到上限，暂时无法创建新的订阅订单。", nil)
}

func writeCommercePersistenceFailure(w http.ResponseWriter, message string) {
	writeCommerceError(w, http.StatusInternalServerError, commerceErrorPersistenceFailed, message, nil)
}

// Preserve existing route names while the handlers map typed settlement errors directly.
func (h *handlers) OrderPayCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.orderPayHandler(w, r)
}

func (h *handlers) OrderPayCallbackCommerceHandler(w http.ResponseWriter, r *http.Request) {
	h.orderPayCallbackHandler(w, r)
}

func writeCommerceError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSONResponse(w, status, message, nil, &APIError{Version: 1, Code: code, Fields: fields})
}
