package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func (h *handlers) ReconcileSubscriptionTemplateDefaults() error {
	page, err := h.services.SubscriptionTemplates.List(context.Background(), entitlements.SubscriptionTemplateQuery{})
	if err != nil {
		return err
	}
	updates := make([]entitlements.SubscriptionTemplateDefault, 0, len(page.Items))
	for _, template := range page.Items {
		renderer := normalizeSubscriptionRenderer(template.Renderer)
		_, normalized, err := normalizeSubscriptionCustomization(renderer, template.Customization)
		if err != nil {
			return fmt.Errorf("normalize subscription template %d: %w", template.ID, err)
		}
		if template.Renderer == renderer && bytes.Equal(bytes.TrimSpace(template.Customization), bytes.TrimSpace(normalized)) {
			continue
		}
		updates = append(updates, entitlements.SubscriptionTemplateDefault{ID: template.ID, Renderer: renderer, Customization: normalized})
	}
	if err := h.services.SubscriptionTemplates.ReconcileDefaults(context.Background(), updates); err != nil {
		return fmt.Errorf("persist subscription template defaults: %w", err)
	}
	return nil
}

func subscriptionTemplateModel(row entitlements.SubscriptionTemplate) model.SubscriptionTemplate {
	return model.SubscriptionTemplate{ID: row.ID, Name: row.Name, Slug: row.Slug, Description: row.Description, Renderer: row.Renderer, Customization: row.Customization, IsActive: row.IsActive, SortOrder: row.SortOrder, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

const (
	maxRenderedSubscriptionBytes = 2 << 20
	maxTemplatePreviewBytes      = 256 << 10
	subscriptionRendererZnetSink = "znet-sink"
	subscriptionRendererClash    = "clash"
	subscriptionRendererSingBox  = "sing-box"
)

var subscriptionTemplateSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type subscriptionTemplateWriteReq struct {
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	Description      string          `json:"description"`
	Renderer         string          `json:"renderer"`
	Customization    json.RawMessage `json:"customization"`
	IsActive         *bool           `json:"is_active"`
	SortOrder        int             `json:"sort_order"`
	ExpectedRevision *uint64         `json:"expected_revision"`
}

type subscriptionTemplatePreviewReq struct {
	Renderer      string          `json:"renderer"`
	Customization json.RawMessage `json:"customization"`
}

type subscriptionTemplatePreview struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
	LineCount   int    `json:"line_count"`
	Truncated   bool   `json:"truncated"`
}

type subscriptionManifestSummary struct {
	ExpiresAt     string `json:"expires_at"`
	FlowTotal     int64  `json:"flow_total"`
	FlowUsed      int64  `json:"flow_used"`
	FlowRemaining int64  `json:"flow_remaining"`
}

type subscriptionManifest struct {
	Version           string                      `json:"version"`
	GeneratedAt       string                      `json:"generated_at"`
	Subscription      subscriptionManifestSummary `json:"subscription"`
	ProtocolEndpoints []subscriptionManifestNode  `json:"protocol_endpoints"`
}

type subscriptionTemplateEndpoint struct {
	NetworkEntryID      uint                   `json:"network_entry_id,omitempty"`
	NetworkEntryNetwork string                 `json:"network_entry_network,omitempty"`
	ID                  uint                   `json:"id"`
	NodeID              uint                   `json:"node_id"`
	SubscriptionID      uint                   `json:"subscription_id,omitempty"`
	CredentialID        string                 `json:"credential_id,omitempty"`
	Name                string                 `json:"name"`
	Region              string                 `json:"region"`
	Address             string                 `json:"address"`
	Port                int                    `json:"port"`
	PublicPort          int                    `json:"public_port"`
	Protocol            string                 `json:"protocol"`
	MultiplierMilli     int64                  `json:"multiplier_milli"`
	Config              map[string]interface{} `json:"config"`
}

type subscriptionTemplateData struct {
	SiteName          string                         `json:"site_name"`
	Version           string                         `json:"version"`
	GeneratedAt       string                         `json:"generated_at"`
	Subscription      subscriptionManifestSummary    `json:"subscription"`
	ProtocolEndpoints []subscriptionTemplateEndpoint `json:"protocol_endpoints"`
}

type subscriptionRendererDefinition struct {
	contentType string
	render      func(subscriptionTemplateData, subscriptionTemplateCustomization) (string, error)
}

var subscriptionRendererDefinitions = map[string]subscriptionRendererDefinition{
	subscriptionRendererZnetSink: {contentType: "application/json", render: renderZnetSinkSubscription},
	subscriptionRendererClash:    {contentType: "application/yaml", render: renderClashSubscription},
	subscriptionRendererSingBox:  {contentType: "application/json", render: renderSingBoxSubscription},
}

func normalizeSubscriptionRenderer(renderer string) string {
	normalized := strings.ToLower(strings.TrimSpace(renderer))
	if isZeroSubscriptionAlias(normalized) {
		return subscriptionRendererZnetSink
	}
	switch normalized {
	case "clash-yaml":
		return subscriptionRendererClash
	case "singbox":
		return subscriptionRendererSingBox
	default:
		return normalized
	}
}

func presentSubscriptionRenderer(renderer string) string {
	if normalizeSubscriptionRenderer(renderer) == subscriptionRendererZnetSink {
		return subscriptionDeliveryZero
	}
	return canonicalSubscriptionFormat(renderer)
}

func subscriptionRenderer(renderer string) (subscriptionRendererDefinition, bool) {
	definition, ok := subscriptionRendererDefinitions[normalizeSubscriptionRenderer(renderer)]
	return definition, ok
}

func renderSubscriptionWithRenderer(renderer string, customizationRaw json.RawMessage, data subscriptionTemplateData) (string, string, error) {
	renderer = normalizeSubscriptionRenderer(renderer)
	definition, ok := subscriptionRenderer(renderer)
	if !ok {
		return "", "", fmt.Errorf("unsupported subscription renderer %q", renderer)
	}
	customization, _, err := normalizeSubscriptionCustomization(renderer, customizationRaw)
	if err != nil {
		return "", "", err
	}
	for _, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID != 0 {
			return "", "", fmt.Errorf("规则集引用 %d 尚未解析", ruleSet.RuleSetID)
		}
	}
	rendered, err := definition.render(data, customization)
	if err != nil {
		return "", "", err
	}
	if len(rendered) > maxRenderedSubscriptionBytes {
		return "", "", errors.New("rendered subscription is too large")
	}
	return rendered, definition.contentType, nil
}

func truncateTemplatePreview(content string) (string, bool) {
	if len(content) <= maxTemplatePreviewBytes {
		return content, false
	}
	end := maxTemplatePreviewBytes
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	return content[:end], true
}

func buildSubscriptionTemplatePreview(content, contentType string) subscriptionTemplatePreview {
	preview, truncated := truncateTemplatePreview(content)
	return subscriptionTemplatePreview{
		Content: preview, ContentType: contentType, Bytes: len(content),
		LineCount: strings.Count(content, "\n") + 1, Truncated: truncated,
	}
}

func normalizeSubscriptionTemplateRequest(req *subscriptionTemplateWriteReq) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Description = strings.TrimSpace(req.Description)
	req.Renderer = normalizeSubscriptionRenderer(req.Renderer)
	fields := map[string]string{}
	if req.Name == "" {
		fields["name"] = "请输入模板名称。"
	} else if len(req.Name) > 80 {
		fields["name"] = "模板名称不能超过 80 个字符。"
	}
	if len(req.Description) > 255 {
		fields["description"] = "模板说明不能超过 255 个字符。"
	}
	if !subscriptionTemplateSlugPattern.MatchString(req.Slug) || len(req.Slug) > 80 {
		fields["slug"] = "链接标识只能包含小写字母、数字和单个连字符，且不能超过 80 个字符。"
	} else if req.Slug == subscriptionDeliveryAuto || req.Slug == subscriptionDeliveryNative {
		fields["slug"] = "auto 和 native 为订阅分发保留标识，请使用其他链接标识。"
	}
	if _, ok := subscriptionRenderer(req.Renderer); !ok {
		fields["renderer"] = "请选择系统支持的订阅输出格式。"
	}
	_, normalizedCustomization, customizationErr := normalizeSubscriptionCustomization(req.Renderer, req.Customization)
	if customizationErr != nil {
		fields["customization"] = customizationErr.Error()
	} else {
		req.Customization = normalizedCustomization
	}
	if len(fields) > 0 {
		return validationError("订阅模板信息校验失败。", fields)
	}
	return nil
}

func validateSubscriptionTemplate(req *subscriptionTemplateWriteReq) error {
	if err := normalizeSubscriptionTemplateRequest(req); err != nil {
		return err
	}
	_, _, err := renderSubscriptionWithRenderer(req.Renderer, req.Customization, sampleSubscriptionTemplateData())
	if err != nil {
		return validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()})
	}
	return nil
}

func (h *handlers) validateSubscriptionTemplateWithRuleSets(ctx context.Context, req *subscriptionTemplateWriteReq) error {
	if err := normalizeSubscriptionTemplateRequest(req); err != nil {
		return err
	}
	resolved, err := h.resolveSubscriptionCustomization(ctx, req.Renderer, req.Customization, true)
	if err != nil {
		return validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()})
	}
	rendered, _, err := renderSubscriptionWithRenderer(req.Renderer, resolved, sampleSubscriptionTemplateData())
	if err != nil {
		return validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()})
	}
	if err := h.validateZeroSubscriptionPreview(context.Background(), req.Renderer, rendered); err != nil {
		return validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()})
	}
	return nil
}

func sampleSubscriptionTemplateData() subscriptionTemplateData {
	return subscriptionTemplateData{
		SiteName:    "Zboard",
		Version:     "zboard.subscription/v1",
		GeneratedAt: "2026-01-01T00:00:00Z",
		Subscription: subscriptionManifestSummary{
			ExpiresAt: "2026-02-01T00:00:00Z", FlowTotal: 107374182400, FlowUsed: 1073741824, FlowRemaining: 106300440576,
		},
		ProtocolEndpoints: []subscriptionTemplateEndpoint{
			{
				ID: 1, NodeID: 1, SubscriptionID: 1, CredentialID: "credential-example", Name: "Hong Kong VLESS", Region: "Hong Kong",
				Address: "edge.example.com", Port: 443, PublicPort: 443, Protocol: "vless", MultiplierMilli: 1000,
				Config: map[string]interface{}{"type": "vless", "id": "00000000-0000-4000-8000-000000000000"},
			},
			{
				ID: 2, NodeID: 2, Name: "Singapore Mieru", Region: "Singapore",
				Address: "mieru.example.com", Port: 2999, PublicPort: 2999, Protocol: "mieru", MultiplierMilli: 1000,
				Config: map[string]interface{}{"type": "mieru", "password": "generated-endpoint-secret", "transport": "tcp"},
			},
		},
	}
}

func presentSubscriptionTemplate(item *model.SubscriptionTemplate) {
	if definition, ok := subscriptionRenderer(item.Renderer); ok {
		item.Renderer = presentSubscriptionRenderer(item.Renderer)
		if item.Renderer == subscriptionDeliveryZero {
			item.ContentType = "text/plain"
		} else {
			item.ContentType = definition.contentType
		}
	} else {
		item.ContentType = ""
	}
}

func presentSubscriptionTemplates(items []model.SubscriptionTemplate) {
	for index := range items {
		presentSubscriptionTemplate(&items[index])
	}
}

func subscriptionTemplateModels(items []entitlements.SubscriptionTemplate) []model.SubscriptionTemplate {
	result := make([]model.SubscriptionTemplate, 0, len(items))
	for _, item := range items {
		result = append(result, subscriptionTemplateModel(item))
	}
	return result
}

func (h *handlers) SubscriptionTemplateListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.authFromRequest(r); err != nil {
		Unauthorized(w, err.Error())
		return
	}
	if parseBoolQuery(r.URL.Query().Get("paged")) {
		offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
		if err != nil {
			BadRequest(w, err.Error())
			return
		}
		active := true
		query := entitlements.SubscriptionTemplateQuery{Active: &active, Paged: true, Offset: offset, Limit: limit}
		if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
			if len(keyword) > 100 {
				BadRequest(w, "search keyword is too long")
				return
			}
			query.Search = keyword
		}
		page, err := h.services.SubscriptionTemplates.List(r.Context(), query)
		if err != nil {
			ServerError(w, err)
			return
		}
		items := subscriptionTemplateModels(page.Items)
		presentSubscriptionTemplates(items)
		OK(w, pagedData(items, page.Total, offset, limit))
		return
	}
	active := true
	page, err := h.services.SubscriptionTemplates.List(r.Context(), entitlements.SubscriptionTemplateQuery{Active: &active})
	if err != nil {
		ServerError(w, err)
		return
	}
	items := subscriptionTemplateModels(page.Items)
	presentSubscriptionTemplates(items)
	OK(w, items)
}

func (h *handlers) AdminSubscriptionTemplateListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	if !parseBoolQuery(r.URL.Query().Get("paged")) {
		page, err := h.services.SubscriptionTemplates.List(r.Context(), entitlements.SubscriptionTemplateQuery{})
		if err != nil {
			ServerError(w, err)
			return
		}
		items := subscriptionTemplateModels(page.Items)
		presentSubscriptionTemplates(items)
		OK(w, items)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := entitlements.SubscriptionTemplateQuery{Paged: true, Offset: offset, Limit: limit}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if len(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		query.Search = keyword
	}
	if activeValue := strings.TrimSpace(r.URL.Query().Get("active")); activeValue != "" {
		active, err := strconv.ParseBool(activeValue)
		if err != nil {
			BadRequest(w, "active must be true or false")
			return
		}
		query.Active = &active
	}
	page, err := h.services.SubscriptionTemplates.List(r.Context(), query)
	if err != nil {
		ServerError(w, err)
		return
	}
	items := subscriptionTemplateModels(page.Items)
	presentSubscriptionTemplates(items)
	OK(w, pagedData(items, page.Total, offset, limit))
}

func (h *handlers) AdminSubscriptionTemplateGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-templates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	stored, err := h.services.SubscriptionTemplates.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, entitlements.ErrTemplateNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	item := subscriptionTemplateModel(stored)
	presentSubscriptionTemplate(&item)
	OK(w, item)
}

func (h *handlers) AdminSubscriptionTemplatePreviewHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var req subscriptionTemplatePreviewReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	req.Renderer = normalizeSubscriptionRenderer(req.Renderer)
	if _, ok := subscriptionRenderer(req.Renderer); !ok {
		BadRequestFields(w, "订阅输出格式预览校验失败。", map[string]string{"renderer": "请选择系统支持的订阅输出格式。"})
		return
	}
	_, normalizedCustomization, err := normalizeSubscriptionCustomization(req.Renderer, req.Customization)
	if err != nil {
		BadRequestFields(w, "订阅输出格式预览校验失败。", map[string]string{"customization": err.Error()})
		return
	}
	resolvedCustomization, err := h.resolveSubscriptionCustomization(r.Context(), req.Renderer, normalizedCustomization, true)
	if err != nil {
		BadRequestFields(w, "订阅输出格式预览校验失败。", map[string]string{"customization": err.Error()})
		return
	}
	rendered, contentType, err := renderSubscriptionWithRenderer(req.Renderer, resolvedCustomization, sampleSubscriptionTemplateData())
	if err != nil {
		BadRequestFields(w, "订阅输出格式预览失败。", map[string]string{"customization": err.Error()})
		return
	}
	if err := h.validateZeroSubscriptionPreview(r.Context(), req.Renderer, rendered); err != nil {
		BadRequestFields(w, "订阅输出格式预览失败。", map[string]string{"customization": err.Error()})
		return
	}
	OK(w, buildSubscriptionTemplatePreview(rendered, contentType))
}

func (h *handlers) AdminSubscriptionTemplateCreateHandler(w http.ResponseWriter, r *http.Request) {
	h.saveSubscriptionTemplate(w, r, 0)
}

func (h *handlers) AdminSubscriptionTemplateUpdateHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-templates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.saveSubscriptionTemplate(w, r, id)
}

func (h *handlers) saveSubscriptionTemplate(w http.ResponseWriter, r *http.Request, id uint) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req subscriptionTemplateWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.validateSubscriptionTemplateWithRuleSets(r.Context(), &req); err != nil {
		BadRequestError(w, err)
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	item := entitlements.SubscriptionTemplate{
		ID: id, Name: req.Name, Slug: req.Slug, Description: req.Description,
		Renderer: req.Renderer, Customization: req.Customization,
		IsActive: active, SortOrder: req.SortOrder, Revision: 1,
	}
	bindings, err := subscriptionTemplateRuleSetBindings(item.Customization)
	if err != nil {
		BadRequestError(w, validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()}))
		return
	}
	saved, currentRevision, err := h.services.SubscriptionTemplates.Save(r.Context(), claims.UserID, item, req.ExpectedRevision, bindings)
	if err != nil {
		if errors.Is(err, entitlements.ErrAdministrativeRead) {
			Forbidden(w, "管理员权限已失效。")
			return
		}
		if errors.Is(err, entitlements.ErrTemplateNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(err, entitlements.ErrTemplateConflict) {
			writeJSON(w, http.StatusConflict, "订阅模板已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
			return
		}
		if errors.Is(err, entitlements.ErrTemplateRuleSet) {
			BadRequestFields(w, "订阅输出格式校验失败。", map[string]string{"customization": "引用的规则集不存在或已停用。"})
			return
		}
		if isDuplicateError(err) {
			BadRequestFields(w, "订阅模板信息校验失败。", map[string]string{"slug": "链接标识已存在，请更换后重试。"})
			return
		}
		ServerError(w, err)
		return
	}
	itemModel := subscriptionTemplateModel(saved)
	presentSubscriptionTemplate(&itemModel)
	OK(w, itemModel)
}

var errSubscriptionTemplateRevisionConflict = entitlements.ErrTemplateConflict

func (h *handlers) AdminSubscriptionTemplateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-templates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if err := h.services.SubscriptionTemplates.Delete(r.Context(), claims.UserID, id); err != nil {
		if errors.Is(err, entitlements.ErrAdministrativeRead) {
			Forbidden(w, "管理员权限已失效。")
			return
		}
		if errors.Is(err, entitlements.ErrTemplateNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}

func (h *handlers) writeSubscriptionTemplate(ctx context.Context, w http.ResponseWriter, slug string, manifest subscriptionManifest) error {
	source, err := h.services.SubscriptionTemplates.RenderSource(ctx, slug)
	if err != nil {
		return err
	}
	item := subscriptionTemplateModel(source.Template)
	data, err := h.subscriptionTemplateData(ctx, manifest)
	if err != nil {
		return err
	}
	data.SiteName = source.SiteName
	renderer := normalizeSubscriptionRenderer(item.Renderer)
	rendered, contentType, err := h.renderSubscriptionWithStoredRuleSets(ctx, renderer, item.Customization, data, false)
	if err != nil {
		return err
	}
	if err := h.validateZeroSubscriptionPreview(ctx, renderer, rendered); err != nil {
		return err
	}
	rendered, contentType, deliveryFormat := encodeSubscriptionTemplateDelivery(renderer, rendered, contentType)
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("X-Zboard-Subscription-Template", item.Slug)
	w.Header().Set("X-Zboard-Subscription-Format", deliveryFormat)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(rendered))
	return err
}

func (h *handlers) subscriptionTemplateData(ctx context.Context, manifest subscriptionManifest) (subscriptionTemplateData, error) {
	data := subscriptionTemplateData{
		Version: manifest.Version, GeneratedAt: manifest.GeneratedAt, Subscription: manifest.Subscription,
		ProtocolEndpoints: make([]subscriptionTemplateEndpoint, 0, len(manifest.ProtocolEndpoints)),
	}
	status, err := h.services.Installation.Status(ctx)
	if err == nil {
		data.SiteName = status.SiteName
	}
	for _, endpoint := range manifest.ProtocolEndpoints {
		config := make(map[string]interface{})
		if err := json.Unmarshal(endpoint.Config, &config); err != nil {
			return subscriptionTemplateData{}, fmt.Errorf("decode endpoint %d client config: %w", endpoint.ID, err)
		}
		data.ProtocolEndpoints = append(data.ProtocolEndpoints, subscriptionTemplateEndpoint{
			NetworkEntryID: endpoint.NetworkEntryID, NetworkEntryNetwork: endpoint.NetworkEntryNetwork, ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: endpoint.SubscriptionID, CredentialID: endpoint.CredentialID,
			Name: endpoint.Name, Region: endpoint.Region, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
			Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli, Config: config,
		})
	}
	return data, nil
}

func encodeSubscriptionTemplateDelivery(renderer, rendered, contentType string) (string, string, string) {
	renderer = normalizeSubscriptionRenderer(renderer)
	if renderer != subscriptionRendererZnetSink {
		return rendered, contentType, presentSubscriptionRenderer(renderer)
	}
	return base64.StdEncoding.EncodeToString([]byte(rendered)), "text/plain", subscriptionDeliveryZero
}
