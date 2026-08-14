package handler

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

const nodeDiagnosticSnapshotTimeout = 15 * time.Second

type nodeDiagnosticCommandResult struct {
	output []byte
	err    error
}

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
	var node model.Node
	if err := h.db.First(&node, nodeID).Error; err != nil {
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

	var endpoints []model.ProtocolEndpoint
	if err := h.db.Select("id", "node_id", "name", "protocol", "port").
		Where("node_id = ? AND is_active = 1", node.ID).
		Order("sort_order asc, id asc").Find(&endpoints).Error; err != nil {
		ServerError(w, err)
		return
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
	start := time.Now()
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return "", time.Since(start), err
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return "", time.Since(start), err
	}
	defer session.Close()

	prepared, stdin, requestPTY, err := h.prepareSSHCommand(node, command, privileged)
	if err != nil {
		return "", time.Since(start), err
	}
	if requestPTY {
		modes := ssh.TerminalModes{ssh.ECHO: 0, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
		if err := session.RequestPty("xterm", 24, 80, modes); err != nil {
			return "", time.Since(start), fmt.Errorf("request privilege terminal: %w", err)
		}
	}
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}

	result := make(chan nodeDiagnosticCommandResult, 1)
	go func() {
		out, runErr := session.CombinedOutput(prepared)
		result <- nodeDiagnosticCommandResult{output: out, err: runErr}
	}()

	timer := time.NewTimer(nodeDiagnosticSnapshotTimeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return string(bytes.TrimSpace(completed.output)), time.Since(start), completed.err
	case <-timer.C:
		_ = session.Close()
		_ = conn.Close()
		return "", time.Since(start), fmt.Errorf("diagnostic snapshot exceeded %s timeout", nodeDiagnosticSnapshotTimeout)
	}
}
