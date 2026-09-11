package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func proxyPoolPathFromLinks(content []byte) (networkEntryPath, int, error) {
	raw := strings.TrimSpace(string(content))
	if !strings.Contains(raw, "://") {
		decoded, err := decodeProxyPoolSubscriptionBase64(raw)
		if err != nil {
			return networkEntryPath{}, 0, errors.New("节点链接订阅不是有效的文本或 Base64")
		}
		raw = string(decoded)
	}
	nodes := []map[string]interface{}{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		node, err := proxyPoolLink(line)
		if err != nil {
			return networkEntryPath{}, 0, fmt.Errorf("第 %d 条节点链接：%w", len(nodes)+1, err)
		}
		// Fragment names need not be unique. Keep all nodes with stable unique tags.
		node["tag"] = fmt.Sprintf("%d-%s", len(nodes)+1, firstConfigString(clashString(node, "tag"), "node"))
		nodes = append(nodes, node)
	}
	payload, _ := json.Marshal(map[string]interface{}{"outbounds": nodes})
	return proxyPoolPathFromSingBox(payload)
}

func proxyPoolLink(raw string) (map[string]interface{}, error) {
	scheme, body, ok := strings.Cut(raw, "://")
	if !ok {
		return nil, errors.New("无效节点链接")
	}
	scheme = strings.ToLower(scheme)
	if scheme == "vmess" {
		return proxyPoolVMessLink(body)
	}
	if scheme == "ss" && !strings.Contains(body, "@") {
		encoded, fragment, _ := strings.Cut(body, "#")
		decoded, err := decodeProxyPoolSubscriptionBase64(encoded)
		if err != nil {
			return nil, errors.New("无效 SS Base64")
		}
		raw = "ss://" + string(decoded)
		if fragment != "" {
			raw += "#" + fragment
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, errors.New("节点链接缺少地址或凭据")
	}
	port, err := strconv.Atoi(u.Port())
	if u.Port() == "" && (scheme == "hysteria2" || scheme == "hy2") {
		port = 443
		err = nil
	}
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("节点端口无效")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("节点查询参数无效")
	}
	allowed := map[string]bool{}
	for _, key := range []string{"security", "type", "sni", "peer", "fp", "pbk", "sid", "flow", "path", "host", "serviceName", "alpn", "allowInsecure", "insecure", "encryption"} {
		allowed[key] = true
	}
	for key := range q {
		if !allowed[key] {
			return nil, fmt.Errorf("暂不支持链接参数 %s", key)
		}
	}
	node := map[string]interface{}{"tag": u.Fragment, "server": u.Hostname(), "server_port": port}
	switch scheme {
	case "ss":
		node["type"] = "shadowsocks"
		method := u.User.Username()
		password, hasPassword := u.User.Password()
		if !hasPassword {
			decoded, err := decodeProxyPoolSubscriptionBase64(method)
			if err != nil {
				return nil, errors.New("无效 SS 凭据")
			}
			method, password, hasPassword = strings.Cut(string(decoded), ":")
		}
		if !hasPassword {
			return nil, errors.New("SS 缺少密码")
		}
		node["method"] = method
		node["password"] = password
	case "vless":
		node["type"] = "vless"
		node["uuid"] = u.User.Username()
		if flow := q.Get("flow"); flow != "" {
			node["flow"] = flow
		}
		if enc := q.Get("encryption"); enc != "" && enc != "none" {
			return nil, errors.New("暂不支持 VLESS encryption")
		}
	case "trojan", "hysteria2", "hy2":
		kind := scheme
		if kind == "hy2" {
			kind = "hysteria2"
		}
		node["type"] = kind
		password := u.User.Username()
		if suffix, ok := u.User.Password(); ok {
			password += ":" + suffix
		}
		node["password"] = password
	default:
		return nil, fmt.Errorf("不支持链接类型 %s", scheme)
	}
	security := q.Get("security")
	if security == "" && (scheme == "trojan" || scheme == "hy2" || scheme == "hysteria2") {
		security = "tls"
	}
	if security != "" && security != "none" && security != "tls" && security != "reality" {
		return nil, errors.New("不支持链接 security")
	}
	if security == "tls" || security == "reality" {
		tls := map[string]interface{}{"enabled": true}
		if sni := firstConfigString(q.Get("sni"), q.Get("peer")); sni != "" {
			tls["server_name"] = sni
		}
		for _, key := range []string{"insecure", "allowInsecure"} {
			if value := q.Get(key); value != "" {
				insecure, err := strconv.ParseBool(value)
				if err != nil {
					return nil, errors.New("无效 insecure 参数")
				}
				tls["insecure"] = insecure
			}
		}
		if fp := q.Get("fp"); fp != "" {
			tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
		}
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		if security == "reality" {
			tls["reality"] = map[string]interface{}{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}
		}
		node["tls"] = tls
	} else if scheme == "trojan" || scheme == "hy2" || scheme == "hysteria2" {
		return nil, errors.New("该协议不能关闭 TLS")
	}
	switch network := q.Get("type"); network {
	case "", "tcp":
	case "ws":
		tr := map[string]interface{}{"type": "ws", "path": firstConfigString(q.Get("path"), "/")}
		if host := q.Get("host"); host != "" {
			tr["headers"] = map[string]interface{}{"Host": host}
		}
		node["transport"] = tr
	case "grpc":
		node["transport"] = map[string]interface{}{"type": "grpc", "service_name": q.Get("serviceName")}
	default:
		return nil, errors.New("不支持该链接传输方式")
	}
	return node, nil
}

func proxyPoolVMessLink(body string) (map[string]interface{}, error) {
	decoded, err := decodeProxyPoolSubscriptionBase64(body)
	if err != nil {
		return nil, errors.New("VMess 链接需要 Base64 JSON")
	}
	var v map[string]interface{}
	if json.Unmarshal(decoded, &v) != nil {
		return nil, errors.New("无效 VMess JSON")
	}
	if clashInt(v, "aid") != 0 {
		return nil, errors.New("不支持 VMess alterId")
	}
	if header := clashString(v, "type"); header != "" && header != "none" {
		return nil, errors.New("不支持 VMess 伪装类型")
	}
	node := map[string]interface{}{"type": "vmess", "tag": v["ps"], "server": v["add"], "server_port": clashInt(v, "port"), "uuid": v["id"], "security": firstConfigString(clashString(v, "scy"), "auto")}
	if security := clashString(v, "tls"); security != "" && security != "none" {
		if security != "tls" {
			return nil, errors.New("不支持 VMess TLS 类型")
		}
		tls := map[string]interface{}{"enabled": true, "server_name": firstConfigString(clashString(v, "sni"), clashString(v, "host"))}
		if fp := clashString(v, "fp"); fp != "" {
			tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
		}
		if alpn := clashString(v, "alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		node["tls"] = tls
	}
	switch clashString(v, "net") {
	case "", "tcp":
	case "ws":
		node["transport"] = map[string]interface{}{"type": "ws", "path": firstConfigString(clashString(v, "path"), "/")}
		if host := clashString(v, "host"); host != "" {
			node["transport"].(map[string]interface{})["headers"] = map[string]interface{}{"Host": host}
		}
	case "grpc":
		node["transport"] = map[string]interface{}{"type": "grpc", "service_name": clashString(v, "path")}
	default:
		return nil, errors.New("不支持 VMess 传输方式")
	}
	return node, nil
}
