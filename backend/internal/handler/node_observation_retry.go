package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func (h *handlers) NodeObservationIntentRetryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	const prefix = "/api/v1/admin/node-observation-intents/"
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/retry") || r.URL.RawQuery != "" {
		BadRequest(w, "观察重试路径或参数无效。")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/retry")
	var in network.ObservationIntentRetryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		BadRequest(w, "请提供原领取次数和明确原因，不要添加其他字段。")
		return
	}
	out, err := h.services.ObservationIntents.Retry(r.Context(), actor.UserID, id, in)
	switch {
	case errors.Is(err, network.ErrObservationIntentRetryInvalid):
		BadRequest(w, "原领取次数、材料编号或3–400字节原因无效。")
		return
	case errors.Is(err, network.ErrObservationIntentRetryConflict):
		writeJSON(w, http.StatusConflict, "原记录已变化或不满足重试条件（仅限已恢复的同一启用资产）；请刷新核对原请求，已接收的申请不会重复重排。", nil)
		return
	case err != nil:
		if !writeNodeAdministrationError(w, err) {
			ServerError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusAccepted, "原材料已重新排队；执行器仍会核对来源、原资产和观察顺序。", out)
}
