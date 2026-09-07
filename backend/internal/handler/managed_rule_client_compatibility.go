package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const managedRuleClientCompatibilityMessage = "规则集包含进程匹配，仅支持 Clash / sing-box；Zero 的 ZRS 当前不支持，不能省略进程规则后发布。"

var errManagedRuleClientCompatibility = errors.New(managedRuleClientCompatibilityMessage)

func isManagedClientRule(kind string) bool {
	return kind == managedRuleTypeProcessName || kind == managedRuleTypeProcessPath
}

func normalizeManagedProcessValue(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || len(value) > managedRuleMaxValueBytes {
		return "", errors.New("进程名称或路径不能为空，且必须是有效 UTF-8 文本")
	}
	for _, char := range value {
		if unicode.IsControl(char) || char == ',' {
			return "", errors.New("进程名称或路径不能包含控制字符或逗号")
		}
	}
	if kind == managedRuleTypeProcessName && strings.ContainsAny(value, `/\`) {
		return "", errors.New("PROCESS-NAME 只接受进程名称；完整路径请使用 PROCESS-PATH")
	}
	// Matching is client-owned; preserve case and spaces in executable names.
	return value, nil
}

func managedRuleAllRules(document managedRuleDocument) []managedRule {
	rules := make([]managedRule, 0, len(document.Rules)+len(document.ClientRules))
	rules = append(rules, document.Rules...)
	return append(rules, document.ClientRules...)
}

func managedRuleStorageFormat(content []byte) (string, error) {
	// Callers provide validated canonical content. Read only the extension,
	// avoiding another normalization/sort of potentially millions of rules.
	var document struct {
		ClientRules []json.RawMessage `json:"client_rules"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return "", err
	}
	if len(document.ClientRules) > 0 {
		return managedRuleSetFormatClient, nil
	}
	return managedRuleSetFormatCanonical, nil
}

func guardManagedRuleClientUpdate(tx *gorm.DB, ruleID uint, format string) error {
	if format != managedRuleSetFormatClient {
		return nil
	}
	var names []string
	err := tx.Model(&model.SubscriptionTemplate{}).
		Joins("JOIN subscription_template_rule_set_bindings b ON b.subscription_template_id = subscription_templates.id").
		Where("b.subscription_rule_set_id = ? AND subscription_templates.renderer IN ?", ruleID, []string{subscriptionRendererZNetSink, "zero"}).
		Pluck("subscription_templates.name", &names).Error
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("%w 请先调整已引用的 Zero 模板：%s", errManagedRuleClientCompatibility, strings.Join(names, "、"))
	}
	return nil
}
