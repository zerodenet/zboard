package handler

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

type nodeDiagnosticPublicTarget struct {
	ListenerIndex int
	Host          string
	Port          int
}

// NodeRuntimeDiagnosticsHandler is the HTTP orchestration layer for #42. It
// keeps the parser independent from database/public-endpoint concerns, gives
// the SSH snapshot a hard deadline, and probes only explicit ProtocolEndpoint
// public addresses rather than guessing from Node.Address.
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
		BadRequest(w, "节点尚未完成 SSH 验证，无法运行诊断："+err.Error())
		return
	}

	output, latency, err := h.execNodeDiagnosticCommand(node, nodeDiagnosticCommand, normalizeSSHPrivilegeMode(node.SSHPrivilegeMode) != sshPrivilegeNone)
	if err != nil {
		BadRequest(w, "运行节点诊断失败："+err.Error())
		return
	}
	snapshot := parseNodeDiagnosticSnapshot(node, output)
	snapshot.NodeID = node.ID
	snapshot.CapturedAt = time.Now().UTC()
	snapshot.LatencyMS = latency.Milliseconds()
	if snapshot.Service.ActiveState == "" {
		snapshot.Warnings = append(snapshot.Warnings, "未取得 systemd 服务状态；诊断不会据此推断服务健康。")
	}
	if err := h.applyProtocolEndpointReachability(node.ID, &snapshot); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "无法读取协议公开入口，已跳过外部 TCP 可达性判断。")
	}
	classifyNodeDiagnostic(&snapshot)
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

func (h *handlers) applyProtocolEndpointReachability(nodeID uint, snapshot *nodeDiagnosticSnapshot) error {
	for index := range snapshot.ExpectedListeners {
		snapshot.ExpectedListeners[index].ExternalReachability = "not_checked"
	}
	if len(snapshot.ExpectedListeners) == 0 {
		return nil
	}

	var endpoints []model.ProtocolEndpoint
	if err := h.db.Select("id", "protocol", "address", "port", "public_port").
		Where("node_id = ? AND is_active = 1", nodeID).
		Order("sort_order asc, id asc").Find(&endpoints).Error; err != nil {
		return err
	}

	targets := make([]nodeDiagnosticPublicTarget, 0)
	seen := make(map[string]struct{})
	for listenerIndex, listener := range snapshot.ExpectedListeners {
		if !listener.Present || !containsNodeDiagnosticNetwork(listener.Networks, "tcp") {
			continue
		}
		for _, endpoint := range endpoints {
			if endpoint.Port != int(listener.Port) || !strings.EqualFold(strings.TrimSpace(endpoint.Protocol), strings.TrimSpace(listener.Protocol)) {
				continue
			}
			host := nodeDiagnosticProbeHost(endpoint.Address)
			if host == "" {
				continue
			}
			port := endpoint.PublicPort
			if port <= 0 {
				port = endpoint.Port
			}
			if port <= 0 || port > 65535 {
				continue
			}
			key := strconv.Itoa(listenerIndex) + "|" + host + "|" + strconv.Itoa(port)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			targets = append(targets, nodeDiagnosticPublicTarget{ListenerIndex: listenerIndex, Host: host, Port: port})
			break
		}
		if len(targets) >= nodeDiagnosticMaxExternalPorts {
			snapshot.Warnings = append(snapshot.Warnings, "外部 TCP 可达性探测已达到单次 32 个公开入口上限。")
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}

	snapshot.Capabilities.TCPExternalReachability = true
	var wait sync.WaitGroup
	var mu sync.Mutex
	for _, target := range targets {
		target := target
		wait.Add(1)
		go func() {
			defer wait.Done()
			address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
			conn, dialErr := net.DialTimeout("tcp", address, nodeDiagnosticExternalTimeout)
			if conn != nil {
				_ = conn.Close()
			}
			mu.Lock()
			defer mu.Unlock()
			if dialErr == nil {
				snapshot.ExpectedListeners[target.ListenerIndex].ExternalReachability = "reachable"
			} else {
				snapshot.ExpectedListeners[target.ListenerIndex].ExternalReachability = "unreachable"
			}
		}()
	}
	wait.Wait()
	return nil
}
