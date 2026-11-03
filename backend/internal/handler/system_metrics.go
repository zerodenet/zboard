package handler

import (
	"net/http"
	"strconv"
	"time"
)

func (h *handlers) AdminSystemMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	minutes := 360
	if value := r.URL.Query().Get("range"); value != "" {
		v, err := strconv.Atoi(value)
		if err != nil || (v != 60 && v != 360 && v != 720 && v != 1440) {
			BadRequest(w, "时间范围须为 60、360、720 或 1440 分钟")
			return
		}
		minutes = v
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, h.services.RuntimeMonitor.Snapshot(minutes, time.Now().UTC()))
}
