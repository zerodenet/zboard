package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func (h *handlers) AutomaticRenewalHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, err := h.authFromRequest(r)
	if err != nil {
		Unauthorized(w, "需要登录。")
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/subscriptions/")
	if err != nil {
		BadRequest(w, "订阅编号无效。")
		return
	}
	service := h.services.AutomaticRenewals(h.credentialCipher, h.zeroMieruAccess)
	if r.Method == http.MethodGet {
		var sku uint64
		if raw := r.URL.Query().Get("plan_sku_id"); raw != "" {
			sku, err = strconv.ParseUint(raw, 10, 64)
			if err != nil || sku == 0 {
				BadRequest(w, "续费规格编号无效。")
				return
			}
		}
		out, err := service.Read(r.Context(), claims.UserID, id, uint(sku))
		if err != nil {
			subscriptionValueError(w, err)
			return
		}
		OK(w, out)
		return
	}
	raw, err := integrationBody(w, r)
	if err != nil {
		BadRequest(w, "自动续费请求无效。")
		return
	}
	in, err := catalog.DecodeObject[commerce.AutomaticRenewalRequest](raw)
	if err != nil {
		BadRequest(w, "自动续费仅接受开关、续费规格、报价确认和版本；不能指定金额或其他账户。")
		return
	}
	out, err := service.Set(r.Context(), claims.UserID, id, in)
	if err != nil {
		subscriptionValueError(w, err)
		return
	}
	OK(w, out)
}

func (h *handlers) StartAutomaticRenewalWorker() {
	if h == nil || h.services == nil {
		return
	}
	h.startScheduledJob("automatic_renewal", time.Minute, func(ctx context.Context) error {
		// One bounded page; persisted retry dates and ordered selection prevent
		// empty/insufficient subscriptions from creating a busy loop.
		_, err := h.services.AutomaticRenewals(h.credentialCipher, h.zeroMieruAccess).Process(ctx, 200)
		return err
	})
}
