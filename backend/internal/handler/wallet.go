package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func walletError(w http.ResponseWriter, err error) {
	if writeResourceBusy(w, err) {
		return
	}
	var invalid *commerce.ValidationError
	switch {
	case errors.Is(err, commerce.ErrOrderPermission):
		Forbidden(w, "无权访问该余额。")
	case errors.As(err, &invalid):
		BadRequestError(w, commerceValidationError(err))
	case errors.Is(err, commerce.ErrWalletRange):
		BadRequest(w, "金额超出支持的整数范围。")
	default:
		writeCommercePersistenceFailure(w, "余额事实暂不可核对，请稍后重试。")
	}
}
func (h *handlers) WalletHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		after, err = 0, nil
	}
	if err != nil {
		BadRequest(w, "流水游标无效。")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			BadRequest(w, "分页参数无效。")
			return
		}
	}
	result, err := h.services.Wallet.Read(r.Context(), claims.UserID, r.URL.Query().Get("currency"), uint(after), limit)
	if err != nil {
		walletError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, result)
}
func (h *handlers) WalletTopUpHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "充值请求无效。")
		return
	}
	request, err := catalog.DecodeObject[commerce.TopUpRequest](raw)
	if err != nil {
		BadRequest(w, "充值请求包含无效或多余字段。")
		return
	}
	result, err := h.services.Wallet.CreateTopUp(r.Context(), claims.UserID, request)
	if err != nil {
		walletError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, result)
}
