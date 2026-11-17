package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
)

func (h *handlers) AdminUserTrafficActivityHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	raw := strings.Split(r.URL.Query().Get("user_ids"), ",")
	if len(raw) > metering.UserActivityBatchLimit {
		BadRequest(w, "一次最多查询 100 个用户。")
		return
	}
	ids := make([]uint, 0, len(raw))
	for _, value := range raw {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 || uint64(uint(id)) != id {
			BadRequest(w, "用户编号无效。")
			return
		}
		ids = append(ids, uint(id))
	}
	now := time.Now().UTC()
	location := h.systemTimezoneLocation()
	out, err := h.services.UserTrafficActivity().Read(r.Context(), actor.UserID, ids, location, now)
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, metering.ErrUsageProjectionPending) {
		today := systemDateAt(now, location)
		w.Header().Set("X-Zboard-Statistics-State", "pending")
		OK(w, metering.UserActivityData{ProjectionPending: true, From: today.AddDate(0, 0, -6).Format("2006-01-02"), To: today.Format("2006-01-02"), Timezone: location.String(), Items: []metering.UserActivityItem{}})
		return
	}
	if err != nil {
		writePrincipalTrendError(w, err)
		return
	}
	w.Header().Set("X-Zboard-Statistics-State", "complete")
	OK(w, out)
}
