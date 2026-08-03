package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v2"
)

const (
	subscriptionCustomizationVersion     = 2
	maxSubscriptionRuleSets              = 64
	maxSubscriptionPolicyGroups          = 16
	maxSubscriptionAdvancedSourceBytes   = 128 << 10
	maxSubscriptionNodePatternBytes      = 256
	subscriptionTargetDirect             = "direct"
	subscriptionTargetReject             = "reject"
	subscriptionTargetGroupPrefix        = "group:"
	subscriptionAllNodesMarker           = "$zboard:all-nodes"
	subscriptionGeneratedProxiesMarker   = "$zboard:generated-proxies"
	subscriptionGeneratedOutboundsMarker = "generated-outbounds"
	defaultSubscriptionProbeURL          = "http://www.gstatic.com/generate_204"
	legacyDefaultSubscriptionProbeURL    = "https://www.gstatic.com/generate_204"
	defaultSubscriptionMixedPort         = 7890
)

var (
	subscriptionRuleSetTagPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	subscriptionPolicyGroupIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

type subscriptionTemplateCustomization struct {
	Version        int                                `json:"version"`
	MixedPort      int                                `json:"mixed_port"`
	MainGroup      string                             `json:"main_group"`
	PolicyGroups   []subscriptionPolicyGroup          `json:"policy_groups"`
	Final          string                             `json:"final,omitempty"`
	RuleSets       []subscriptionRuleSetCustomization `json:"rule_sets,omitempty"`
	AdvancedSource string                             `json:"advanced_source,omitempty"`
}

type subscriptionPolicyGroup struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	IncludePattern string   `json:"include_pattern,omitempty"`
	ExcludePattern string   `json:"exclude_pattern,omitempty"`
	IncludeGroups  []string `json:"include_groups,omitempty"`
	DefaultGroup   string   `json:"default_group,omitempty"`
	ProbeURL       string   `json:"probe_url,omitempty"`
	Interval       int      `json:"interval,omitempty"`
	Tolerance      int      `json:"tolerance,omitempty"`
}

type subscriptionRuleSetCustomization struct {
	RuleSetID uint   `json:"rule_set_id,omitempty"`
	Tag       string `json:"tag,omitempty"`
	URL       string `json:"url,omitempty"`
	Behavior  string `json:"behavior,omitempty"`
	Format    string `json:"format,omitempty"`
	Target    string `json:"target"`
	Interval  int    `json:"interval,omitempty"`
}

type legacySubscriptionTemplateCustomization struct {
	Version        int                                      `json:"version"`
	Profile        string                                   `json:"profile,omitempty"`
	GroupName      string                                   `json:"group_name,omitempty"`
	Final          string                                   `json:"final,omitempty"`
	RuleSets       []legacySubscriptionRuleSetCustomization `json:"rule_sets,omitempty"`
	AdvancedSource string                                   `json:"advanced_source,omitempty"`
}

type legacySubscriptionRuleSetCustomization struct {
	RuleSetID uint   `json:"rule_set_id,omitempty"`
	Tag       string `json:"tag,omitempty"`
	URL       string `json:"url,omitempty"`
	Behavior  string `json:"behavior,omitempty"`
	Format    string `json:"format,omitempty"`
	Action    string `json:"action"`
	Interval  int    `json:"interval,omitempty"`
}

func defaultSubscriptionCustomization(renderer string) subscriptionTemplateCustomization {
	mainGroup := subscriptionPolicyGroup{
		ID: "main", Name: "节点选择", Type: "select",
		IncludeGroups: []string{"auto"}, DefaultGroup: "auto",
	}
	autoGroup := subscriptionPolicyGroup{
		ID: "auto", Name: "自动选择", Type: "urltest",
		ProbeURL: defaultSubscriptionProbeURL, Interval: 300, Tolerance: 50,
	}
	return subscriptionTemplateCustomization{
		Version:      subscriptionCustomizationVersion,
		MixedPort:    defaultSubscriptionMixedPort,
		MainGroup:    mainGroup.ID,
		PolicyGroups: []subscriptionPolicyGroup{mainGroup, autoGroup},
		Final:        subscriptionGroupTarget(mainGroup.ID),
		RuleSets:     []subscriptionRuleSetCustomization{},
	}
}

func normalizeSubscriptionCustomization(renderer string, raw json.RawMessage) (subscriptionTemplateCustomization, json.RawMessage, error) {
	customization := defaultSubscriptionCustomization(renderer)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("{}")) {
		var envelope struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("配置不是受支持的 JSON 结构: %w", err)
		}
		if envelope.Version <= 1 {
			legacy, err := decodeLegacySubscriptionCustomization(trimmed)
			if err != nil {
				return subscriptionTemplateCustomization{}, nil, err
			}
			legacyProfile := strings.ToLower(strings.TrimSpace(legacy.Profile))
			if legacyProfile != "" && legacyProfile != "minimal" && legacyProfile != "balanced" {
				return subscriptionTemplateCustomization{}, nil, fmt.Errorf("旧配置 profile %q 不受支持", legacy.Profile)
			}
			customization = migrateLegacySubscriptionCustomization(renderer, legacy)
		} else {
			var decoded subscriptionTemplateCustomization
			decoder := json.NewDecoder(bytes.NewReader(trimmed))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&decoded); err != nil {
				return subscriptionTemplateCustomization{}, nil, fmt.Errorf("配置不是受支持的 JSON 结构: %w", err)
			}
			if err := ensureJSONEOF(decoder); err != nil {
				return subscriptionTemplateCustomization{}, nil, err
			}
			customization = decoded
		}
	}
	if customization.Version == 0 {
		customization.Version = subscriptionCustomizationVersion
	}
	if customization.Version != subscriptionCustomizationVersion {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("不支持配置版本 %d", customization.Version)
	}
	if customization.MixedPort == 0 {
		customization.MixedPort = defaultSubscriptionMixedPort
	}
	if customization.MixedPort < 1 || customization.MixedPort > 65535 {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("本地混合入站端口必须在 1 到 65535 之间")
	}
	if err := normalizeSubscriptionPolicyGroups(renderer, &customization); err != nil {
		return subscriptionTemplateCustomization{}, nil, err
	}
	customization.Final = normalizeSubscriptionTarget(customization.Final, customization.MainGroup, false)
	if err := validateSubscriptionTarget(customization.Final, customization.PolicyGroups, false); err != nil {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("最终路由: %w", err)
	}
	if len(customization.RuleSets) > maxSubscriptionRuleSets {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("规则集最多允许 %d 个", maxSubscriptionRuleSets)
	}
	seenTags := make(map[string]struct{}, len(customization.RuleSets))
	seenReferences := make(map[uint]struct{}, len(customization.RuleSets))
	for index := range customization.RuleSets {
		ruleSet := &customization.RuleSets[index]
		ruleSet.Tag = strings.TrimSpace(ruleSet.Tag)
		ruleSet.URL = strings.TrimSpace(ruleSet.URL)
		ruleSet.Behavior = strings.ToLower(strings.TrimSpace(ruleSet.Behavior))
		ruleSet.Format = strings.ToLower(strings.TrimSpace(ruleSet.Format))
		ruleSet.Target = normalizeSubscriptionTarget(ruleSet.Target, customization.MainGroup, true)
		if err := validateSubscriptionTarget(ruleSet.Target, customization.PolicyGroups, true); err != nil {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("第 %d 个规则集的出站目标: %w", index+1, err)
		}
		if ruleSet.RuleSetID != 0 {
			if _, exists := seenReferences[ruleSet.RuleSetID]; exists {
				return subscriptionTemplateCustomization{}, nil, fmt.Errorf("第 %d 个规则集引用重复", index+1)
			}
			seenReferences[ruleSet.RuleSetID] = struct{}{}
			if ruleSet.Tag != "" || ruleSet.URL != "" || ruleSet.Behavior != "" || ruleSet.Format != "" || ruleSet.Interval != 0 {
				return subscriptionTemplateCustomization{}, nil, fmt.Errorf("第 %d 个规则集引用不能覆盖统一维护的来源字段", index+1)
			}
			continue
		}
		if !subscriptionRuleSetTagPattern.MatchString(ruleSet.Tag) {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("第 %d 个规则集标识仅允许字母、数字、点、下划线和连字符", index+1)
		}
		normalizedTag := strings.ToLower(ruleSet.Tag)
		if _, exists := seenTags[normalizedTag]; exists {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("规则集标识 %q 重复", ruleSet.Tag)
		}
		seenTags[normalizedTag] = struct{}{}
		if err := validateSubscriptionRuleSetURL(ruleSet.URL); err != nil {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("规则集 %q: %w", ruleSet.Tag, err)
		}
		if ruleSet.Interval == 0 {
			ruleSet.Interval = 86400
		}
		if ruleSet.Interval < 60 || ruleSet.Interval > 604800 {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("规则集 %q 的更新间隔必须在 60 到 604800 秒之间", ruleSet.Tag)
		}
		if err := normalizeRendererRuleSet(renderer, ruleSet); err != nil {
			return subscriptionTemplateCustomization{}, nil, fmt.Errorf("规则集 %q: %w", ruleSet.Tag, err)
		}
	}
	customization.AdvancedSource = strings.TrimSpace(customization.AdvancedSource)
	if len(customization.AdvancedSource) > maxSubscriptionAdvancedSourceBytes {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("高级配置不能超过 %d KiB", maxSubscriptionAdvancedSourceBytes>>10)
	}
	if customization.AdvancedSource != "" {
		if _, err := parseSubscriptionAdvancedOverlay(renderer, customization.AdvancedSource); err != nil {
			return subscriptionTemplateCustomization{}, nil, err
		}
	}
	normalized, err := json.Marshal(customization)
	if err != nil {
		return subscriptionTemplateCustomization{}, nil, fmt.Errorf("编码订阅模板配置: %w", err)
	}
	return customization, normalized, nil
}

func decodeLegacySubscriptionCustomization(raw []byte) (legacySubscriptionTemplateCustomization, error) {
	var legacy legacySubscriptionTemplateCustomization
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return legacy, fmt.Errorf("配置不是受支持的 JSON 结构: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return legacy, err
	}
	return legacy, nil
}

func migrateLegacySubscriptionCustomization(renderer string, legacy legacySubscriptionTemplateCustomization) subscriptionTemplateCustomization {
	groupName := strings.TrimSpace(legacy.GroupName)
	if groupName == "" {
		groupName = "节点选择"
	}
	mainGroup := subscriptionPolicyGroup{ID: "main", Name: groupName, Type: "select"}
	groups := []subscriptionPolicyGroup{mainGroup}
	if strings.EqualFold(strings.TrimSpace(legacy.Profile), "balanced") {
		auto := subscriptionPolicyGroup{
			ID: "auto", Name: "自动选择", Type: "urltest",
			ProbeURL: defaultSubscriptionProbeURL, Interval: 300, Tolerance: 50,
		}
		mainGroup.IncludeGroups = []string{"auto"}
		mainGroup.DefaultGroup = "auto"
		groups[0] = mainGroup
		groups = append(groups, auto)
		if renderer == subscriptionRendererClash {
			groups[0].IncludeGroups = append(groups[0].IncludeGroups, "failover")
			groups = append(groups, subscriptionPolicyGroup{ID: "failover", Name: "故障转移", Type: "fallback"})
		}
	}
	ruleSets := make([]subscriptionRuleSetCustomization, 0, len(legacy.RuleSets))
	for _, item := range legacy.RuleSets {
		ruleSets = append(ruleSets, subscriptionRuleSetCustomization{
			RuleSetID: item.RuleSetID, Tag: item.Tag, URL: item.URL, Behavior: item.Behavior,
			Format: item.Format, Target: legacySubscriptionTarget(item.Action), Interval: item.Interval,
		})
	}
	final := subscriptionTargetDirect
	if strings.ToLower(strings.TrimSpace(legacy.Final)) != subscriptionTargetDirect {
		final = subscriptionGroupTarget(mainGroup.ID)
	}
	return subscriptionTemplateCustomization{
		Version: subscriptionCustomizationVersion, MixedPort: defaultSubscriptionMixedPort, MainGroup: mainGroup.ID,
		PolicyGroups: groups, Final: final, RuleSets: ruleSets, AdvancedSource: legacy.AdvancedSource,
	}
}

func legacySubscriptionTarget(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case subscriptionTargetDirect:
		return subscriptionTargetDirect
	case subscriptionTargetReject:
		return subscriptionTargetReject
	default:
		return subscriptionGroupTarget("main")
	}
}

func subscriptionGroupTarget(groupID string) string {
	return subscriptionTargetGroupPrefix + strings.TrimSpace(groupID)
}

func normalizeSubscriptionTarget(target, mainGroup string, allowReject bool) string {
	target = strings.ToLower(strings.TrimSpace(target))
	switch target {
	case "", "proxy":
		return subscriptionGroupTarget(mainGroup)
	case subscriptionTargetDirect:
		return subscriptionTargetDirect
	case subscriptionTargetReject:
		if allowReject {
			return subscriptionTargetReject
		}
		return target
	default:
		if strings.HasPrefix(target, subscriptionTargetGroupPrefix) {
			return target
		}
		return subscriptionGroupTarget(target)
	}
}

func validateSubscriptionTarget(target string, groups []subscriptionPolicyGroup, allowReject bool) error {
	if target == subscriptionTargetDirect {
		return nil
	}
	if target == subscriptionTargetReject {
		if allowReject {
			return nil
		}
		return errors.New("最终路由不能使用拦截")
	}
	if !strings.HasPrefix(target, subscriptionTargetGroupPrefix) {
		return errors.New("请选择策略组、直连或拦截")
	}
	groupID := strings.TrimPrefix(target, subscriptionTargetGroupPrefix)
	for _, group := range groups {
		if group.ID == groupID {
			return nil
		}
	}
	return fmt.Errorf("策略组 %q 不存在", groupID)
}

func normalizeSubscriptionPolicyGroups(renderer string, customization *subscriptionTemplateCustomization) error {
	if len(customization.PolicyGroups) == 0 {
		return errors.New("至少需要一个策略组")
	}
	if len(customization.PolicyGroups) > maxSubscriptionPolicyGroups {
		return fmt.Errorf("策略组最多允许 %d 个", maxSubscriptionPolicyGroups)
	}
	groupIDs := make(map[string]struct{}, len(customization.PolicyGroups))
	groupNames := make(map[string]struct{}, len(customization.PolicyGroups))
	for index := range customization.PolicyGroups {
		group := &customization.PolicyGroups[index]
		group.ID = strings.ToLower(strings.TrimSpace(group.ID))
		group.Name = strings.TrimSpace(group.Name)
		group.Type = strings.ToLower(strings.TrimSpace(group.Type))
		group.IncludePattern = strings.TrimSpace(group.IncludePattern)
		group.ExcludePattern = strings.TrimSpace(group.ExcludePattern)
		group.ProbeURL = strings.TrimSpace(group.ProbeURL)
		if group.ProbeURL == legacyDefaultSubscriptionProbeURL {
			group.ProbeURL = defaultSubscriptionProbeURL
		}
		group.DefaultGroup = strings.ToLower(strings.TrimSpace(group.DefaultGroup))
		if !subscriptionPolicyGroupIDPattern.MatchString(group.ID) {
			return fmt.Errorf("第 %d 个策略组标识只能使用小写字母、数字和连字符", index+1)
		}
		if _, exists := groupIDs[group.ID]; exists {
			return fmt.Errorf("策略组标识 %q 重复", group.ID)
		}
		groupIDs[group.ID] = struct{}{}
		if group.Name == "" || utf8.RuneCountInString(group.Name) > 64 || strings.ContainsAny(group.Name, "\r\n\t") {
			return fmt.Errorf("第 %d 个策略组名称需包含 1 到 64 个字符且不能包含控制字符", index+1)
		}
		normalizedName := strings.ToLower(group.Name)
		switch normalizedName {
		case "direct", "reject", "block":
			return fmt.Errorf("策略组名称 %q 是系统保留名称", group.Name)
		}
		if renderer == subscriptionRendererClash && strings.Contains(group.Name, ",") {
			return fmt.Errorf("Clash 策略组名称 %q 不能包含逗号", group.Name)
		}
		if _, exists := groupNames[normalizedName]; exists {
			return fmt.Errorf("策略组名称 %q 重复", group.Name)
		}
		groupNames[normalizedName] = struct{}{}
		if !subscriptionPolicyGroupTypeSupported(renderer, group.Type) {
			return fmt.Errorf("%s 不支持策略组类型 %q", renderer, group.Type)
		}
		for label, pattern := range map[string]string{"包含": group.IncludePattern, "排除": group.ExcludePattern} {
			if len(pattern) > maxSubscriptionNodePatternBytes {
				return fmt.Errorf("策略组 %q 的%s正则不能超过 %d 字节", group.Name, label, maxSubscriptionNodePatternBytes)
			}
			if pattern != "" {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("策略组 %q 的%s正则无效: %w", group.Name, label, err)
				}
			}
		}
		if group.Interval == 0 {
			group.Interval = 300
		}
		if group.Interval < 60 || group.Interval > 86400 {
			return fmt.Errorf("策略组 %q 的检测间隔需在 60 秒到 24 小时之间", group.Name)
		}
		if group.Tolerance < 0 || group.Tolerance > 10000 {
			return fmt.Errorf("策略组 %q 的延迟容差需在 0 到 10000 毫秒之间", group.Name)
		}
		if group.Type == "urltest" || group.Type == "fallback" {
			if group.ProbeURL == "" {
				group.ProbeURL = defaultSubscriptionProbeURL
			}
			if err := validateSubscriptionProbeURL(renderer, group.ProbeURL); err != nil {
				return fmt.Errorf("策略组 %q: %w", group.Name, err)
			}
		} else {
			group.ProbeURL = ""
			group.Tolerance = 0
		}
	}
	customization.MainGroup = strings.ToLower(strings.TrimSpace(customization.MainGroup))
	if _, exists := groupIDs[customization.MainGroup]; !exists {
		return fmt.Errorf("主策略组 %q 不存在", customization.MainGroup)
	}
	for index := range customization.PolicyGroups {
		group := &customization.PolicyGroups[index]
		seenIncludes := make(map[string]struct{}, len(group.IncludeGroups))
		normalizedIncludes := make([]string, 0, len(group.IncludeGroups))
		for _, included := range group.IncludeGroups {
			included = strings.ToLower(strings.TrimSpace(included))
			if included == "" {
				continue
			}
			if included == group.ID {
				return fmt.Errorf("策略组 %q 不能包含自身", group.Name)
			}
			if _, exists := groupIDs[included]; !exists {
				return fmt.Errorf("策略组 %q 引用了不存在的策略组 %q", group.Name, included)
			}
			if _, exists := seenIncludes[included]; exists {
				continue
			}
			seenIncludes[included] = struct{}{}
			normalizedIncludes = append(normalizedIncludes, included)
		}
		group.IncludeGroups = normalizedIncludes
		if group.DefaultGroup != "" {
			if group.Type != "select" {
				return fmt.Errorf("只有 select 策略组可以设置默认子策略组")
			}
			if _, exists := seenIncludes[group.DefaultGroup]; !exists {
				return fmt.Errorf("策略组 %q 的默认子策略组必须先加入包含列表", group.Name)
			}
		}
	}
	if err := validateSubscriptionPolicyGroupGraph(customization.PolicyGroups); err != nil {
		return err
	}
	return nil
}

func subscriptionPolicyGroupTypeSupported(renderer, groupType string) bool {
	switch groupType {
	case "select", "urltest":
		return true
	case "fallback":
		return renderer == subscriptionRendererClash || renderer == subscriptionRendererZnetSink
	default:
		return false
	}
}

func validateSubscriptionProbeURL(renderer, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("检测地址必须是完整的 HTTP 或 HTTPS 地址")
	}
	if renderer == subscriptionRendererZnetSink && parsed.Scheme != "http" {
		return errors.New("Zero url_test 当前只支持 HTTP 检测地址")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return errors.New("检测地址不能包含账号、密码或片段")
	}
	return nil
}

func validateSubscriptionPolicyGroupGraph(groups []subscriptionPolicyGroup) error {
	graph := make(map[string][]string, len(groups))
	for _, group := range groups {
		graph[group.ID] = group.IncludeGroups
	}
	state := make(map[string]uint8, len(groups))
	var visit func(string) error
	visit = func(groupID string) error {
		if state[groupID] == 1 {
			return fmt.Errorf("策略组引用形成循环，涉及 %q", groupID)
		}
		if state[groupID] == 2 {
			return nil
		}
		state[groupID] = 1
		for _, child := range graph[groupID] {
			if err := visit(child); err != nil {
				return err
			}
		}
		state[groupID] = 2
		return nil
	}
	for groupID := range graph {
		if err := visit(groupID); err != nil {
			return err
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("配置包含无效尾部内容: %w", err)
	}
	return errors.New("配置只能包含一个 JSON 对象")
}

func validateSubscriptionRuleSetURL(raw string) error {
	if len(raw) > 2048 {
		return errors.New("下载地址不能超过 2048 个字符")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("请输入完整的 HTTP 或 HTTPS 下载地址")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("下载地址仅支持 HTTP 或 HTTPS")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return errors.New("下载地址不能包含账号、密码或片段")
	}
	return nil
}

func normalizeRendererRuleSet(renderer string, ruleSet *subscriptionRuleSetCustomization) error {
	switch renderer {
	case subscriptionRendererZnetSink:
		if ruleSet.Format == "" {
			ruleSet.Format = "domain_list"
		}
		switch ruleSet.Format {
		case "domain_list", "cidr_list", "zero_rule_ir", "zrs":
		default:
			return errors.New("ZNet Sink 规则格式只能是 domain_list、cidr_list、zero_rule_ir 或 zrs")
		}
		ruleSet.Behavior = ""
	case subscriptionRendererClash:
		if ruleSet.Behavior == "" {
			ruleSet.Behavior = "classical"
		}
		if ruleSet.Behavior != "domain" && ruleSet.Behavior != "ipcidr" && ruleSet.Behavior != "classical" {
			return errors.New("Clash behavior 只能是 domain、ipcidr 或 classical")
		}
		if ruleSet.Format == "" {
			ruleSet.Format = "yaml"
		}
		if ruleSet.Format != "yaml" && ruleSet.Format != "text" && ruleSet.Format != "mrs" {
			return errors.New("Clash 规则格式只能是 yaml、text 或 mrs")
		}
		if ruleSet.Format == "mrs" && ruleSet.Behavior == "classical" {
			return errors.New("MRS 格式不支持 classical behavior")
		}
	case subscriptionRendererSingBox:
		if ruleSet.Format == "" {
			ruleSet.Format = "source"
		}
		if ruleSet.Format != "source" && ruleSet.Format != "binary" {
			return errors.New("sing-box 规则格式只能是 source 或 binary")
		}
		ruleSet.Behavior = ""
	default:
		return errors.New("当前输出格式不支持自定义规则集")
	}
	return nil
}

func parseSubscriptionAdvancedOverlay(renderer, source string) (map[string]interface{}, error) {
	var overlay map[string]interface{}
	if renderer == subscriptionRendererClash {
		var raw interface{}
		if err := yaml.Unmarshal([]byte(source), &raw); err != nil {
			return nil, fmt.Errorf("高级 YAML 配置无效: %w", err)
		}
		normalized, err := normalizeYAMLValue(raw)
		if err != nil {
			return nil, fmt.Errorf("高级 YAML 配置无效: %w", err)
		}
		var ok bool
		overlay, ok = normalized.(map[string]interface{})
		if !ok {
			return nil, errors.New("高级 YAML 配置必须是顶层对象")
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(source))
		decoder.UseNumber()
		if err := decoder.Decode(&overlay); err != nil {
			return nil, fmt.Errorf("高级 JSON 配置无效: %w", err)
		}
		if err := ensureJSONEOF(decoder); err != nil {
			return nil, err
		}
		if overlay == nil {
			return nil, errors.New("高级 JSON 配置必须是顶层对象")
		}
	}
	protected := "outbounds"
	if renderer == subscriptionRendererClash {
		protected = "proxies"
	}
	if value, exists := overlay[protected]; exists {
		if err := validateSubscriptionRootInjection(renderer, value); err != nil {
			return nil, err
		}
	}
	return overlay, nil
}

func validateSubscriptionRootInjection(renderer string, value interface{}) error {
	items, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("高级配置字段 %q 必须是数组并保留系统注入标记", subscriptionProtectedRoot(renderer))
	}
	matches := 0
	for _, item := range items {
		if renderer == subscriptionRendererClash {
			if marker, ok := item.(string); ok && marker == subscriptionGeneratedProxiesMarker {
				matches++
			}
			continue
		}
		marker, ok := item.(map[string]interface{})
		if ok && len(marker) == 1 && marker["$zboard"] == subscriptionGeneratedOutboundsMarker {
			matches++
		}
	}
	if matches != 1 {
		if renderer == subscriptionRendererClash {
			return fmt.Errorf("高级配置覆盖 proxies 时必须且只能包含一个 %q 注入标记", subscriptionGeneratedProxiesMarker)
		}
		return errors.New(`高级配置覆盖 outbounds 时必须且只能包含一个 {"$zboard":"generated-outbounds"} 注入标记`)
	}
	return nil
}

func subscriptionProtectedRoot(renderer string) string {
	if renderer == subscriptionRendererClash {
		return "proxies"
	}
	return "outbounds"
}

func normalizeYAMLValue(value interface{}) (interface{}, error) {
	switch current := value.(type) {
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(current))
		for key, item := range current {
			text, ok := key.(string)
			if !ok {
				return nil, errors.New("对象键必须是字符串")
			}
			child, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			normalized[text] = child
		}
		return normalized, nil
	case []interface{}:
		normalized := make([]interface{}, len(current))
		for index, item := range current {
			child, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			normalized[index] = child
		}
		return normalized, nil
	default:
		return value, nil
	}
}

func applySubscriptionAdvancedOverlay(renderer string, target map[string]interface{}, source string, nodeTags []string) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	overlay, err := parseSubscriptionAdvancedOverlay(renderer, source)
	if err != nil {
		return err
	}
	protected := subscriptionProtectedRoot(renderer)
	if value, exists := overlay[protected]; exists {
		expanded, err := expandSubscriptionRootInjection(renderer, value, target[protected])
		if err != nil {
			return err
		}
		overlay[protected] = expanded
	}
	deepMergeSubscriptionMap(target, overlay)
	expandSubscriptionNodeMarkers(target, nodeTags)
	return nil
}

func expandSubscriptionRootInjection(renderer string, value, generated interface{}) ([]interface{}, error) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("高级配置字段 %q 必须是数组", subscriptionProtectedRoot(renderer))
	}
	generatedItems, ok := generated.([]interface{})
	if !ok {
		payload, err := json.Marshal(generated)
		if err != nil {
			return nil, fmt.Errorf("编码系统注入内容: %w", err)
		}
		if err := json.Unmarshal(payload, &generatedItems); err != nil {
			return nil, fmt.Errorf("解析系统注入内容: %w", err)
		}
	}
	expanded := make([]interface{}, 0, len(items)+len(generatedItems))
	for _, item := range items {
		isMarker := false
		if renderer == subscriptionRendererClash {
			marker, ok := item.(string)
			isMarker = ok && marker == subscriptionGeneratedProxiesMarker
		} else if marker, ok := item.(map[string]interface{}); ok {
			isMarker = len(marker) == 1 && marker["$zboard"] == subscriptionGeneratedOutboundsMarker
		}
		if isMarker {
			expanded = append(expanded, generatedItems...)
			continue
		}
		expanded = append(expanded, item)
	}
	return expanded, nil
}

func expandSubscriptionNodeMarkers(value interface{}, nodeTags []string) {
	switch current := value.(type) {
	case map[string]interface{}:
		for key, child := range current {
			if list, ok := child.([]interface{}); ok {
				current[key] = expandSubscriptionNodeMarkerList(list, nodeTags)
				expandSubscriptionNodeMarkers(current[key], nodeTags)
				continue
			}
			expandSubscriptionNodeMarkers(child, nodeTags)
		}
	case []interface{}:
		for _, child := range current {
			expandSubscriptionNodeMarkers(child, nodeTags)
		}
	}
}

func expandSubscriptionNodeMarkerList(items []interface{}, nodeTags []string) []interface{} {
	expanded := make([]interface{}, 0, len(items)+len(nodeTags))
	for _, item := range items {
		if marker, ok := item.(string); ok && marker == subscriptionAllNodesMarker {
			for _, tag := range nodeTags {
				expanded = append(expanded, tag)
			}
			continue
		}
		expanded = append(expanded, item)
	}
	return expanded
}

func deepMergeSubscriptionMap(target, overlay map[string]interface{}) {
	for key, value := range overlay {
		overlayMap, overlayIsMap := value.(map[string]interface{})
		targetMap, targetIsMap := target[key].(map[string]interface{})
		if overlayIsMap && targetIsMap {
			deepMergeSubscriptionMap(targetMap, overlayMap)
			continue
		}
		target[key] = value
	}
}

func subscriptionRuleSetCachePath(renderer string, ruleSet subscriptionRuleSetCustomization) string {
	extension := ruleSet.Format
	switch ruleSet.Format {
	case "domain_list", "cidr_list":
		extension = "txt"
	case "zero_rule_ir", "source":
		extension = "json"
	case "binary":
		extension = "srs"
	}
	if renderer == subscriptionRendererClash {
		return path.Join("./rules", ruleSet.Tag+"."+extension)
	}
	return path.Join("rules", ruleSet.Tag+"."+extension)
}
