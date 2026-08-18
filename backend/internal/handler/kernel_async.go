package handler

import (
	"errors"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// NodeKernelReconcileAsyncHandler accepts the operator intent and hands the
// long-running SSH/activation/Connector verification work to the persisted
// Task/TaskItem executor. Request cancellation therefore cannot cancel an
// accepted kernel generation or cause a browser timeout to trigger rollback.
func (h *handlers) NodeKernelReconcileAsyncHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	request, err := decodeKernelReconcileRequest(r)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	request.Version = strings.TrimSpace(request.Version)
	if request.Version == "" {
		BadRequest(w, "version is required for kernel reconcile")
		return
	}

	content := operationTaskContent{
		RequestedBy:    claims.UserID,
		Actor:          claims.Email,
		KernelVersion:  request.Version,
		AllowDowngrade: request.AllowDowngrade,
	}
	scope := map[string]interface{}{
		"node_ids":       []uint{node.ID},
		"kernel_version": request.Version,
	}
	task, err := h.createOperationTask(claims, taskTypeNodeReconcile, scope, content, "node", []uint{node.ID}, "")
	if err != nil {
		writeOperationTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "kernel reconcile task accepted", task)
}
