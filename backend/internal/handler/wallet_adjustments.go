package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"net/http"
	"strconv"
	"strings"
)

func adjustmentError(w http.ResponseWriter, err error, conflict string) {
	if errors.Is(err, commerce.ErrWalletAdjustmentConflict) {
		writeJSON(w, http.StatusConflict, conflict, nil)
		return
	}
	if errors.Is(err, commerce.ErrNotFound) {
		NotFound(w)
		return
	}
	walletError(w, err)
}
func adjustmentPage(r *http.Request) (uint, int, error) {
	var after uint64
	var err error
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	return uint(after), limit, err
}
func (h *handlers) AdminUserWalletHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	user, err := parsePathID(r.URL.Path, "/api/v1/admin/users/")
	if err != nil {
		BadRequest(w, "用户编号无效。")
		return
	}
	after, limit, err := adjustmentPage(r)
	if err != nil {
		BadRequest(w, "分页参数无效。")
		return
	}
	out, err := h.services.WalletAdjustments.AdministrativeWallet(r.Context(), actor.UserID, user, r.URL.Query().Get("currency"), after, limit)
	if err != nil {
		adjustmentError(w, err, "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
func (h *handlers) AdminWalletAdjustmentsHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method == http.MethodPost {
		raw, err := integrationBody(w, r)
		if err != nil {
			BadRequest(w, "扣减请求无效。")
			return
		}
		in, err := catalog.DecodeObject[commerce.WalletDebitRequest](raw)
		if err != nil {
			BadRequest(w, "扣减请求包含无效或多余字段。")
			return
		}
		out, err := h.services.WalletAdjustments.Debit(r.Context(), actor.UserID, in)
		if err != nil {
			adjustmentError(w, err, "该请求标识已经用于不同的扣减内容，请先核对已有记录。")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		OK(w, out)
		return
	}
	user, err := strconv.ParseUint(r.URL.Query().Get("user_id"), 10, 64)
	if err != nil || user == 0 {
		BadRequest(w, "请选择目标用户。")
		return
	}
	after, limit, err := adjustmentPage(r)
	if err != nil {
		BadRequest(w, "分页参数无效。")
		return
	}
	out, err := h.services.WalletAdjustments.List(r.Context(), actor.UserID, uint(user), r.URL.Query().Get("currency"), after, limit)
	if err != nil {
		adjustmentError(w, err, "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
func (h *handlers) AdminWalletAdjustmentHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/wallet-adjustments/")
	if err != nil {
		BadRequest(w, "调整编号无效。")
		return
	}
	if r.Method == http.MethodGet {
		out, err := h.services.WalletAdjustments.Read(r.Context(), actor.UserID, id)
		if err != nil {
			adjustmentError(w, err, "")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		OK(w, out)
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "记录请求无效。")
		return
	}
	var out commerce.WalletAdjustmentSummary
	conflict := "已确认的外部结果与本次记录冲突，请通过工单核对；不会自动调整余额。"
	if strings.HasSuffix(r.URL.Path, "/payout-results") {
		in, e := catalog.DecodeObject[commerce.WalletPayoutRequest](raw)
		if e != nil {
			BadRequest(w, "出款结果包含无效或多余字段。")
			return
		}
		out, err = h.services.WalletAdjustments.RecordPayout(r.Context(), actor.UserID, id, in)
	} else {
		in, e := catalog.DecodeObject[commerce.WalletReversalRequest](raw)
		if e != nil {
			BadRequest(w, "余额恢复请求无效。")
			return
		}
		out, err = h.services.WalletAdjustments.Reverse(r.Context(), actor.UserID, id, in)
		conflict = "只有外部结果已确认失败的扣减可以恢复，未知或成功结果不能反向记账。"
	}
	if err != nil {
		adjustmentError(w, err, conflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, out)
}
