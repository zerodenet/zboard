package network

import (
	"net/netip"
	"strings"
)

const DefaultProtocolListenAddress = "0.0.0.0"

// NormalizeProtocolListenAddress keeps the stored address independent from the
// public subscription address. An omitted/empty value retains legacy IPv4 binds.
func NormalizeProtocolListenAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultProtocolListenAddress, nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = raw[1 : len(raw)-1]
	}
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" || len(raw) > 64 {
		return "", &ProtocolEndpointMutationValidation{
			Message: "协议服务校验失败。",
			Fields:  map[string]string{"listen_address": "请输入 VPS 本地 IPv4 或 IPv6 地址；所有 IPv4 地址用 0.0.0.0，所有 IPv6 地址用 ::。"},
		}
	}
	return address.Unmap().String(), nil
}
