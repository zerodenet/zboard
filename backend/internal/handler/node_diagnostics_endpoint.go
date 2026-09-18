package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const nodeDiagnosticSnapshotTimeout = 15 * time.Second

// NodeRuntimeDiagnosticsHandler runs an operator-triggered, read-only service
// check for the Zero protocols that Zboard has actually assigned to this node.
// Host-level evidence is deliberately kept internal and is reduced to business
// status before crossing the API boundary.
func (h *handlers) NodeRuntimeDiagnosticsHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNodeContext(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, "节点尚未完成 SSH 验证，无法运行诊断")
		return
	}

	rows, err := h.services.NetworkInventory.DiagnosticEndpoints(r.Context(), node.ID)
	if err != nil {
		ServerError(w, err)
		return
	}
	endpoints := make([]model.ProtocolEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, model.ProtocolEndpoint{ID: row.ID, NodeID: row.NodeID, Name: row.Name, Protocol: row.Protocol, Port: row.Port})
	}

	snapshot := newNodeDiagnosticSnapshot(node.ID, endpoints)
	output, _, err := h.execNodeDiagnosticCommand(node, nodeDiagnosticCommand, normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) != sshPrivilegeNone)
	if err != nil {
		markNodeDiagnosticSSHUnavailable(&snapshot)
		OK(w, snapshot)
		return
	}
	evaluateNodeDiagnosticSnapshot(&snapshot, endpoints, output)
	OK(w, snapshot)
}

func (h *handlers) execNodeDiagnosticCommand(node model.Node, command string, privileged bool) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nodeDiagnosticSnapshotTimeout)
	defer cancel()
	return h.execSSHCommandWithPrivilegeContext(ctx, node, command, privileged)
}
