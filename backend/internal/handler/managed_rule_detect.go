package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

func detectManagedRuleSourceFormat(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return "", fmt.Errorf("规则 JSON 格式无效：%w", err)
		}
		if object["inbounds"] != nil || object["outbounds"] != nil || object["route"] != nil || object["dns"] != nil {
			return "", fmt.Errorf("这是客户端配置，不是独立规则集；请在订阅模板中配置，或选择 Clash/Provider 下的规则文件")
		}
		return managedRuleSourceZeroRuleIR, nil
	}
	lines, wrapped, err := decodeManagedRuleProviderPayload(trimmed)
	if err != nil {
		return "", err
	}
	if !wrapped {
		lines = strings.Split(string(trimmed), "\n")
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, `"route":`) || strings.HasPrefix(line, `"log":`) || strings.HasPrefix(line, `"dns":`) {
			return "", fmt.Errorf("这是 sing-box 配置片段，不是独立规则集；请选择 Clash/Provider 下的规则文件")
		}
		if strings.Contains(line, ",") {
			return managedRuleSourceClashClassical, nil
		}
		if _, err := netip.ParsePrefix(line); err == nil {
			return managedRuleSourceCIDRList, nil
		}
		return managedRuleSourceDomainList, nil
	}
	return "", fmt.Errorf("没有可导入的有效规则，文件为空或只有注释")
}
