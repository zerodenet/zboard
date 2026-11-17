package handler

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gopkg.in/yaml.v2"
)

type subscriptionTemplatePreviewGroup struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Members []string `json:"members"`
	Default string   `json:"default"`
}

// Projection and rendering use the existing delivery path. Preview never
// provisions a credential, records access, or persists the rendered document.
func (h *handlers) subscriptionPreviewData(ctx context.Context, actor, id uint) (subscriptionTemplateData, string, error) {
	if id == 0 {
		return sampleSubscriptionTemplateData(), "示例节点", nil
	}
	detail, err := h.services.SubscriptionQueries.Detail(ctx, actor, id)
	if err != nil {
		return subscriptionTemplateData{}, "", err
	}
	now := time.Now().UTC()
	invalid := func(message string) (subscriptionTemplateData, string, error) {
		return subscriptionTemplateData{}, "", &entitlements.QueryError{Message: message}
	}
	if detail.Status != "active" || detail.EndedAt != nil || !detail.EndAt.After(now) || detail.FlowUsed >= detail.FlowTotal {
		return invalid("所选订阅当前不可用，请重新选择预览样本。")
	}
	if detail.ActiveCredentialCount > 256 {
		return invalid("所选订阅超过 256 个节点，请使用较小的预览样本。")
	}
	sub := model.Subscription{ID: detail.ID, UserID: detail.UserID, PlanID: detail.PlanID, PlanSKUID: detail.PlanSKUID, NodeGroupID: detail.NodeGroupID, Status: detail.Status, StartAt: detail.StartAt, EndAt: detail.EndAt, FlowTotal: detail.FlowTotal, FlowUsed: detail.FlowUsed, DeviceLimit: detail.DeviceLimit, SpeedLimitMbps: detail.SpeedLimitMbps, FamilyLimit: detail.FamilyLimit}
	nodes, err := h.buildProjectedSubscriptionManifestNodes(ctx, []model.Subscription{sub}, subscriptionProjectionFilter{}, now)
	if err != nil {
		return subscriptionTemplateData{}, "", err
	}
	if len(nodes) == 0 {
		return invalid("所选订阅暂无可导出节点，请选择其他样本。")
	}
	if len(nodes) > 256 {
		return invalid("所选订阅超过 256 个节点，请使用较小的预览样本。")
	}
	data, err := h.subscriptionTemplateData(ctx, subscriptionManifest{Version: "zboard.subscription/v1", GeneratedAt: now.Format(time.RFC3339), Subscription: subscriptionManifestSummary{ExpiresAt: sub.EndAt.Format(time.RFC3339), FlowTotal: sub.FlowTotal, FlowUsed: sub.FlowUsed, FlowRemaining: sub.FlowTotal - sub.FlowUsed}, ProtocolEndpoints: nodes})
	if err != nil {
		return subscriptionTemplateData{}, "", err
	}
	for index := range data.ProtocolEndpoints {
		data.ProtocolEndpoints[index].CredentialID = "preview-credential"
		redactSubscriptionPreviewConfig(data.ProtocolEndpoints[index].Config)
	}
	return data, detail.UserEmail + " · " + detail.PlanName + "（预览凭据）", nil
}

func redactSubscriptionPreviewConfig(config map[string]interface{}) {
	for key, value := range config {
		switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
		case "id", "uuid":
			if _, ok := value.(string); ok {
				config[key] = "00000000-0000-4000-8000-000000000000"
				continue
			}
		case "password", "username", "user", "token", "secret", "authorization", "cookie", "credential_id", "auth", "auth_str", "secret_key", "api_key", "plugin_opts", "obfs_password":
			config[key] = "preview-credential"
			continue
		case "private_key", "preshared_key", "privatekey", "presharedkey", "psk", "key":
			config[key] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			continue
		case "headers":
			if headers, ok := value.(map[string]interface{}); ok {
				for name := range headers {
					if !strings.EqualFold(name, "Host") && !strings.EqualFold(name, "User-Agent") {
						headers[name] = "preview-credential"
					}
				}
				continue
			}
		}
		switch nested := value.(type) {
		case map[string]interface{}:
			redactSubscriptionPreviewConfig(nested)
		case []interface{}:
			for _, item := range nested {
				if row, ok := item.(map[string]interface{}); ok {
					redactSubscriptionPreviewConfig(row)
				}
			}
		}
	}
}

// Read the actual final document, including advanced overrides, rather than
// estimating effects from the editor's intermediate model.
func subscriptionPreviewGroups(renderer, content string) []subscriptionTemplatePreviewGroup {
	type group struct {
		Name      string   `json:"name" yaml:"name"`
		Tag       string   `json:"tag" yaml:"tag"`
		Type      string   `json:"type" yaml:"type"`
		Proxies   []string `json:"proxies" yaml:"proxies"`
		Outbounds []string `json:"outbounds" yaml:"outbounds"`
		Default   string   `json:"default" yaml:"default"`
		Selected  string   `json:"selected" yaml:"selected"`
	}
	var doc struct {
		Clash     []group `yaml:"proxy-groups"`
		Zero      []group `json:"outbound_groups"`
		Outbounds []group `json:"outbounds"`
	}
	groups := []group{}
	switch renderer {
	case subscriptionRendererClash:
		if yaml.Unmarshal([]byte(content), &doc) != nil {
			return []subscriptionTemplatePreviewGroup{}
		}
		groups = doc.Clash
	case subscriptionRendererZnetSink:
		if json.Unmarshal([]byte(content), &doc) != nil {
			return []subscriptionTemplatePreviewGroup{}
		}
		groups = doc.Zero
	case subscriptionRendererSingBox:
		if json.Unmarshal([]byte(content), &doc) != nil {
			return []subscriptionTemplatePreviewGroup{}
		}
		for _, item := range doc.Outbounds {
			if item.Type == "selector" || item.Type == "urltest" || item.Type == "fallback" {
				groups = append(groups, item)
			}
		}
	}
	result := make([]subscriptionTemplatePreviewGroup, 0, len(groups))
	for _, item := range groups {
		name := item.Tag
		members := item.Outbounds
		if renderer == subscriptionRendererClash {
			name = item.Name
			members = item.Proxies
		}
		selected := item.Selected
		if selected == "" {
			selected = item.Default
		}
		if selected == "" && len(members) > 0 && (item.Type == "select" || item.Type == "selector") {
			selected = members[0]
		}
		result = append(result, subscriptionTemplatePreviewGroup{Name: name, Type: item.Type, Members: members, Default: selected})
	}
	return result
}
