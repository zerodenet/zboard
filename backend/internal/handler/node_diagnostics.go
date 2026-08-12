package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	nodeDiagnosticJournalMaxBytes  = 64 << 10
	nodeDiagnosticKernelMaxBytes   = 32 << 10
	nodeDiagnosticPressureRatio    = 0.90
	nodeDiagnosticExternalTimeout  = 1200 * time.Millisecond
	nodeDiagnosticMaxExternalPorts = 32
)

var nodeDiagnosticSecretPattern = regexp.MustCompile(`(?i)(password|token|secret|api[_-]?key|authorization|credential)(\s*[:=]\s*)([^\s,;]+)`)

const nodeDiagnosticCommand = `LC_ALL=C; export LC_ALL
set +e
printf '__ZBOARD_DIAG_VERSION_BEGIN__\n'
timeout 2s /usr/local/bin/zero --version 2>/dev/null | head -c 4096
printf '\n__ZBOARD_DIAG_VERSION_END__\n'
printf '__ZBOARD_DIAG_STATUS_BEGIN__\n'
timeout 3s /usr/local/bin/zero status --json --socket /run/zerodenet/control.sock 2>/dev/null | head -c 1048576
printf '\n__ZBOARD_DIAG_STATUS_END__\n'
printf '__ZBOARD_DIAG_SS_BEGIN__\n'
timeout 2s ss -H -lntu 2>/dev/null | head -n 2048
printf '__ZBOARD_DIAG_SS_END__\n'
printf '__ZBOARD_DIAG_SERVICE_BEGIN__\n'
timeout 2s systemctl show zero --no-pager -p ActiveState -p SubState -p MainPID -p ExecMainStatus 2>/dev/null | head -n 16
printf '__ZBOARD_DIAG_SERVICE_END__\n'
pid="$(timeout 2s systemctl show zero -p MainPID --value 2>/dev/null | tr -cd '0-9')"
printf '__ZBOARD_DIAG_FD_BEGIN__\n'
if [ -n "$pid" ] && [ "$pid" != "0" ] && [ -d "/proc/$pid" ]; then
  printf 'count='; find "/proc/$pid/fd" -mindepth 1 -maxdepth 1 2>/dev/null | wc -l
  awk '$1=="Max" && $2=="open" && $3=="files" {print "soft_limit=" $4; print "hard_limit=" $5}' "/proc/$pid/limits" 2>/dev/null
fi
printf '__ZBOARD_DIAG_FD_END__\n'
printf '__ZBOARD_DIAG_CONNTRACK_BEGIN__\n'
if [ -r /proc/sys/net/netfilter/nf_conntrack_count ]; then printf 'count='; cat /proc/sys/net/netfilter/nf_conntrack_count; fi
if [ -r /proc/sys/net/netfilter/nf_conntrack_max ]; then printf 'max='; cat /proc/sys/net/netfilter/nf_conntrack_max; fi
printf '__ZBOARD_DIAG_CONNTRACK_END__\n'
printf '__ZBOARD_DIAG_JOURNAL_BEGIN__\n'
timeout 2s journalctl -u zero --since '-30 min' --no-pager -p warning -n 160 --output=short-iso 2>/dev/null | tail -c 65536
printf '\n__ZBOARD_DIAG_JOURNAL_END__\n'
printf '__ZBOARD_DIAG_KERNEL_BEGIN__\n'
timeout 2s dmesg --level=err,warn -T 2>/dev/null | tail -n 80 | tail -c 32768
printf '\n__ZBOARD_DIAG_KERNEL_END__\n'`

type nodeDiagnosticStatus struct {
	Config struct {
		ConfigRevision uint64                    `json:"config_revision"`
		Listeners      []nodeExpectedListenerRaw `json:"listeners"`
	} `json:"config"`
	Runtime struct {
		CoreInstanceID  string `json:"core_instance_id"`
		ConfigRevision  uint64 `json:"config_revision"`
		PID             uint32 `json:"pid"`
		ConfigPath      string `json:"config_path"`
		StartedAtUnixMS uint64 `json:"started_at_unix_ms"`
	} `json:"runtime"`
}

type nodeExpectedListenerRaw struct {
	Tag           string `json:"tag"`
	Protocol      string `json:"protocol"`
	ListenAddress string `json:"listen_address"`
	ListenPort    uint16 `json:"listen_port"`
	UDPEnabled    bool   `json:"udp_enabled"`
}

type nodeDiagnosticExpectedListener struct {
	Tag                  string   `json:"tag"`
	Protocol             string   `json:"protocol"`
	Address              string   `json:"address"`
	Port                 uint16   `json:"port"`
	Networks             []string `json:"networks"`
	Present              bool     `json:"present"`
	MissingNetworks      []string `json:"missing_networks,omitempty"`
	ExternalReachability string   `json:"external_reachability"`
}

type nodeDiagnosticActualListener struct {
	Network string `json:"network"`
	State   string `json:"state"`
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}

type nodeDiagnosticRuntime struct {
	ControlStatus   string `json:"control_status"`
	CoreInstanceID  string `json:"core_instance_id,omitempty"`
	ConfigRevision  uint64 `json:"config_revision,omitempty"`
	PID             uint32 `json:"pid,omitempty"`
	ConfigPath      string `json:"config_path,omitempty"`
	EngineVersion   string `json:"engine_version,omitempty"`
	StartedAtUnixMS uint64 `json:"started_at_unix_ms,omitempty"`
	ListenerSource  string `json:"listener_health_source"`
}

type nodeDiagnosticService struct {
	ActiveState    string `json:"active_state,omitempty"`
	SubState       string `json:"sub_state,omitempty"`
	MainPID        uint64 `json:"main_pid,omitempty"`
	ExecMainStatus int64  `json:"exec_main_status,omitempty"`
}

type nodeDiagnosticResources struct {
	FDCount             uint64  `json:"fd_count,omitempty"`
	FDSoftLimit         uint64  `json:"fd_soft_limit,omitempty"`
	FDRatio             float64 `json:"fd_ratio,omitempty"`
	ConntrackCount      uint64  `json:"conntrack_count,omitempty"`
	ConntrackMax        uint64  `json:"conntrack_max,omitempty"`
	ConntrackRatio      float64 `json:"conntrack_ratio,omitempty"`
	ResourcePressure    bool    `json:"resource_pressure"`
	ResourceInformation bool    `json:"resource_information_available"`
}

type nodeDiagnosticCapabilities struct {
	SSH                     bool `json:"ssh"`
	NativeRuntimeSnapshot   bool `json:"native_runtime_snapshot"`
	NativeListenerHealth    bool `json:"native_listener_health"`
	TCPExternalReachability bool `json:"tcp_external_reachability"`
	UDPExternalReachability bool `json:"udp_external_reachability"`
}

type nodeDiagnosticSnapshot struct {
	NodeID            uint                             `json:"node_id"`
	CapturedAt        time.Time                        `json:"captured_at"`
	LatencyMS         int64                            `json:"latency_ms"`
	Classification    string                           `json:"classification"`
	Summary           string                           `json:"summary"`
	Runtime           nodeDiagnosticRuntime            `json:"runtime"`
	Service           nodeDiagnosticService            `json:"service"`
	Resources         nodeDiagnosticResources          `json:"resources"`
	ExpectedListeners []nodeDiagnosticExpectedListener `json:"expected_listeners"`
	ActualListeners   []nodeDiagnosticActualListener   `json:"actual_listeners"`
	RecentZeroLogs    string                           `json:"recent_zero_logs,omitempty"`
	RecentKernelLogs  string                           `json:"recent_kernel_logs,omitempty"`
	Capabilities      nodeDiagnosticCapabilities       `json:"capabilities"`
	Warnings          []string                         `json:"warnings,omitempty"`
}

func parseNodeDiagnosticSnapshot(_ model.Node, output string) nodeDiagnosticSnapshot {
	snapshot := nodeDiagnosticSnapshot{
		Classification: "unknown",
		Summary:        "诊断证据不足。",
		Capabilities: nodeDiagnosticCapabilities{
			SSH:                     true,
			NativeListenerHealth:    false,
			UDPExternalReachability: false,
		},
	}

	statusText := extractNodeDiagnosticSection(output, "STATUS")
	var status nodeDiagnosticStatus
	if strings.TrimSpace(statusText) != "" && json.Unmarshal([]byte(statusText), &status) == nil {
		snapshot.Capabilities.NativeRuntimeSnapshot = true
		snapshot.Runtime.ControlStatus = "healthy"
		snapshot.Runtime.CoreInstanceID = strings.TrimSpace(status.Runtime.CoreInstanceID)
		snapshot.Runtime.ConfigRevision = status.Runtime.ConfigRevision
		if snapshot.Runtime.ConfigRevision == 0 {
			snapshot.Runtime.ConfigRevision = status.Config.ConfigRevision
		}
		snapshot.Runtime.PID = status.Runtime.PID
		snapshot.Runtime.ConfigPath = strings.TrimSpace(status.Runtime.ConfigPath)
		snapshot.Runtime.StartedAtUnixMS = status.Runtime.StartedAtUnixMS
		snapshot.Runtime.ListenerSource = "ssh:ss"
		for _, raw := range status.Config.Listeners {
			expected := nodeDiagnosticExpectedListener{
				Tag:                  strings.TrimSpace(raw.Tag),
				Protocol:             strings.TrimSpace(raw.Protocol),
				Address:              strings.TrimSpace(raw.ListenAddress),
				Port:                 raw.ListenPort,
				Networks:             expectedListenerNetworks(raw),
				ExternalReachability: "not_checked",
			}
			snapshot.ExpectedListeners = append(snapshot.ExpectedListeners, expected)
		}
	} else {
		snapshot.Runtime.ControlStatus = "unavailable"
		snapshot.Runtime.ListenerSource = "unavailable"
		snapshot.Warnings = append(snapshot.Warnings, "Zero status 快照不可用；不会把 systemd 存活误判为数据面健康。")
	}

	snapshot.Runtime.EngineVersion = strings.TrimSpace(extractNodeDiagnosticSection(output, "VERSION"))
	snapshot.ActualListeners = parseNodeSSListeners(extractNodeDiagnosticSection(output, "SS"))
	snapshot.Service = parseNodeDiagnosticService(extractNodeDiagnosticSection(output, "SERVICE"))
	snapshot.Resources = parseNodeDiagnosticResources(extractNodeDiagnosticSection(output, "FD"), extractNodeDiagnosticSection(output, "CONNTRACK"))
	snapshot.RecentZeroLogs = redactNodeDiagnosticText(extractNodeDiagnosticSection(output, "JOURNAL"), nodeDiagnosticJournalMaxBytes)
	snapshot.RecentKernelLogs = redactNodeDiagnosticText(extractNodeDiagnosticSection(output, "KERNEL"), nodeDiagnosticKernelMaxBytes)
	matchNodeExpectedListeners(snapshot.ExpectedListeners, snapshot.ActualListeners)
	appendNodeDiagnosticBindingWarnings(&snapshot)
	return snapshot
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

func expectedListenerNetworks(listener nodeExpectedListenerRaw) []string {
	protocol := strings.ToLower(strings.TrimSpace(listener.Protocol))
	if strings.Contains(protocol, "hysteria") || strings.Contains(protocol, "quic") {
		return []string{"udp"}
	}
	networks := []string{"tcp"}
	if listener.UDPEnabled {
		networks = append(networks, "udp")
	}
	return networks
}

func parseNodeSSListeners(output string) []nodeDiagnosticActualListener {
	listeners := make([]nodeDiagnosticActualListener, 0)
	seen := map[string]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 6 {
			continue
		}
		network := strings.ToLower(fields[0])
		if network != "tcp" && network != "udp" {
			continue
		}
		address, port, ok := parseSSLocalEndpoint(fields[len(fields)-2])
		if !ok {
			continue
		}
		key := network + "|" + address + "|" + strconv.Itoa(int(port))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		listeners = append(listeners, nodeDiagnosticActualListener{
			Network: network,
			State:   strings.ToLower(fields[1]),
			Address: address,
			Port:    port,
		})
	}
	sort.Slice(listeners, func(i, j int) bool {
		if listeners[i].Port != listeners[j].Port {
			return listeners[i].Port < listeners[j].Port
		}
		if listeners[i].Network != listeners[j].Network {
			return listeners[i].Network < listeners[j].Network
		}
		return listeners[i].Address < listeners[j].Address
	})
	return listeners
}

func parseSSLocalEndpoint(value string) (string, uint16, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || port == 0 {
			return "", 0, false
		}
		return normalizeListenerAddress(host), uint16(port), true
	}
	index := strings.LastIndex(value, ":")
	if index < 0 || index == len(value)-1 {
		return "", 0, false
	}
	port, err := strconv.ParseUint(value[index+1:], 10, 16)
	if err != nil || port == 0 {
		return "", 0, false
	}
	return normalizeListenerAddress(value[:index]), uint16(port), true
}

func normalizeListenerAddress(address string) string {
	address = strings.TrimSpace(strings.Trim(address, "[]"))
	if zone := strings.LastIndex(address, "%"); zone > 0 {
		address = address[:zone]
	}
	return address
}

func matchNodeExpectedListeners(expected []nodeDiagnosticExpectedListener, actual []nodeDiagnosticActualListener) {
	for index := range expected {
		missing := make([]string, 0)
		for _, network := range expected[index].Networks {
			found := false
			for _, listener := range actual {
				if listener.Network == network && listener.Port == expected[index].Port && listenerAddressSatisfiesConfiguredBind(expected[index].Address, listener.Address) {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, network)
			}
		}
		expected[index].MissingNetworks = missing
		expected[index].Present = len(missing) == 0
	}
}

func listenerAddressMatches(expected, actual string) bool {
	expected = normalizeListenerAddress(expected)
	actual = normalizeListenerAddress(actual)
	if expected == actual {
		return true
	}
	if actual == "*" {
		return true
	}
	expectedIP := net.ParseIP(expected)
	actualIP := net.ParseIP(actual)
	if expectedIP == nil {
		return false
	}
	if expectedIP.IsUnspecified() {
		return actualIP != nil && actualIP.IsUnspecified() && ((expectedIP.To4() == nil) == (actualIP.To4() == nil))
	}
	if actualIP != nil && actualIP.IsUnspecified() {
		return (expectedIP.To4() == nil) == (actualIP.To4() == nil)
	}
	return actualIP != nil && expectedIP.Equal(actualIP)
}

func parseNodeDiagnosticService(output string) nodeDiagnosticService {
	var service nodeDiagnosticService
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			service.ActiveState = strings.TrimSpace(value)
		case "SubState":
			service.SubState = strings.TrimSpace(value)
		case "MainPID":
			service.MainPID, _ = strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		case "ExecMainStatus":
			service.ExecMainStatus, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		}
	}
	return service
}

func parseNodeDiagnosticResources(fdOutput, conntrackOutput string) nodeDiagnosticResources {
	var resources nodeDiagnosticResources
	fdValues := parseNodeDiagnosticKeyValues(fdOutput)
	conntrackValues := parseNodeDiagnosticKeyValues(conntrackOutput)
	resources.FDCount = parseNodeDiagnosticUint(fdValues["count"])
	resources.FDSoftLimit = parseNodeDiagnosticUint(fdValues["soft_limit"])
	if resources.FDSoftLimit > 0 {
		resources.FDRatio = float64(resources.FDCount) / float64(resources.FDSoftLimit)
		resources.ResourceInformation = true
	}
	resources.ConntrackCount = parseNodeDiagnosticUint(conntrackValues["count"])
	resources.ConntrackMax = parseNodeDiagnosticUint(conntrackValues["max"])
	if resources.ConntrackMax > 0 {
		resources.ConntrackRatio = float64(resources.ConntrackCount) / float64(resources.ConntrackMax)
		resources.ResourceInformation = true
	}
	resources.ResourcePressure = resources.FDRatio >= nodeDiagnosticPressureRatio || resources.ConntrackRatio >= nodeDiagnosticPressureRatio
	return resources
}

func parseNodeDiagnosticKeyValues(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func parseNodeDiagnosticUint(value string) uint64 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return parsed
}

func redactNodeDiagnosticText(value string, maxBytes int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	value = nodeDiagnosticSecretPattern.ReplaceAllString(value, "$1[REDACTED]")
	if len(value) > maxBytes {
		value = value[len(value)-maxBytes:]
		value = "[truncated]\n" + value
	}
	return value
}

func nodeDiagnosticProbeHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err == nil {
			return parsed.Hostname()
		}
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

func containsNodeDiagnosticNetwork(networks []string, target string) bool {
	for _, network := range networks {
		if network == target {
			return true
		}
	}
	return false
}

func classifyNodeDiagnostic(snapshot *nodeDiagnosticSnapshot) {
	missing := 0
	unreachable := 0
	for _, listener := range snapshot.ExpectedListeners {
		if !listener.Present {
			missing++
		}
		if listener.ExternalReachability == "unreachable" {
			unreachable++
		}
	}
	serviceDegraded := snapshot.Service.ActiveState != "" && snapshot.Service.ActiveState != "active"
	switch {
	case !snapshot.Capabilities.NativeRuntimeSnapshot || len(snapshot.ExpectedListeners) == 0:
		snapshot.Classification = "unknown"
		snapshot.Summary = "无法取得 Zero 配置期望，未将进程或控制面存活视为数据面健康。"
	case missing > 0 || serviceDegraded:
		snapshot.Classification = "data_plane_missing"
		snapshot.Summary = fmt.Sprintf("发现 %d 个配置监听项缺失或绑定不符合配置，或 Zero 服务未处于 active。", missing)
	case snapshot.Resources.ResourcePressure:
		snapshot.Classification = "resource_pressure"
		snapshot.Summary = "本地监听完整，但文件描述符或 conntrack 使用率已达到 90% 压力阈值。"
	case unreachable > 0:
		snapshot.Classification = "network_reachability"
		snapshot.Summary = fmt.Sprintf("本地监听完整，但 %d 个 TCP 公开入口从 Zboard 所在网络无法连接。", unreachable)
	default:
		snapshot.Classification = "healthy"
		snapshot.Summary = "Zero 控制快照可用，配置监听均存在，未发现高资源压力。"
	}
}
