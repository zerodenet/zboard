package handler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var realityProbeDefaults = []string{"www.microsoft.com", "www.cloudflare.com", "www.apple.com", "www.wikipedia.org", "www.bing.com", "www.ubuntu.com"}
var realityProbeDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

type realityProbeRequest struct {
	NodeID  uint     `json:"node_id"`
	Domains []string `json:"domains"`
}

type realityProbeResult struct {
	ServerName string `json:"server_name"`
	Available  bool   `json:"available"`
	Status     string `json:"status"`
	LatencyMS  int64  `json:"latency_ms"`
}

type realityProbeSnapshot struct {
	NodeID    uint                 `json:"node_id"`
	SampledAt time.Time            `json:"sampled_at"`
	Items     []realityProbeResult `json:"items"`
}

func (h *handlers) ProtocolRealityProbeHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var req realityProbeRequest
	if err := decodeBody(r, &req); err != nil || req.NodeID == 0 {
		BadRequest(w, "请选择承载 VPS 后再探测伪装域名。")
		return
	}
	domains, err := normalizeRealityProbeDomains(req.Domains)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNodeContext(r.Context(), req.NodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
		} else {
			ServerError(w, err)
		}
		return
	}
	if node.SSHVerifiedAt == nil || strings.TrimSpace(node.SSHHostKeyFingerprint) == "" {
		BadRequest(w, "节点尚未完成 SSH 与主机身份验证，无法探测伪装域名。")
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, "节点 SSH 配置不可用："+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	output, _, runErr := h.execSSHCommandWithPrivilegeContext(ctx, node, buildRealityProbeCommand(domains), false)
	if strings.Contains(output, "ZBOARD_REALITY_ERROR=dependencies") {
		BadRequest(w, "节点缺少探测工具，请安装 OpenSSL（支持 TLS 1.3）和 GNU coreutils（timeout/date）后重试。")
		return
	}
	if runErr != nil {
		BadRequest(w, "从 VPS 探测失败："+runErr.Error())
		return
	}
	items, err := parseRealityProbeResults(output, domains)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	OK(w, realityProbeSnapshot{NodeID: node.ID, SampledAt: time.Now().UTC(), Items: items})
}

func normalizeRealityProbeDomains(values []string) ([]string, error) {
	if len(values) == 0 {
		values = realityProbeDefaults
	}
	if len(values) > 12 {
		return nil, errors.New("每次最多探测 12 个域名。")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		labels := strings.Split(value, ".")
		valid := len(value) <= 253 && len(labels) >= 2 && net.ParseIP(value) == nil
		for _, label := range labels {
			valid = valid && len(label) <= 63 && realityProbeDomain.MatchString(label)
		}
		if !valid {
			return nil, fmt.Errorf("无效域名 %q；请输入完整域名，不包含协议、端口或路径。", raw)
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}

// Each probe makes a real TLS handshake on the selected node. No software or
// configuration is installed. Inputs are validated domain names, never shell code.
func buildRealityProbeCommand(domains []string) string {
	quoted := make([]string, len(domains))
	for i, domain := range domains {
		quoted[i] = shellQuote(domain)
	}
	return `export LC_ALL=C
for tool in openssl timeout date; do
  command -v "$tool" >/dev/null 2>&1 || { printf 'ZBOARD_REALITY_ERROR=dependencies\n'; exit 1; }
done
case "$(date +%s%N)" in
  ''|*[!0-9]*) printf 'ZBOARD_REALITY_ERROR=dependencies\n'; exit 1;;
esac
probe() {
  domain="$1"
  start=$(date +%s%N)
  output=$(timeout -k 1s 5s openssl s_client -connect "$domain:443" -servername "$domain" -tls1_3 -groups X25519 -alpn h2 -verify_hostname "$domain" -verify_return_error </dev/null 2>&1)
  code=$?
  end=$(date +%s%N)
  elapsed=$(((end-start)/1000000))
  status=unreachable
  if printf '%s\n' "$output" | grep -q 'ALPN protocol: h2' &&
     printf '%s\n' "$output" | grep -q 'TLSv1.3' &&
     printf '%s\n' "$output" | grep -Eq 'Verify return code: 0 \(ok\)|Verification: OK'; then
    status=available
  elif [ "$code" -eq 124 ] || [ "$code" -eq 137 ]; then
    status=timeout
  elif printf '%s\n' "$output" | grep -Eqi 'certificate verify failed|verify error|hostname mismatch'; then
    status=certificate
  elif printf '%s\n' "$output" | grep -Eqi 'CONNECTED|unknown option|no protocols available|alert protocol version|no suitable key share'; then
    status=unsupported
  fi
  printf 'ZBOARD_REALITY|%s|%s|%s\n' "$domain" "$status" "$elapsed"
}
for domain in ` + strings.Join(quoted, " ") + `; do
  probe "$domain" &
done
wait`
}

func parseRealityProbeResults(output string, domains []string) ([]realityProbeResult, error) {
	results := make(map[string]realityProbeResult, len(domains))
	expected := make(map[string]bool, len(domains))
	for _, domain := range domains {
		expected[domain] = true
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "ZBOARD_REALITY|") {
			continue
		}
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 4 || !expected[parts[1]] {
			return nil, errors.New("节点返回了无效的域名探测结果。")
		}
		if _, exists := results[parts[1]]; exists {
			return nil, errors.New("节点返回了重复的域名探测结果。")
		}
		switch parts[2] {
		case "available", "timeout", "certificate", "unsupported", "unreachable":
		default:
			return nil, errors.New("节点返回了未知的域名探测状态。")
		}
		latency, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || latency < 0 {
			return nil, errors.New("节点返回了无效的域名探测耗时。")
		}
		results[parts[1]] = realityProbeResult{ServerName: parts[1], Available: parts[2] == "available", Status: parts[2], LatencyMS: latency}
	}
	items := make([]realityProbeResult, 0, len(domains))
	for _, domain := range domains {
		result, ok := results[domain]
		if !ok {
			return nil, errors.New("节点未返回完整探测结果，请检查 OpenSSL 与 timeout 是否可用。")
		}
		items = append(items, result)
	}
	return items, nil
}
