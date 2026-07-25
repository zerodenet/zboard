package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func validateGeneratedSubscriptionDocument(renderer string, source map[string]interface{}) error {
	document, err := normalizeSubscriptionDocument(source)
	if err != nil {
		return fmt.Errorf("校验生成配置: %w", err)
	}
	if containsSubscriptionInjectionMarker(document) {
		return errors.New("生成配置仍包含未展开的 Zboard 注入标记")
	}
	switch renderer {
	case subscriptionRendererClash:
		return validateGeneratedClashDocument(document)
	case subscriptionRendererSingBox:
		return validateGeneratedSingBoxDocument(document)
	case subscriptionRendererZnetSink:
		return validateGeneratedZeroDocument(document)
	default:
		return fmt.Errorf("不支持校验输出格式 %q", renderer)
	}
}

func normalizeSubscriptionDocument(source map[string]interface{}) (map[string]interface{}, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var document map[string]interface{}
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func validateGeneratedClashDocument(document map[string]interface{}) error {
	proxies, err := subscriptionObjectList(document["proxies"], "Clash proxies")
	if err != nil {
		return err
	}
	targets := map[string]struct{}{
		"DIRECT": {}, "REJECT": {}, "REJECT-DROP": {}, "PASS": {}, "GLOBAL": {},
	}
	for index, proxy := range proxies {
		name := subscriptionRequiredString(proxy, "name")
		if name == "" {
			return fmt.Errorf("Clash proxies[%d].name 不能为空", index)
		}
		if _, exists := targets[name]; exists {
			return fmt.Errorf("Clash 节点或策略组名称 %q 重复或使用保留名称", name)
		}
		targets[name] = struct{}{}
	}
	groups, err := subscriptionObjectList(document["proxy-groups"], "Clash proxy-groups")
	if err != nil {
		return err
	}
	groupNames := make(map[string]struct{}, len(groups))
	for index, group := range groups {
		name := subscriptionRequiredString(group, "name")
		if name == "" {
			return fmt.Errorf("Clash proxy-groups[%d].name 不能为空", index)
		}
		if _, exists := targets[name]; exists {
			return fmt.Errorf("Clash 节点或策略组名称 %q 重复或使用保留名称", name)
		}
		targets[name] = struct{}{}
		groupNames[name] = struct{}{}
	}
	groupGraph := make(map[string][]string, len(groups))
	for index, group := range groups {
		name := subscriptionRequiredString(group, "name")
		groupType := subscriptionRequiredString(group, "type")
		switch groupType {
		case "select", "url-test", "fallback", "load-balance", "relay":
		default:
			return fmt.Errorf("Clash 策略组 %q 使用了不支持的类型 %q", name, groupType)
		}
		members, err := subscriptionStringList(group["proxies"], fmt.Sprintf("Clash proxy-groups[%d].proxies", index))
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return fmt.Errorf("Clash 策略组 %q 至少需要一个成员", name)
		}
		for _, member := range members {
			if _, exists := targets[member]; !exists {
				return fmt.Errorf("Clash 策略组 %q 引用了不存在的节点或策略组 %q", name, member)
			}
			if _, isGroup := groupNames[member]; isGroup {
				groupGraph[name] = append(groupGraph[name], member)
			}
		}
		if groupType == "url-test" || groupType == "fallback" {
			if err := validateClientProbeFields(name, group, false); err != nil {
				return err
			}
		}
	}
	if err := validateRenderedSubscriptionGroupGraph("Clash", groupGraph); err != nil {
		return err
	}
	rules, err := subscriptionStringList(document["rules"], "Clash rules")
	if err != nil {
		return err
	}
	for index, rule := range rules {
		parts := strings.Split(rule, ",")
		if len(parts) < 2 {
			return fmt.Errorf("Clash rules[%d] 不是有效规则", index)
		}
		var target string
		switch parts[0] {
		case "MATCH":
			target = parts[1]
		case "RULE-SET":
			if len(parts) < 3 {
				return fmt.Errorf("Clash rules[%d] 缺少出站目标", index)
			}
			target = parts[2]
		default:
			continue
		}
		if _, exists := targets[target]; !exists {
			return fmt.Errorf("Clash rules[%d] 引用了不存在的出站目标 %q", index, target)
		}
	}
	return nil
}

func validateGeneratedSingBoxDocument(document map[string]interface{}) error {
	outbounds, err := subscriptionObjectList(document["outbounds"], "sing-box outbounds")
	if err != nil {
		return err
	}
	targets := make(map[string]struct{}, len(outbounds))
	groupNames := make(map[string]struct{}, len(outbounds))
	for index, outbound := range outbounds {
		tag := subscriptionRequiredString(outbound, "tag")
		if tag == "" {
			return fmt.Errorf("sing-box outbounds[%d].tag 不能为空", index)
		}
		if _, exists := targets[tag]; exists {
			return fmt.Errorf("sing-box outbound tag %q 重复", tag)
		}
		targets[tag] = struct{}{}
		groupType := subscriptionRequiredString(outbound, "type")
		if groupType == "selector" || groupType == "urltest" {
			groupNames[tag] = struct{}{}
		}
	}
	groupGraph := make(map[string][]string, len(groupNames))
	for _, outbound := range outbounds {
		tag := subscriptionRequiredString(outbound, "tag")
		groupType := subscriptionRequiredString(outbound, "type")
		if groupType != "selector" && groupType != "urltest" {
			continue
		}
		members, err := subscriptionStringList(outbound["outbounds"], fmt.Sprintf("sing-box outbound %q outbounds", tag))
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return fmt.Errorf("sing-box 策略组 %q 至少需要一个成员", tag)
		}
		for _, member := range members {
			if _, exists := targets[member]; !exists {
				return fmt.Errorf("sing-box 策略组 %q 引用了不存在的 outbound %q", tag, member)
			}
			if _, isGroup := groupNames[member]; isGroup {
				groupGraph[tag] = append(groupGraph[tag], member)
			}
		}
		if defaultTarget := strings.TrimSpace(subscriptionOptionalString(outbound, "default")); defaultTarget != "" {
			if !stringSliceContains(members, defaultTarget) {
				return fmt.Errorf("sing-box 策略组 %q 的默认成员 %q 不在成员列表中", tag, defaultTarget)
			}
		}
		if groupType == "urltest" {
			if err := validateClientProbeFields(tag, outbound, false); err != nil {
				return err
			}
		}
	}
	if err := validateRenderedSubscriptionGroupGraph("sing-box", groupGraph); err != nil {
		return err
	}
	route, ok := document["route"].(map[string]interface{})
	if !ok {
		return errors.New("sing-box route 必须是对象")
	}
	finalTarget := strings.TrimSpace(subscriptionOptionalString(route, "final"))
	if finalTarget == "" {
		return errors.New("sing-box route.final 不能为空")
	}
	if _, exists := targets[finalTarget]; !exists {
		return fmt.Errorf("sing-box route.final 引用了不存在的 outbound %q", finalTarget)
	}
	rules, err := subscriptionObjectListAllowMissing(route["rules"], "sing-box route.rules")
	if err != nil {
		return err
	}
	for index, rule := range rules {
		if subscriptionOptionalString(rule, "action") != "route" {
			continue
		}
		target := strings.TrimSpace(subscriptionOptionalString(rule, "outbound"))
		if target == "" {
			return fmt.Errorf("sing-box route.rules[%d] 缺少 outbound", index)
		}
		if _, exists := targets[target]; !exists {
			return fmt.Errorf("sing-box route.rules[%d] 引用了不存在的 outbound %q", index, target)
		}
	}
	return nil
}

func validateGeneratedZeroDocument(document map[string]interface{}) error {
	outbounds, err := subscriptionObjectList(document["outbounds"], "Zero outbounds")
	if err != nil {
		return err
	}
	targets := make(map[string]struct{}, len(outbounds))
	for index, outbound := range outbounds {
		tag := subscriptionRequiredString(outbound, "tag")
		if tag == "" {
			return fmt.Errorf("Zero outbounds[%d].tag 不能为空", index)
		}
		if _, exists := targets[tag]; exists {
			return fmt.Errorf("Zero outbound tag %q 重复", tag)
		}
		targets[tag] = struct{}{}
	}
	groups, err := subscriptionObjectList(document["outbound_groups"], "Zero outbound_groups")
	if err != nil {
		return err
	}
	groupNames := make(map[string]struct{}, len(groups))
	for index, group := range groups {
		tag := subscriptionRequiredString(group, "tag")
		if tag == "" {
			return fmt.Errorf("Zero outbound_groups[%d].tag 不能为空", index)
		}
		if _, exists := targets[tag]; exists {
			return fmt.Errorf("Zero outbound 或策略组 tag %q 重复", tag)
		}
		targets[tag] = struct{}{}
		groupNames[tag] = struct{}{}
	}
	groupGraph := make(map[string][]string, len(groups))
	for index, group := range groups {
		tag := subscriptionRequiredString(group, "tag")
		groupType := subscriptionRequiredString(group, "type")
		switch groupType {
		case "selector", "fallback", "url_test", "load_balance":
			members, err := subscriptionStringList(group["outbounds"], fmt.Sprintf("Zero outbound_groups[%d].outbounds", index))
			if err != nil {
				return err
			}
			if len(members) == 0 {
				return fmt.Errorf("Zero 策略组 %q 至少需要一个成员", tag)
			}
			for _, member := range members {
				if _, exists := targets[member]; !exists {
					return fmt.Errorf("Zero 策略组 %q 引用了不存在的 outbound %q", tag, member)
				}
				if _, isGroup := groupNames[member]; isGroup {
					groupGraph[tag] = append(groupGraph[tag], member)
				}
			}
			if defaultTarget := strings.TrimSpace(subscriptionOptionalString(group, "default")); defaultTarget != "" && !stringSliceContains(members, defaultTarget) {
				return fmt.Errorf("Zero 策略组 %q 的默认成员 %q 不在成员列表中", tag, defaultTarget)
			}
			if groupType == "url_test" {
				if err := validateClientProbeFields(tag, group, true); err != nil {
					return err
				}
			}
		case "relay":
			members, err := subscriptionStringList(group["proxies"], fmt.Sprintf("Zero outbound_groups[%d].proxies", index))
			if err != nil {
				return err
			}
			if len(members) < 2 {
				return fmt.Errorf("Zero relay 策略组 %q 至少需要两个成员", tag)
			}
			for _, member := range members {
				if _, exists := targets[member]; !exists {
					return fmt.Errorf("Zero relay 策略组 %q 引用了不存在的 outbound %q", tag, member)
				}
				if _, isGroup := groupNames[member]; isGroup {
					groupGraph[tag] = append(groupGraph[tag], member)
				}
			}
		default:
			return fmt.Errorf("Zero 策略组 %q 使用了不支持的类型 %q", tag, groupType)
		}
	}
	if err := validateRenderedSubscriptionGroupGraph("Zero", groupGraph); err != nil {
		return err
	}
	route, ok := document["route"].(map[string]interface{})
	if !ok {
		return errors.New("Zero route 必须是对象")
	}
	if err := validateZeroRouteAction(route["final"], "Zero route.final", targets); err != nil {
		return err
	}
	rules, err := subscriptionObjectListAllowMissing(route["rules"], "Zero route.rules")
	if err != nil {
		return err
	}
	for index, rule := range rules {
		if err := validateZeroRouteAction(rule["action"], fmt.Sprintf("Zero route.rules[%d].action", index), targets); err != nil {
			return err
		}
	}
	return nil
}

func validateZeroRouteAction(value interface{}, field string, targets map[string]struct{}) error {
	action, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s 必须是对象", field)
	}
	actionType := subscriptionRequiredString(action, "type")
	switch actionType {
	case "direct", "reject":
		return nil
	case "route":
		target := subscriptionRequiredString(action, "outbound")
		if _, exists := targets[target]; !exists {
			return fmt.Errorf("%s 引用了不存在的 outbound %q", field, target)
		}
		return nil
	default:
		return fmt.Errorf("%s 使用了不支持的动作 %q", field, actionType)
	}
}

func validateClientProbeFields(groupName string, group map[string]interface{}, httpOnly bool) error {
	rawURL := subscriptionRequiredString(group, "url")
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("策略组 %q 的检测地址不是有效 HTTP(S) URL", groupName)
	}
	if httpOnly && parsed.Scheme != "http" {
		return fmt.Errorf("策略组 %q 的检测地址必须使用 HTTP", groupName)
	}
	if interval, exists := group["interval"]; exists {
		if value, ok := interval.(float64); ok && value <= 0 {
			return fmt.Errorf("策略组 %q 的检测间隔必须大于 0", groupName)
		}
	}
	if interval, exists := group["interval_seconds"]; exists {
		if value, ok := interval.(float64); !ok || value <= 0 {
			return fmt.Errorf("策略组 %q 的检测间隔必须大于 0", groupName)
		}
	}
	return nil
}

func subscriptionObjectList(value interface{}, field string) ([]map[string]interface{}, error) {
	if value == nil {
		return nil, fmt.Errorf("%s 不能为空", field)
	}
	return subscriptionObjectListAllowMissing(value, field)
}

func subscriptionObjectListAllowMissing(value interface{}, field string) ([]map[string]interface{}, error) {
	if value == nil {
		return []map[string]interface{}{}, nil
	}
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s 必须是数组", field)
	}
	result := make([]map[string]interface{}, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s[%d] 必须是对象", field, index)
		}
		result = append(result, object)
	}
	return result, nil
}

func subscriptionStringList(value interface{}, field string) ([]string, error) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s 必须是数组", field)
	}
	result := make([]string, 0, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%s[%d] 必须是非空字符串", field, index)
		}
		result = append(result, text)
	}
	return result, nil
}

func subscriptionRequiredString(source map[string]interface{}, key string) string {
	value, _ := source[key].(string)
	return strings.TrimSpace(value)
}

func subscriptionOptionalString(source map[string]interface{}, key string) string {
	value, _ := source[key].(string)
	return value
}

func stringSliceContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func validateRenderedSubscriptionGroupGraph(client string, graph map[string][]string) error {
	state := make(map[string]uint8, len(graph))
	var visit func(string) error
	visit = func(group string) error {
		if state[group] == 1 {
			return fmt.Errorf("%s 策略组引用形成循环，涉及 %q", client, group)
		}
		if state[group] == 2 {
			return nil
		}
		state[group] = 1
		for _, child := range graph[group] {
			if err := visit(child); err != nil {
				return err
			}
		}
		state[group] = 2
		return nil
	}
	for group := range graph {
		if err := visit(group); err != nil {
			return err
		}
	}
	return nil
}

func containsSubscriptionInjectionMarker(value interface{}) bool {
	switch current := value.(type) {
	case string:
		return current == subscriptionAllNodesMarker ||
			current == subscriptionGeneratedProxiesMarker ||
			current == subscriptionGeneratedOutboundsMarker
	case map[string]interface{}:
		if marker, exists := current["$zboard"]; exists && marker == subscriptionGeneratedOutboundsMarker {
			return true
		}
		for _, child := range current {
			if containsSubscriptionInjectionMarker(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range current {
			if containsSubscriptionInjectionMarker(child) {
				return true
			}
		}
	}
	return false
}
