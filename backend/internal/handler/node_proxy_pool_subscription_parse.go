package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

func parseProxyPoolSubscription(content []byte, format string) (networkEntryPath, int, error) {
	format, err := normalizeProxyPoolSubscriptionFormat(format)
	if err != nil {
		return networkEntryPath{}, 0, err
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return networkEntryPath{}, 0, errors.New("订阅响应为空")
	}
	parseZero := func(raw string) (networkEntryPath, int, error) {
		var document map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return networkEntryPath{}, 0, errors.New("订阅不是有效的 Zero JSON")
		}
		return proxyPoolPathFromZeroDocument(document)
	}
	if format == "zero" {
		decoded, err := decodeProxyPoolSubscriptionBase64(trimmed)
		if err != nil {
			return networkEntryPath{}, 0, errors.New("无效 Base64")
		}
		return parseZero(string(decoded))
	}
	if format == "zero-json" {
		return parseZero(trimmed)
	}
	if format == "clash" {
		return proxyPoolPathFromClash([]byte(trimmed))
	}
	if format == "sing-box" {
		return proxyPoolPathFromSingBox([]byte(trimmed))
	}
	if format == "links" {
		return proxyPoolPathFromLinks([]byte(trimmed))
	}
	if decoded, err := decodeProxyPoolSubscriptionBase64(trimmed); err == nil {
		trimmed = strings.TrimSpace(string(decoded))
	}
	if strings.HasPrefix(trimmed, "{") {
		var document map[string]interface{}
		if json.Unmarshal([]byte(trimmed), &document) != nil {
			return networkEntryPath{}, 0, errors.New("无效订阅 JSON")
		}
		if outbounds, ok := document["outbounds"].([]interface{}); ok && len(outbounds) > 0 {
			if first, ok := outbounds[0].(map[string]interface{}); ok && first["type"] != nil {
				return proxyPoolPathFromSingBox([]byte(trimmed))
			}
		}
		return parseZero(trimmed)
	}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "://") && !strings.Contains(line, ": ") {
			return proxyPoolPathFromLinks([]byte(trimmed))
		}
		break
	}
	return proxyPoolPathFromClash([]byte(trimmed))
}

func decodeProxyPoolSubscriptionBase64(raw string) ([]byte, error) {
	compact := strings.Join(strings.Fields(raw), "")
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(compact); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}

func proxyPoolPathFromZeroDocument(document map[string]interface{}) (networkEntryPath, int, error) {
	rawOutbounds, ok := document["outbounds"].([]interface{})
	if !ok {
		return networkEntryPath{}, 0, errors.New("Zero 订阅缺少 outbounds")
	}
	path := networkEntryPath{Outbounds: make([]map[string]interface{}, 0, len(rawOutbounds))}
	for _, raw := range rawOutbounds {
		item, ok := raw.(map[string]interface{})
		if !ok || item["protocol"] == nil {
			return networkEntryPath{}, 0, errors.New("Zero 节点缺少 protocol")
		}
		if ok {
			path.Outbounds = append(path.Outbounds, item)
		}
	}
	if rawGroups, ok := document["outbound_groups"].([]interface{}); ok {
		for _, raw := range rawGroups {
			if item, ok := raw.(map[string]interface{}); ok {
				path.Groups = append(path.Groups, item)
			}
		}
	}
	path.Target, _ = document["target"].(string)
	if path.Target == "" {
		if route, ok := document["route"].(map[string]interface{}); ok {
			if final, ok := route["final"].(map[string]interface{}); ok {
				path.Target, _ = final["outbound"].(string)
			}
		}
	}
	tags := map[string]bool{}
	for _, item := range path.Outbounds {
		if tag, _ := item["tag"].(string); tag != "" {
			tags[tag] = true
		}
	}
	for _, item := range path.Groups {
		if tag, _ := item["tag"].(string); tag != "" {
			tags[tag] = true
		}
	}
	if !tags[path.Target] {
		if len(path.Groups) > 0 {
			path.Target, _ = path.Groups[0]["tag"].(string)
		}
		if path.Target == "" && len(path.Outbounds) > 0 {
			path.Target, _ = path.Outbounds[0]["tag"].(string)
		}
	}
	count := 0
	for _, item := range path.Outbounds {
		protocol, _ := item["protocol"].(map[string]interface{})
		kind, _ := protocol["type"].(string)
		if kind != "direct" && kind != "block" && kind != "dns" {
			count++
		}
	}
	if count == 0 || path.Target == "" {
		return networkEntryPath{}, 0, errors.New("订阅中没有可用代理节点")
	}
	return path, count, nil
}
