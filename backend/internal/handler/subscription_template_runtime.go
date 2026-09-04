package handler

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

const (
	subscriptionModeRule   = "rule"
	subscriptionModeGlobal = "global"
	subscriptionModeDirect = "direct"
)

var subscriptionRuntimeTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type subscriptionTunCustomization struct {
	Enabled     bool     `json:"enabled"`
	Addresses   []string `json:"addresses"`
	MTU         int      `json:"mtu"`
	AutoRoute   bool     `json:"auto_route"`
	StrictRoute bool     `json:"strict_route"`
	DNSHijack   bool     `json:"dns_hijack"`
}

type subscriptionDNSCustomization struct {
	Enabled       bool                    `json:"enabled"`
	Servers       []subscriptionDNSServer `json:"servers"`
	DefaultServer string                  `json:"default_server"`
	Strategy      string                  `json:"strategy"`
	CacheEnabled  bool                    `json:"cache_enabled"`
	CacheCapacity int                     `json:"cache_capacity"`
	FakeIPEnabled bool                    `json:"fake_ip_enabled"`
	FakeIPv4Range string                  `json:"fake_ipv4_range"`
	FakeIPv6Range string                  `json:"fake_ipv6_range,omitempty"`
}

type subscriptionDNSServer struct {
	Tag        string `json:"tag"`
	Type       string `json:"type"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Path       string `json:"path,omitempty"`
	ServerName string `json:"server_name,omitempty"`
}

func defaultSubscriptionTunCustomization() subscriptionTunCustomization {
	return subscriptionTunCustomization{
		Addresses: []string{"10.66.0.1/24", "fd66::1/64"},
		MTU:       1500, AutoRoute: true, StrictRoute: true, DNSHijack: true,
	}
}

func defaultSubscriptionDNSCustomization() subscriptionDNSCustomization {
	return subscriptionDNSCustomization{
		Servers:       []subscriptionDNSServer{{Tag: "default", Type: "system"}},
		DefaultServer: "default", Strategy: "prefer_ipv4",
		CacheEnabled: true, CacheCapacity: 1024,
		FakeIPv4Range: "198.18.0.0/15", FakeIPv6Range: "fc00::/18",
	}
}

func subscriptionRendererSupportsRuntimeNetwork(renderer string) bool {
	switch renderer {
	case subscriptionRendererZnetSink, subscriptionRendererClash, subscriptionRendererSingBox:
		return true
	default:
		return false
	}
}

func normalizeSubscriptionRuntime(renderer string, customization *subscriptionTemplateCustomization) error {
	customization.Mode = strings.ToLower(strings.TrimSpace(customization.Mode))
	if customization.Mode == "" {
		customization.Mode = subscriptionModeRule
	}
	switch customization.Mode {
	case subscriptionModeRule, subscriptionModeGlobal, subscriptionModeDirect:
	default:
		return errors.New("运行模式只能是 rule、global 或 direct")
	}
	if customization.SystemProxy && renderer != subscriptionRendererSingBox {
		return errors.New("只有 sing-box 模板支持自动设置系统 HTTP 代理")
	}
	if customization.SystemProxy && !customization.MixedEnabled {
		return errors.New("自动设置系统 HTTP 代理需要先启用本地混合代理")
	}
	if err := normalizeSubscriptionTun(renderer, &customization.Tun); err != nil {
		return err
	}
	if err := normalizeSubscriptionDNS(renderer, &customization.DNS); err != nil {
		return err
	}
	if customization.Tun.Enabled && customization.Tun.DNSHijack && !customization.DNS.Enabled {
		return errors.New("TUN DNS 劫持需要先启用 DNS")
	}
	if !customization.MixedEnabled && !customization.Tun.Enabled {
		return errors.New("本地混合代理与 TUN 不能同时关闭")
	}
	return nil
}

func normalizeSubscriptionTun(renderer string, tun *subscriptionTunCustomization) error {
	if tun.MTU == 0 {
		tun.MTU = 1500
	}
	if len(tun.Addresses) == 0 {
		tun.Addresses = defaultSubscriptionTunCustomization().Addresses
	}
	addresses := make([]string, 0, len(tun.Addresses))
	for _, raw := range tun.Addresses {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			addresses = append(addresses, trimmed)
		}
	}
	tun.Addresses = addresses
	if !tun.Enabled {
		return nil
	}
	if !subscriptionRendererSupportsRuntimeNetwork(renderer) {
		return errors.New("当前输出格式不支持可视化 TUN 配置")
	}
	if len(tun.Addresses) < 1 || len(tun.Addresses) > 2 {
		return errors.New("TUN 必须配置一个或两个不同地址族的 CIDR 地址")
	}
	seenFamilies := map[bool]struct{}{}
	for _, raw := range tun.Addresses {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return fmt.Errorf("TUN 地址 %q 不是有效 CIDR", raw)
		}
		family := prefix.Addr().Is6()
		if _, exists := seenFamilies[family]; exists {
			return errors.New("TUN 地址不能重复使用同一地址族")
		}
		seenFamilies[family] = struct{}{}
	}
	if tun.MTU < 576 || tun.MTU > 9000 {
		return errors.New("TUN MTU 必须在 576 到 9000 之间")
	}
	return nil
}

func normalizeSubscriptionDNS(renderer string, dns *subscriptionDNSCustomization) error {
	dns.DefaultServer = strings.TrimSpace(dns.DefaultServer)
	dns.Strategy = strings.ToLower(strings.TrimSpace(dns.Strategy))
	if dns.Strategy == "" {
		dns.Strategy = "prefer_ipv4"
	}
	if dns.CacheCapacity == 0 {
		dns.CacheCapacity = 1024
	}
	if dns.FakeIPv4Range == "" {
		dns.FakeIPv4Range = "198.18.0.0/15"
	}
	if !dns.Enabled {
		return nil
	}
	if !subscriptionRendererSupportsRuntimeNetwork(renderer) {
		return errors.New("当前输出格式不支持可视化 DNS 配置")
	}
	switch dns.Strategy {
	case "prefer_ipv4", "prefer_ipv6", "ipv4_only", "ipv6_only":
	default:
		return errors.New("DNS 地址策略不受支持")
	}
	if len(dns.Servers) < 1 || len(dns.Servers) > 8 {
		return errors.New("DNS 服务器数量必须在 1 到 8 个之间")
	}
	seen := make(map[string]struct{}, len(dns.Servers))
	for index := range dns.Servers {
		server := &dns.Servers[index]
		server.Tag = strings.TrimSpace(server.Tag)
		server.Type = strings.ToLower(strings.TrimSpace(server.Type))
		server.Host = strings.Trim(strings.TrimSpace(server.Host), "[]")
		server.Path = strings.TrimSpace(server.Path)
		server.ServerName = strings.TrimSpace(server.ServerName)
		if !subscriptionRuntimeTagPattern.MatchString(server.Tag) {
			return fmt.Errorf("第 %d 个 DNS 服务器标识格式无效", index+1)
		}
		normalizedTag := strings.ToLower(server.Tag)
		if _, exists := seen[normalizedTag]; exists {
			return fmt.Errorf("DNS 服务器标识 %q 重复", server.Tag)
		}
		seen[normalizedTag] = struct{}{}
		if err := normalizeSubscriptionDNSServer(renderer, server); err != nil {
			return fmt.Errorf("DNS 服务器 %q: %w", server.Tag, err)
		}
	}
	if _, exists := seen[strings.ToLower(dns.DefaultServer)]; !exists {
		return errors.New("默认 DNS 服务器不存在")
	}
	if dns.CacheEnabled && (dns.CacheCapacity < 1 || dns.CacheCapacity > 65536) {
		return errors.New("DNS 缓存容量必须在 1 到 65536 之间")
	}
	if dns.FakeIPEnabled {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(dns.FakeIPv4Range)); err != nil || !prefix.Addr().Is4() {
			return errors.New("Fake-IP IPv4 地址池必须是有效的 IPv4 CIDR")
		}
		if dns.FakeIPv6Range != "" {
			if prefix, err := netip.ParsePrefix(strings.TrimSpace(dns.FakeIPv6Range)); err != nil || !prefix.Addr().Is6() {
				return errors.New("Fake-IP IPv6 地址池必须是有效的 IPv6 CIDR")
			}
		}
	}
	return nil
}

func normalizeSubscriptionDNSServer(renderer string, server *subscriptionDNSServer) error {
	if server.Type == "system" {
		server.Host, server.Path, server.ServerName, server.Port = "", "", "", 0
		return nil
	}
	defaultPort := 53
	switch server.Type {
	case "udp", "tcp":
		if renderer == subscriptionRendererZnetSink && server.Type == "tcp" {
			return errors.New("Zero 当前不支持 TCP DNS；请选择 UDP、DoH、DoT 或 DoQ")
		}
	case "doh":
		defaultPort = 443
		if server.Path == "" {
			server.Path = "/dns-query"
		}
	case "dot", "doq":
		defaultPort = 853
	default:
		return errors.New("类型只能是 system、udp、tcp、doh、dot 或 doq")
	}
	if _, err := netip.ParseAddr(server.Host); err != nil {
		return errors.New("可视化 DNS 服务器地址必须使用 IP；域名和自定义 bootstrap 可在 Raw/高级配置中设置")
	}
	if server.Port == 0 {
		server.Port = defaultPort
	}
	if server.Port < 1 || server.Port > 65535 {
		return errors.New("端口必须在 1 到 65535 之间")
	}
	if server.Type == "doh" && !strings.HasPrefix(server.Path, "/") {
		return errors.New("DoH 路径必须以 / 开头")
	}
	return nil
}

func zeroSubscriptionMode(customization subscriptionTemplateCustomization, groupNames map[string]string) map[string]interface{} {
	switch customization.Mode {
	case subscriptionModeGlobal:
		return map[string]interface{}{"type": "global", "outbound": groupNames[customization.MainGroup]}
	case subscriptionModeDirect:
		return map[string]interface{}{"type": "direct"}
	default:
		return map[string]interface{}{"type": "rule"}
	}
}

func subscriptionModeFinalTarget(customization subscriptionTemplateCustomization, groupNames map[string]string) string {
	switch customization.Mode {
	case subscriptionModeGlobal:
		return groupNames[customization.MainGroup]
	case subscriptionModeDirect:
		return "direct"
	default:
		return singBoxSubscriptionActionTarget(customization.Final, groupNames)
	}
}

func zeroSubscriptionRuntime(customization subscriptionTemplateCustomization) map[string]interface{} {
	runtime := map[string]interface{}{}
	if customization.DNS.Enabled {
		runtime["dns"] = zeroSubscriptionDNS(customization.DNS)
	}
	if customization.Tun.Enabled {
		tun := map[string]interface{}{
			"addr": customization.Tun.Addresses[0], "tag": "tun-in", "mtu": customization.Tun.MTU,
			"auto_route": customization.Tun.AutoRoute, "dual_stack": len(customization.Tun.Addresses) > 1,
			"strict_route": customization.Tun.StrictRoute, "dns_hijack": customization.Tun.DNSHijack,
		}
		if len(customization.Tun.Addresses) > 1 {
			tun["secondary_addr"] = customization.Tun.Addresses[1]
		}
		runtime["tun"] = tun
	}
	return runtime
}

func zeroSubscriptionDNS(dns subscriptionDNSCustomization) map[string]interface{} {
	servers := make(map[string]interface{}, len(dns.Servers))
	for _, server := range dns.Servers {
		item := map[string]interface{}{"type": server.Type}
		if server.Type != "system" {
			item["host"] = server.Host
			item["port"] = server.Port
		}
		if server.Type == "doh" {
			item["path"] = server.Path
		}
		if server.ServerName != "" && (server.Type == "doh" || server.Type == "dot" || server.Type == "doq") {
			item["server_name"] = server.ServerName
		}
		servers[server.Tag] = item
	}
	answer := map[string]interface{}{"type": "real"}
	if dns.FakeIPEnabled {
		answer = map[string]interface{}{
			"type": "fake_ip", "cidr": dns.FakeIPv4Range, "ttl_seconds": 86400,
		}
		if dns.FakeIPv6Range != "" {
			answer["ipv6_cidr"] = dns.FakeIPv6Range
		}
	}
	result := map[string]interface{}{
		"servers": servers, "default_server": dns.DefaultServer, "dispatch": []interface{}{},
		"answer": answer, "policy": map[string]interface{}{"address_family": dns.Strategy},
	}
	if dns.CacheEnabled {
		result["cache"] = map[string]interface{}{"max_entries": dns.CacheCapacity}
	}
	return result
}

func singBoxSubscriptionDNS(dns subscriptionDNSCustomization) map[string]interface{} {
	servers := make([]map[string]interface{}, 0, len(dns.Servers)+1)
	for _, server := range dns.Servers {
		serverType := map[string]string{
			"system": "local", "udp": "udp", "tcp": "tcp", "doh": "https", "dot": "tls", "doq": "quic",
		}[server.Type]
		item := map[string]interface{}{"type": serverType, "tag": server.Tag}
		if server.Type != "system" {
			item["server"] = server.Host
			item["server_port"] = server.Port
		}
		if server.Type == "doh" {
			item["path"] = server.Path
		}
		if server.ServerName != "" && (server.Type == "doh" || server.Type == "dot" || server.Type == "doq") {
			item["tls"] = map[string]interface{}{"enabled": true, "server_name": server.ServerName}
		}
		servers = append(servers, item)
	}
	rules := []map[string]interface{}{}
	if dns.FakeIPEnabled {
		fake := map[string]interface{}{"type": "fakeip", "tag": "zboard-fakeip", "inet4_range": dns.FakeIPv4Range}
		if dns.FakeIPv6Range != "" {
			fake["inet6_range"] = dns.FakeIPv6Range
		}
		servers = append(servers, fake)
		rules = append(rules, map[string]interface{}{
			"query_type": []string{"A", "AAAA"}, "action": "route", "server": "zboard-fakeip",
		})
	}
	result := map[string]interface{}{
		"servers": servers, "rules": rules, "final": dns.DefaultServer, "strategy": dns.Strategy,
		"disable_cache": !dns.CacheEnabled,
	}
	if dns.CacheEnabled {
		result["cache_capacity"] = dns.CacheCapacity
	}
	return result
}

func singBoxSubscriptionInbounds(customization subscriptionTemplateCustomization) []map[string]interface{} {
	mixed := map[string]interface{}{
		"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": customization.MixedPort,
	}
	if customization.SystemProxy {
		mixed["set_system_proxy"] = true
	}
	inbounds := []map[string]interface{}{}
	if customization.MixedEnabled {
		inbounds = append(inbounds, mixed)
	}
	if customization.Tun.Enabled {
		tun := map[string]interface{}{
			"type": "tun", "tag": "tun-in", "address": customization.Tun.Addresses,
			"mtu": customization.Tun.MTU, "auto_route": customization.Tun.AutoRoute,
			"strict_route": customization.Tun.StrictRoute, "stack": "mixed",
		}
		inbounds = append(inbounds, tun)
	}
	return inbounds
}

func clashSubscriptionDNS(dns subscriptionDNSCustomization) map[string]interface{} {
	nameservers := make([]string, 0, len(dns.Servers))
	for _, server := range dns.Servers {
		nameservers = append(nameservers, clashSubscriptionDNSServer(server))
	}
	result := map[string]interface{}{
		"enable": true, "nameserver": nameservers, "ipv6": dns.Strategy != "ipv4_only",
		"enhanced-mode": "redir-host",
	}
	if dns.FakeIPEnabled {
		result["enhanced-mode"] = "fake-ip"
		result["fake-ip-range"] = dns.FakeIPv4Range
		if dns.FakeIPv6Range != "" {
			result["fake-ip-range6"] = dns.FakeIPv6Range
		}
	}
	if dns.CacheEnabled {
		result["cache-algorithm"] = "lru"
	}
	return result
}

func clashSubscriptionDNSServer(server subscriptionDNSServer) string {
	if server.Type == "system" {
		return "system"
	}
	host := server.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	scheme := map[string]string{"udp": "udp", "tcp": "tcp", "doh": "https", "dot": "tls", "doq": "quic"}[server.Type]
	endpoint := fmt.Sprintf("%s://%s:%d", scheme, host, server.Port)
	if server.Type == "doh" {
		endpoint += server.Path
	}
	if server.ServerName != "" && (server.Type == "doh" || server.Type == "dot" || server.Type == "doq") {
		endpoint += "#name-cert-verify=" + server.ServerName
	}
	return endpoint
}

func clashSubscriptionTun(tun subscriptionTunCustomization) map[string]interface{} {
	result := map[string]interface{}{
		"enable": true, "stack": "mixed", "auto-route": tun.AutoRoute,
		"auto-detect-interface": tun.AutoRoute, "strict-route": tun.StrictRoute, "mtu": tun.MTU,
	}
	if tun.DNSHijack {
		result["dns-hijack"] = []string{"any:53", "tcp://any:53"}
	}
	for _, raw := range tun.Addresses {
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Addr().Is6() {
			result["inet6-address"] = raw
			break
		}
	}
	return result
}

func singBoxRuntimeRouteRules(customization subscriptionTemplateCustomization, rules []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(rules)+2)
	if customization.Tun.Enabled {
		result = append(result, map[string]interface{}{"action": "sniff"})
		if customization.Tun.DNSHijack {
			result = append(result, map[string]interface{}{"protocol": "dns", "action": "hijack-dns"})
		}
	}
	if customization.Mode == subscriptionModeRule {
		result = append(result, rules...)
	}
	return result
}
