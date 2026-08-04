package handler

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const (
	nodeAddressSourceNodeAddress  = "node_address"
	nodeAddressSourceNodeDNS      = "node_address_dns"
	nodeAddressSourceSSHHost      = "ssh_host"
	nodeAddressSourceSSHHostDNS   = "ssh_host_dns"
	nodeAddressSourceSSHGlobal    = "ssh_global"
	nodeAddressProbeNotConfigured = "not_configured"
	nodeAddressProbeNotVerified   = "not_verified"
	nodeAddressProbeSucceeded     = "succeeded"
	nodeAddressProbeFailed        = "failed"
	nodeAddressCandidatePolicy    = "public_only"
	nodeAddressLookupTimeout      = 3 * time.Second
	nodeAddressDiscoveryCommand   = "LC_ALL=C; export LC_ALL; command -v ip >/dev/null 2>&1 || exit 127; ip -o -4 address show scope global; ip -o -6 address show scope global"
)

var nodeAddressLookupIPAddrs = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type nodeAddressCandidate struct {
	Address string `json:"address"`
	Source  string `json:"source"`
}

type nodeAddressCandidatesResponse struct {
	NodeID          uint                   `json:"node_id"`
	Policy          string                 `json:"policy"`
	IPv4            []nodeAddressCandidate `json:"ipv4"`
	IPv6            []nodeAddressCandidate `json:"ipv6"`
	RecommendedIPv4 string                 `json:"recommended_ipv4,omitempty"`
	RecommendedIPv6 string                 `json:"recommended_ipv6,omitempty"`
	SSHProbeStatus  string                 `json:"ssh_probe_status"`
	Warnings        []string               `json:"warnings,omitempty"`
}

type nodeAddressCandidateCollector struct {
	seen map[string]struct{}
	ipv4 []nodeAddressCandidate
	ipv6 []nodeAddressCandidate
}

func newNodeAddressCandidateCollector() *nodeAddressCandidateCollector {
	return &nodeAddressCandidateCollector{seen: make(map[string]struct{})}
}

func (collector *nodeAddressCandidateCollector) add(rawAddress, source string) bool {
	address, err := netip.ParseAddr(strings.TrimSpace(rawAddress))
	if err != nil {
		return false
	}
	address = address.Unmap()
	if !isPublicNodeAddress(address) {
		return false
	}
	normalized := address.String()
	if _, exists := collector.seen[normalized]; exists {
		return false
	}
	collector.seen[normalized] = struct{}{}
	candidate := nodeAddressCandidate{Address: normalized, Source: source}
	if address.Is4() {
		collector.ipv4 = append(collector.ipv4, candidate)
	} else {
		collector.ipv6 = append(collector.ipv6, candidate)
	}
	return true
}

func (collector *nodeAddressCandidateCollector) response(nodeID uint, sshProbeStatus string, warnings []string) nodeAddressCandidatesResponse {
	result := nodeAddressCandidatesResponse{
		NodeID: nodeID, Policy: nodeAddressCandidatePolicy,
		IPv4: collector.ipv4, IPv6: collector.ipv6,
		SSHProbeStatus: sshProbeStatus, Warnings: warnings,
	}
	if len(result.IPv4) > 0 {
		result.RecommendedIPv4 = result.IPv4[0].Address
	}
	if len(result.IPv6) > 0 {
		result.RecommendedIPv6 = result.IPv6[0].Address
	}
	return result
}

func (h *handlers) NodeAddressCandidatesHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/admin/nodes/")
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

	collector := newNodeAddressCandidateCollector()
	warnings := make([]string, 0, 3)
	resolveNodeAddressField(r.Context(), node.Address, nodeAddressSourceNodeAddress, nodeAddressSourceNodeDNS, "节点地址", collector, &warnings)
	resolveNodeAddressField(r.Context(), node.SSHHost, nodeAddressSourceSSHHost, nodeAddressSourceSSHHostDNS, "SSH 主机", collector, &warnings)

	sshProbeStatus := nodeAddressProbeNotConfigured
	sshConfigured := strings.TrimSpace(node.SSHHost) != "" && strings.TrimSpace(node.SSHUser) != "" && strings.TrimSpace(node.SSHPwd) != ""
	if sshConfigured {
		sshProbeStatus = nodeAddressProbeNotVerified
		if node.SSHVerifiedAt == nil || strings.TrimSpace(node.SSHHostKeyFingerprint) == "" {
			warnings = append(warnings, "节点尚未完成 SSH 主机身份验证，已跳过主机网卡地址探测。")
		} else if err := h.validateNodeSSH(node); err != nil {
			sshProbeStatus = nodeAddressProbeFailed
			warnings = append(warnings, "SSH 配置当前不可用，已跳过主机网卡地址探测。")
		} else {
			output, _, probeErr := h.execSSHCommand(node, nodeAddressDiscoveryCommand)
			if probeErr != nil {
				sshProbeStatus = nodeAddressProbeFailed
				warnings = append(warnings, "读取主机网卡地址失败；已保留节点字段和 DNS 解析得到的候选。")
			} else {
				sshProbeStatus = nodeAddressProbeSucceeded
				for _, address := range parseNodeGlobalAddressOutput(output) {
					collector.add(address, nodeAddressSourceSSHGlobal)
				}
			}
		}
	}

	OK(w, collector.response(node.ID, sshProbeStatus, uniqueStrings(warnings)))
}

func resolveNodeAddressField(ctx context.Context, rawValue, literalSource, resolvedSource, label string, collector *nodeAddressCandidateCollector, warnings *[]string) {
	host := normalizeNodeAddressHost(rawValue)
	if host == "" {
		return
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if !collector.add(address.String(), literalSource) {
			*warnings = append(*warnings, label+"不是可公开路由的 IP 地址。")
		}
		return
	}
	if strings.ContainsAny(host, " /@") {
		*warnings = append(*warnings, label+"格式无法用于地址发现。")
		return
	}
	lookupContext, cancel := context.WithTimeout(ctx, nodeAddressLookupTimeout)
	defer cancel()
	addresses, err := nodeAddressLookupIPAddrs(lookupContext, host)
	if err != nil {
		*warnings = append(*warnings, label+"无法解析为 IP 地址。")
		return
	}
	sort.Slice(addresses, func(i, j int) bool {
		left, leftOK := netip.AddrFromSlice(addresses[i].IP)
		right, rightOK := netip.AddrFromSlice(addresses[j].IP)
		if !leftOK || !rightOK {
			return addresses[i].String() < addresses[j].String()
		}
		left, right = left.Unmap(), right.Unmap()
		if left.Is4() != right.Is4() {
			return left.Is4()
		}
		return left.Less(right)
	})
	added := false
	for _, address := range addresses {
		added = collector.add(address.IP.String(), resolvedSource) || added
	}
	if !added {
		*warnings = append(*warnings, label+"解析结果中没有可公开路由的 IP 地址。")
	}
}

func normalizeNodeAddressHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if address, err := netip.ParseAddr(strings.Trim(value, "[]")); err == nil {
		return address.String()
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.TrimSuffix(strings.Trim(value, "[]"), ".")
}

func parseNodeGlobalAddressOutput(output string) []string {
	addresses := make([]string, 0)
	for _, rawLine := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, " temporary") || strings.Contains(lower, " deprecated") || strings.Contains(lower, " tentative") || strings.Contains(lower, " dadfailed") {
			continue
		}
		fields := strings.Fields(line)
		for index, field := range fields {
			if (field != "inet" && field != "inet6") || index+1 >= len(fields) {
				continue
			}
			value := fields[index+1]
			if prefix, err := netip.ParsePrefix(value); err == nil {
				addresses = append(addresses, prefix.Addr().Unmap().String())
			} else if address, err := netip.ParseAddr(value); err == nil {
				addresses = append(addresses, address.Unmap().String())
			}
			break
		}
	}
	return addresses
}

func isPublicNodeAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range nonPublicNodeAddressPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var nonPublicNodeAddressPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
