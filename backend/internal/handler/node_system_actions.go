package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const nodeSystemActionEnableBBR = "enable_bbr"

type nodeSystemActionRequest struct {
	Action string `json:"action"`
}

type nodeBBRState struct {
	Available                   bool      `json:"available"`
	Active                      bool      `json:"active"`
	Persistent                  bool      `json:"persistent"`
	CongestionControl           string    `json:"congestion_control"`
	AvailableCongestionControls []string  `json:"available_congestion_controls"`
	DefaultQdisc                string    `json:"default_qdisc"`
	KernelRelease               string    `json:"kernel_release"`
	SampledAt                   time.Time `json:"sampled_at"`
	LatencyMS                   int64     `json:"latency_ms"`
}

type nodeSystemActionsSnapshot struct {
	NodeID           uint         `json:"node_id"`
	SupportedActions []string     `json:"supported_actions"`
	BBR              nodeBBRState `json:"bbr"`
}

const nodeBBRProbeCommand = `set -u
if ! command -v sysctl >/dev/null 2>&1; then
  printf 'ZBOARD_BBR_ERROR=sysctl_not_found\n'
  exit 0
fi
available="$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || true)"
current="$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || true)"
qdisc="$(sysctl -n net.core.default_qdisc 2>/dev/null || true)"
persistent=0
if [ -f /etc/sysctl.d/99-zboard-bbr.conf ] && \
   grep -Eq '^[[:space:]]*net\.core\.default_qdisc[[:space:]]*=[[:space:]]*fq[[:space:]]*$' /etc/sysctl.d/99-zboard-bbr.conf && \
   grep -Eq '^[[:space:]]*net\.ipv4\.tcp_congestion_control[[:space:]]*=[[:space:]]*bbr[[:space:]]*$' /etc/sysctl.d/99-zboard-bbr.conf; then
  persistent=1
fi
printf 'ZBOARD_BBR_AVAILABLE=%s\n' "$available"
printf 'ZBOARD_BBR_CURRENT=%s\n' "$current"
printf 'ZBOARD_BBR_QDISC=%s\n' "$qdisc"
printf 'ZBOARD_BBR_PERSISTENT=%s\n' "$persistent"
printf 'ZBOARD_BBR_KERNEL=%s\n' "$(uname -r 2>/dev/null || printf unknown)"`

const nodeBBREnableCommand = `set -eu
test "$(id -u)" = "0"
test "$(uname -s)" = "Linux"
command -v sysctl >/dev/null 2>&1
old_cc="$(sysctl -n net.ipv4.tcp_congestion_control)"
old_qdisc="$(sysctl -n net.core.default_qdisc)"
target=/etc/sysctl.d/99-zboard-bbr.conf
backup=""
had_file=0
if [ -f "$target" ]; then
  backup="$(mktemp /tmp/zboard-bbr-backup.XXXXXX)"
  cp -a "$target" "$backup"
  had_file=1
fi
tmp=""
rollback() {
  if [ "$had_file" = "1" ] && [ -n "$backup" ] && [ -f "$backup" ]; then
    cp -a "$backup" "$target" || true
  else
    rm -f "$target" || true
  fi
  sysctl -w "net.ipv4.tcp_congestion_control=$old_cc" >/dev/null 2>&1 || true
  sysctl -w "net.core.default_qdisc=$old_qdisc" >/dev/null 2>&1 || true
  if [ -n "$tmp" ]; then rm -f "$tmp" || true; fi
  if [ -n "$backup" ]; then rm -f "$backup" || true; fi
}
trap 'rc=$?; if [ "$rc" != "0" ]; then rollback; fi; exit "$rc"' EXIT
available="$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || true)"
if ! printf '%s\n' "$available" | tr ' ' '\n' | grep -qx bbr; then
  if command -v modprobe >/dev/null 2>&1; then modprobe tcp_bbr >/dev/null 2>&1 || true; fi
  available="$(sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || true)"
fi
if ! printf '%s\n' "$available" | tr ' ' '\n' | grep -qx bbr; then
  printf 'ZBOARD_BBR_UNAVAILABLE=1\n' >&2
  exit 42
fi
install -d -m 0755 /etc/sysctl.d
tmp="$(mktemp /etc/sysctl.d/.99-zboard-bbr.conf.XXXXXX)"
printf '%s\n' \
  '# Managed by zboard. Manual changes may be replaced by the BBR system action.' \
  'net.core.default_qdisc = fq' \
  'net.ipv4.tcp_congestion_control = bbr' > "$tmp"
chmod 0644 "$tmp"
mv -f "$tmp" "$target"
tmp=""
sysctl -w net.core.default_qdisc=fq >/dev/null
sysctl -w net.ipv4.tcp_congestion_control=bbr >/dev/null
test "$(sysctl -n net.core.default_qdisc)" = "fq"
test "$(sysctl -n net.ipv4.tcp_congestion_control)" = "bbr"
trap - EXIT
if [ -n "$backup" ]; then rm -f "$backup"; fi
printf 'ZBOARD_BBR_APPLIED=1\n'`

func (h *handlers) NodeSystemActionsHandler(w http.ResponseWriter, r *http.Request) {
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
		BadRequest(w, "节点尚未完成 SSH 验证，无法执行 VPS 自动化："+err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		state, err := h.probeNodeBBR(node)
		if err != nil {
			BadRequest(w, "读取 BBR 状态失败："+err.Error())
			return
		}
		OK(w, nodeSystemActionsSnapshot{NodeID: node.ID, SupportedActions: []string{nodeSystemActionEnableBBR}, BBR: state})
	case http.MethodPost:
		var req nodeSystemActionRequest
		if err := decodeBody(r, &req); err != nil {
			BadRequest(w, err.Error())
			return
		}
		req.Action = strings.ToLower(strings.TrimSpace(req.Action))
		if req.Action != nodeSystemActionEnableBBR {
			BadRequest(w, "unsupported node system action")
			return
		}
		state, err := h.enableNodeBBR(node, claims)
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		OK(w, nodeSystemActionsSnapshot{NodeID: node.ID, SupportedActions: []string{nodeSystemActionEnableBBR}, BBR: state})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (h *handlers) probeNodeBBR(node model.Node) (nodeBBRState, error) {
	output, latency, err := h.execSSHCommandWithPrivilege(node, nodeBBRProbeCommand, true)
	if err != nil {
		return nodeBBRState{}, fmt.Errorf("probe over SSH: %w: %s", err, truncateKernelError(output))
	}
	state, err := parseNodeBBRState(output)
	if err != nil {
		return nodeBBRState{}, err
	}
	state.SampledAt = time.Now().UTC()
	state.LatencyMS = latency.Milliseconds()
	return state, nil
}

func (h *handlers) enableNodeBBR(node model.Node, claims authClaims) (nodeBBRState, error) {
	output, _, err := h.execSSHCommandWithPrivilege(node, nodeBBREnableCommand, true)
	if err != nil {
		detail := "result=failed"
		if strings.Contains(output, "ZBOARD_BBR_UNAVAILABLE=1") {
			detail += " reason=kernel_unavailable"
		}
		_ = createAuditLog(h.db, claims, "node.system_action.bbr", fmt.Sprintf("node:%d", node.ID), detail)
		if strings.Contains(output, "ZBOARD_BBR_UNAVAILABLE=1") {
			return nodeBBRState{}, errors.New("当前 Linux 内核未提供 BBR 拥塞控制算法，未修改系统配置")
		}
		return nodeBBRState{}, fmt.Errorf("启用 BBR 失败：%w: %s", err, truncateKernelError(output))
	}
	state, err := h.probeNodeBBR(node)
	if err != nil {
		_ = createAuditLog(h.db, claims, "node.system_action.bbr", fmt.Sprintf("node:%d", node.ID), "result=verification_failed")
		return nodeBBRState{}, fmt.Errorf("BBR 已执行但状态复核失败：%w", err)
	}
	if !state.Active || !state.Persistent || state.DefaultQdisc != "fq" {
		_ = createAuditLog(h.db, claims, "node.system_action.bbr", fmt.Sprintf("node:%d", node.ID), "result=verification_failed")
		return nodeBBRState{}, errors.New("BBR 状态复核未达到期望值，系统配置未被报告为成功")
	}
	if err := createAuditLog(h.db, claims, "node.system_action.bbr", fmt.Sprintf("node:%d", node.ID), "result=succeeded congestion_control=bbr qdisc=fq"); err != nil {
		return nodeBBRState{}, err
	}
	return state, nil
}

func parseNodeBBRState(output string) (nodeBBRState, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ZBOARD_BBR_") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	if values["ZBOARD_BBR_ERROR"] == "sysctl_not_found" {
		return nodeBBRState{}, errors.New("目标系统缺少 sysctl，无法管理 BBR")
	}
	if _, ok := values["ZBOARD_BBR_AVAILABLE"]; !ok {
		return nodeBBRState{}, errors.New("BBR 状态探测返回不完整")
	}
	available := strings.Fields(values["ZBOARD_BBR_AVAILABLE"])
	state := nodeBBRState{
		AvailableCongestionControls: available,
		CongestionControl:           values["ZBOARD_BBR_CURRENT"],
		DefaultQdisc:                values["ZBOARD_BBR_QDISC"],
		KernelRelease:               values["ZBOARD_BBR_KERNEL"],
		Persistent:                  values["ZBOARD_BBR_PERSISTENT"] == "1",
	}
	for _, item := range available {
		if item == "bbr" {
			state.Available = true
			break
		}
	}
	state.Active = state.CongestionControl == "bbr"
	return state, nil
}
