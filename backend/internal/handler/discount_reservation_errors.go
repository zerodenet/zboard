package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
)

func writeDiscountReservationError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, commerce.ErrDiscountRedemptionPending):
		writeCapabilityError(w, http.StatusConflict, "优惠预留尚未确认或已撤销，不能付款；订单金额保持不变，请核对原订单或取消。", "discount_reservation_pending")
	case errors.Is(err, commerce.ErrDiscountRedemptionUnsupported):
		writeCapabilityError(w, http.StatusConflict, "优惠提供方不支持可靠的排他预留，无法确认本次优惠；未扣款，不会恢复原价。", "discount_reservation_unsupported")
	case errors.Is(err, commerce.ErrDiscountRedemptionConflict):
		writeCapabilityError(w, http.StatusConflict, "优惠预留依据需要核对，未扣款；请通过工单核对原订单。", "discount_reservation_conflict")
	default:
		return false
	}
	return true
}
