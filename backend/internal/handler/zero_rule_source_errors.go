package handler

import (
	"fmt"
	"net/netip"
	"strings"
)

// Diagnose a mismatched source format without silently changing the import
// contract saved for subsequent synchronization.
func managedRuleSourceLineError(format, location string, index int, line string, cause error) error {
	position := fmt.Sprintf("第 %d 行", index)
	if location == "payload item" {
		position = fmt.Sprintf("payload 第 %d 项", index)
	}
	if kind, _, ok := strings.Cut(line, ","); ok && format != managedRuleSourceClashClassical {
		switch strings.ToUpper(strings.TrimSpace(kind)) {
		case "PROCESS-NAME", "PROCESS-PATH", "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6":
			selected := "域名列表"
			if format == managedRuleSourceCIDRList {
				selected = "CIDR 列表"
			}
			return fmt.Errorf("%s：当前选择“%s”，但规则包含 %s 类型前缀。请将“远端来源格式”改为“Clash classical”后重新导入。", position, selected, strings.ToUpper(strings.TrimSpace(kind)))
		}
	}
	if format == managedRuleSourceCIDRList {
		return fmt.Errorf("%s：CIDR 列表只接受纯 IP 网段，例如 192.0.2.0/24 或 2001:db8::/32，不能填写域名或规则类型前缀：%w", position, cause)
	}
	if format == managedRuleSourceClashClassical {
		if _, err := netip.ParsePrefix(line); err == nil {
			return fmt.Errorf("%s：当前选择“Clash classical”，但内容是纯 IP 网段。请选择“CIDR 列表”，或使用 IP-CIDR,网段 / IP-CIDR6,网段 格式。", position)
		}
	}
	return fmt.Errorf("%s：%w", position, cause)
}
