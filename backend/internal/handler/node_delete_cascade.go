package handler

import (
	"net/http"
)

func (h *handlers) NodeCascadeDeleteHandler(w http.ResponseWriter, r *http.Request) {
	h.nodeCascadeDeleteHandler(w, r)
}

func (h *handlers) nodeCascadeDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	lock := h.nodePublishLock(id)
	if err := lock.Lock(r.Context()); err != nil {
		BadRequest(w, err.Error())
		return
	}
	defer lock.Unlock()
	h.backgroundJobs()
	result, err := h.services.NodeRetirement(h).Repository.Start(r.Context(), claims.UserID, id, "delete")
	if err != nil {
		resourceRemovalError(w, err)
		return
	}
	h.invalidateZeroEventCredential(id)
	writeJSON(w, http.StatusAccepted, "节点删除任务已提交", result)
}
