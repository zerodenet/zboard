package handler

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

// Runtime diagnostics are intentionally scoped to Zboard-owned service facts.
// The SSH command may inspect the host listener table as an ephemeral input,
// but host addresses, ports, process details, logs and resource information are
// never returned by the diagnostics API.
const nodeDiagnosticCommand = `LC_ALL=C; export LC_ALL
set +e
printf '__ZBOARD_DIAG_STATUS_BEGIN__\n'
timeout 3s /usr/local/bin/zero status --json --socket /run/zerodenet/control.sock 2>/dev/null | head -c 262144
printf '\n__ZBOARD_DIAG_STATUS_END__\n'
printf '__ZBOARD_DIAG_SS_BEGIN__\n'
timeout 2s ss -H -lntu 2>/dev/null | head -n 2048
printf '__ZBOARD_DIAG_SS_END__\n'`

const (
	nodeDiagnosticHealthy = "healthy"
	nodeDiagnosticError   = "error"
)

type nodeDiagnosticChecks struct {
	SSH  string `json:"ssh"`
	Zero string `json:"zero"`
}

type nodeDiagnosticProtocolResult struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

type nodeDiagnosticSnapshot struct {
	NodeID    uint                           `json:"node_id"`
	Status    string                         `json:"status"`
	Checks    nodeDiagnosticChecks           `json:"checks"`
	Protocols []nodeDiagnosticProtocolResult `json:"protocols"`
}

type nodeDiagnosticListener struct {
	Network string
	Port    uint16
}

func newNodeDiagnosticSnapshot(nodeID uint, endpoints []model.ProtocolEndpoint) nodeDiagnosticSnapshot {
	protocols := make([]nodeDiagnosticProtocolResult, 0, len(endpoints))
	for _, endpoint := range endpoints {
		protocols = append(protocols, nodeDiagnosticProtocolResult{
			Name:     strings.TrimSpace(endpoint.Name),
			Protocol: strings.ToLower(strings.TrimSpace(endpoint.Protocol)),
			Status:   nodeDiagnosticError,
			Reason:   "not_checked",
		})
	}
	return nodeDiagnosticSnapshot{
		NodeID: nodeID,
		Status: nodeDiagnosticError,
		Checks: nodeDiagnosticChecks{
			SSH:  nodeDiagnosticError,
			Zero: nodeDiagnosticError,
		},
		Protocols: protocols,
	}
}

func evaluateNodeDiagnosticSnapshot(snapshot *nodeDiagnosticSnapshot, endpoints []model.ProtocolEndpoint, output string) {
	snapshot.Checks.SSH = nodeDiagnosticHealthy
	statusText := extractNodeDiagnosticSection(output, "STATUS")
	if !nodeDiagnosticRuntimeAvailable(statusText) {
		snapshot.Checks.Zero = nodeDiagnosticError
		for index := range snapshot.Protocols {
			snapshot.Protocols[index].Status = nodeDiagnosticError
			snapshot.Protocols[index].Reason = "zero_unavailable"
		}
		return
	}

	snapshot.Checks.Zero = nodeDiagnosticHealthy
	listeners := parseNodeDiagnosticListeners(extractNodeDiagnosticSection(output, "SS"))
	for index, endpoint := range endpoints {
		if endpoint.Port <= 0 || endpoint.Port > 65535 {
			snapshot.Protocols[index].Status = nodeDiagnosticError
			snapshot.Protocols[index].Reason = "config_invalid"
			continue
		}
		if nodeDiagnosticEndpointListening(endpoint, listeners) {
			snapshot.Protocols[index].Status = nodeDiagnosticHealthy
			snapshot.Protocols[index].Reason = ""
			continue
		}
		snapshot.Protocols[index].Status = nodeDiagnosticError
		snapshot.Protocols[index].Reason = "listener_unavailable"
	}

	snapshot.Status = nodeDiagnosticHealthy
	for _, protocol := range snapshot.Protocols {
		if protocol.Status != nodeDiagnosticHealthy {
			snapshot.Status = nodeDiagnosticError
			break
		}
	}
}

func markNodeDiagnosticSSHUnavailable(snapshot *nodeDiagnosticSnapshot) {
	snapshot.Status = nodeDiagnosticError
	snapshot.Checks.SSH = nodeDiagnosticError
	snapshot.Checks.Zero = nodeDiagnosticError
	for index := range snapshot.Protocols {
		snapshot.Protocols[index].Status = nodeDiagnosticError
		snapshot.Protocols[index].Reason = "ssh_unavailable"
	}
}

func extractNodeDiagnosticSection(output, name string) string {
	startMarker := "__ZBOARD_DIAG_" + name + "_BEGIN__"
	endMarker := "__ZBOARD_DIAG_" + name + "_END__"
	start := strings.Index(output, startMarker)
	if start < 0 {
		return ""
	}
	start += len(startMarker)
	end := strings.Index(output[start:], endMarker)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(output[start : start+end])
}

func nodeDiagnosticRuntimeAvailable(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil || len(object) == 0 {
		return false
	}
	if raw, ok := object["runtime"]; ok && len(raw) > 0 && string(raw) != "null" {
		var runtime map[string]json.RawMessage
		if json.Unmarshal(raw, &runtime) == nil && len(runtime) > 0 {
			return true
		}
	}
	// `zero status --socket` may serialize RuntimeSnapshot directly depending on
	// the Core CLI/control-plane generation. Runtime fields are sufficient to
	// prove the Zero control query succeeded; none of them are returned to UI.
	for _, field := range []string{"core_instance_id", "config_revision", "stats", "active_sessions"} {
		if _, ok := object[field]; ok {
			return true
		}
	}
	return false
}

func parseNodeDiagnosticListeners(output string) []nodeDiagnosticListener {
	listeners := make([]nodeDiagnosticListener, 0)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 6 {
			continue
		}
		network := strings.ToLower(fields[0])
		if network != "tcp" && network != "udp" {
			continue
		}
		port, ok := parseNodeDiagnosticPort(fields[len(fields)-2])
		if !ok {
			continue
		}
		key := network + "|" + strconv.Itoa(int(port))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		listeners = append(listeners, nodeDiagnosticListener{Network: network, Port: port})
	}
	return listeners
}

func parseNodeDiagnosticPort(value string) (uint16, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if _, portText, err := net.SplitHostPort(value); err == nil {
		port, err := strconv.ParseUint(portText, 10, 16)
		return uint16(port), err == nil && port > 0
	}
	index := strings.LastIndex(value, ":")
	if index < 0 || index == len(value)-1 {
		return 0, false
	}
	port, err := strconv.ParseUint(value[index+1:], 10, 16)
	return uint16(port), err == nil && port > 0
}

func nodeDiagnosticEndpointListening(endpoint model.ProtocolEndpoint, actual []nodeDiagnosticListener) bool {
	required := nodeDiagnosticEndpointNetworks(endpoint.Protocol)
	for _, network := range required {
		found := false
		for _, listener := range actual {
			if listener.Network == network && int(listener.Port) == endpoint.Port {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func nodeDiagnosticEndpointNetworks(protocol string) []string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "shadowsocks":
		return []string{"tcp", "udp"}
	case "hysteria2", "hysteria", "quic":
		return []string{"udp"}
	default:
		return []string{"tcp"}
	}
}
