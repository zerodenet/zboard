package network

import (
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"strings"
)

type OutboundDial struct {
	AddressFamily string  `json:"address_family"`
	Interface     *string `json:"interface,omitempty"`
	SourceIP      *string `json:"source_ip,omitempty"`
}

func normalizeOutboundDial(raw []byte) (*OutboundDial, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, protocolEndpointEgressValidation("出站约束必须是 JSON 对象。")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, protocolEndpointEgressValidation("出站约束必须是 JSON 对象。")
	}
	if value, ok := fields["address_family"]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, protocolEndpointEgressValidation("出站地址族不能为空。")
	}
	dial := OutboundDial{AddressFamily: "auto"}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dial); err != nil {
		return nil, protocolEndpointEgressValidation("出站约束只能包含 address_family、interface 和 source_ip。")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, protocolEndpointEgressValidation("出站约束不是有效 JSON。")
	}
	if dial.AddressFamily != "auto" && dial.AddressFamily != "only_ipv4" && dial.AddressFamily != "only_ipv6" {
		return nil, protocolEndpointEgressValidation("请选择 auto、only_ipv4 或 only_ipv6。")
	}
	if dial.Interface != nil && (*dial.Interface == "" || *dial.Interface != strings.TrimSpace(*dial.Interface) || strings.ContainsAny(*dial.Interface, "\x00/")) {
		return nil, protocolEndpointEgressValidation("请输入不带首尾空格、斜杠或空字符的网卡名称。")
	}
	if dial.SourceIP != nil {
		address, err := netip.ParseAddr(*dial.SourceIP)
		if err != nil || address.Zone() != "" {
			return nil, protocolEndpointEgressValidation("源 IP 必须是具体的 IPv4 或 IPv6 地址，不含端口、CIDR 或 zone。")
		}
		address = address.Unmap()
		if address.IsUnspecified() || address.IsMulticast() || address == netip.MustParseAddr("255.255.255.255") {
			return nil, protocolEndpointEgressValidation("源 IP 不能是通配、组播或广播地址。")
		}
		if (dial.AddressFamily == "only_ipv4" && address.Is6()) || (dial.AddressFamily == "only_ipv6" && address.Is4()) {
			return nil, protocolEndpointEgressValidation("源 IP 与选择的出站地址族冲突。")
		}
		canonical := address.String()
		dial.SourceIP = &canonical
	}
	if dial.AddressFamily == "auto" && dial.Interface == nil && dial.SourceIP == nil {
		return nil, nil
	}
	return &dial, nil
}
