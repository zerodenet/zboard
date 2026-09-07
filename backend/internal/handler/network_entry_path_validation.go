package handler

import "fmt"

// These are explicit unsupported transports in Zero's current UDP path. The
// native validator remains authoritative for fields, graph cycles and protocol parsing.
func (path networkEntryPath) validateDatagramPath() error {
	nodes := map[string]map[string]interface{}{}
	for _, list := range [][]map[string]interface{}{path.Outbounds, path.Groups} {
		for _, item := range list {
			tag, _ := item["tag"].(string)
			nodes[tag] = item
		}
	}
	visited := map[string]bool{}
	var visit func(string, bool) error
	visit = func(tag string, finalHop bool) error {
		key := fmt.Sprintf("%s/%t", tag, finalHop)
		if visited[key] {
			return nil
		}
		visited[key] = true
		item := nodes[tag]
		if item == nil {
			return nil
		} // native graph validation reports missing references
		if protocol, ok := item["protocol"].(map[string]interface{}); ok {
			kind, _ := protocol["type"].(string)
			if kind == "http" {
				return fmt.Errorf("HTTP CONNECT 出口不支持原始 UDP，请选择支持 UDP 的出口")
			}
			if finalHop && (kind == "socks5" || kind == "direct" || kind == "block") {
				return fmt.Errorf("%s 不支持作为 UDP 代理链最后一跳；例如可使用 SOCKS5 → Shadowsocks", kind)
			}
			if kind == "vless" && protocol["flow"] == "xtls-rprx-vision" {
				return fmt.Errorf("VLESS Vision 不支持 UDP 转发，请使用兼容 UDP 的路径")
			}
			return nil
		}
		kind, _ := item["type"].(string)
		memberKey := "outbounds"
		if kind == "relay" {
			memberKey = "proxies"
		}
		members, _ := item[memberKey].([]interface{})
		if kind == "relay" {
			if len(members) > 0 {
				last, _ := members[len(members)-1].(string)
				return visit(last, true)
			}
			return nil
		}
		for _, member := range members {
			tag, _ := member.(string)
			if err := visit(tag, finalHop); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(path.Target, false)
}
