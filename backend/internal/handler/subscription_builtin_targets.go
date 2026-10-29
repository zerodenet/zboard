package handler

import (
	"fmt"
	"reflect"
)

func subscriptionSelectionTargets(renderer string) (direct, reject string) {
	if renderer == subscriptionRendererClash {
		return "DIRECT", "REJECT"
	}
	return "direct", "block"
}

// Older advanced templates reference DIRECT/REJECT. Resolve those aliases only
// at the native JSON output boundary, after node injection. Arbitrary node and
// group tags remain case-sensitive; protocol names and reject actions are not tags.
func normalizeSubscriptionBuiltinTargets(renderer string, source map[string]interface{}) error {
	document, err := normalizeSubscriptionDocument(source)
	if err != nil {
		return err
	}
	outbounds, err := subscriptionObjectList(document["outbounds"], "outbounds")
	if err != nil {
		return err
	}
	declared := make(map[string]map[string]interface{}, len(outbounds))
	for _, outbound := range outbounds {
		declared[subscriptionRequiredString(outbound, "tag")] = outbound
	}
	groups := outbounds
	if renderer == subscriptionRendererZnetSink {
		groups, err = subscriptionObjectList(document["outbound_groups"], "outbound_groups")
		if err != nil {
			return err
		}
		for _, group := range groups {
			declared[subscriptionRequiredString(group, "tag")] = group
		}
	}
	protocol := func(outbound map[string]interface{}) string {
		if renderer == subscriptionRendererZnetSink {
			value, _ := outbound["protocol"].(map[string]interface{})
			return subscriptionOptionalString(value, "type")
		}
		return subscriptionOptionalString(outbound, "type")
	}
	aliases := make(map[string]string)
	for alias, target := range map[string]string{"DIRECT": "direct", "REJECT": "block"} {
		if existing, ok := declared[alias]; ok {
			if protocol(existing) == target {
				aliases[alias] = target
			}
		} else if existing, ok := declared[target]; ok && protocol(existing) == target {
			aliases[alias] = target
		}
	}
	seen := make(map[string]map[string]interface{}, len(outbounds))
	fromAlias := make(map[string]bool)
	canonical := make([]interface{}, 0, len(outbounds))
	for _, outbound := range outbounds {
		tag := subscriptionRequiredString(outbound, "tag")
		originalTag := tag
		target, renamed := aliases[tag]
		if renamed {
			tag = target
			outbound["tag"] = target
		}
		if previous, exists := seen[tag]; exists && (renamed || fromAlias[tag]) {
			if !reflect.DeepEqual(previous, outbound) {
				return fmt.Errorf("内置出站别名 %q 与 %q 的配置冲突，请为自定义出站使用独立名称", originalTag, tag)
			}
			fromAlias[tag] = true
			continue
		}
		seen[tag] = outbound
		fromAlias[tag] = renamed
		canonical = append(canonical, outbound)
	}
	document["outbounds"] = canonical
	for _, group := range groups {
		kind := subscriptionOptionalString(group, "type")
		switch kind {
		case "selector", "urltest", "url_test", "fallback", "load_balance":
			rewriteSubscriptionTargetList(group, "outbounds", aliases, true)
			rewriteSubscriptionTarget(group, "default", aliases)
		case "relay":
			// Repeated relay hops have meaning; do not collapse them.
			rewriteSubscriptionTargetList(group, "proxies", aliases, false)
		}
	}
	if route, ok := document["route"].(map[string]interface{}); ok {
		rewriteSubscriptionTarget(route, "final", aliases)
		rewriteSubscriptionTargetList(route, "auto_outbounds", aliases, true)
		rewriteSubscriptionTargetFields(route, "outbound", aliases)
	}
	if mode, ok := document["mode"].(map[string]interface{}); ok {
		rewriteSubscriptionTarget(mode, "outbound", aliases)
	}
	for _, outbound := range outbounds {
		rewriteSubscriptionTargetFields(outbound, "detour", aliases)
	}
	for _, field := range []string{"dns", "runtime"} {
		rewriteSubscriptionTargetFields(document[field], "detour", aliases)
	}
	clear(source)
	for key, value := range document {
		source[key] = value
	}
	return nil
}

func rewriteSubscriptionTarget(object map[string]interface{}, field string, aliases map[string]string) {
	if value, ok := object[field].(string); ok {
		if target, exists := aliases[value]; exists {
			object[field] = target
		}
	}
}

func rewriteSubscriptionTargetList(object map[string]interface{}, field string, aliases map[string]string, deduplicate bool) {
	values, ok := object[field].([]interface{})
	if !ok {
		return
	}
	seen := make(map[string]bool)
	builtins := make(map[string]bool)
	for _, target := range aliases {
		builtins[target] = true
	}
	next := make([]interface{}, 0, len(values))
	for _, value := range values {
		if tag, ok := value.(string); ok {
			if target, exists := aliases[tag]; exists {
				tag = target
			}
			if deduplicate && builtins[tag] && seen[tag] {
				continue
			}
			seen[tag] = true
			value = tag
		}
		next = append(next, value)
	}
	object[field] = next
}

func rewriteSubscriptionTargetFields(value interface{}, field string, aliases map[string]string) {
	switch current := value.(type) {
	case map[string]interface{}:
		rewriteSubscriptionTarget(current, field, aliases)
		for _, child := range current {
			rewriteSubscriptionTargetFields(child, field, aliases)
		}
	case []interface{}:
		for _, child := range current {
			rewriteSubscriptionTargetFields(child, field, aliases)
		}
	}
}
