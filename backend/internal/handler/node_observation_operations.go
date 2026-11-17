package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
	"net/url"
	"strconv"
)

func (h *handlers) NodeObservationIntentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		BadRequest(w, "查询参数无效。")
		return
	}
	for name, entries := range values {
		if (name != "status" && name != "cursor" && name != "limit") || len(entries) != 1 {
			BadRequest(w, "仅支持单值 status、cursor 和 limit 参数。")
			return
		}
	}
	query := network.ObservationIntentQuery{Status: values.Get("status"), Cursor: values.Get("cursor"), Limit: 50}
	if values.Has("status") && query.Status == "" {
		BadRequest(w, "status 不可为空。")
		return
	}
	if values.Has("cursor") && query.Cursor == "" {
		BadRequest(w, "cursor 不可为空。")
		return
	}
	if values.Has("limit") {
		query.Limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || query.Limit < 1 || query.Limit > 100 {
			BadRequest(w, "limit 必须为 1–100 的整数。")
			return
		}
	}
	page, err := h.services.ObservationIntents.List(r.Context(), actor.UserID, query)
	if errors.Is(err, network.ErrObservationIntentQuery) {
		BadRequest(w, "观察队列筛选或分页游标无效。")
		return
	}
	if err != nil {
		if !writeNodeAdministrationError(w, err) {
			ServerError(w, err)
		}
		return
	}
	OK(w, page)
}
