package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
	ID              uint                   `json:"id"`
	NodeID          uint                   `json:"node_id"`
	SubscriptionID  uint                   `json:"subscription_id,omitempty"`
	CredentialID    string                 `json:"credential_id,omitempty"`
	Name            string                 `json:"name"`
	Region          string                 `json:"region"`
	Address         string                 `json:"address"`
	Port            int                    `json:"port"`
	PublicPort      int                    `json:"public_port"`
	Protocol        string                 `json:"protocol"`
	MultiplierMilli int64                  `json:"multiplier_milli"`
	Config          map[string]interface{} `json:"config"`
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

func subscriptionRenderer(renderer string) (subscriptionRendererDefinition, bool) {
	definition, ok := subscriptionRendererDefinitions[strings.ToLower(strings.TrimSpace(renderer))]
	return definition, ok
}

func renderSubscriptionWithRenderer(renderer string, customizationRaw json.RawMessage, data subscriptionTemplateData) (string, string, error) {
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

func normalizeSubscriptionTemplateRequest(req *subscriptionTemplateWriteReq) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Description = strings.TrimSpace(req.Description)
	req.Renderer = strings.ToLower(strings.TrimSpace(req.Renderer))
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

func (h *handlers) validateSubscriptionTemplateWithRuleSets(db *gorm.DB, req *subscriptionTemplateWriteReq) error {
	if err := normalizeSubscriptionTemplateRequest(req); err != nil {
		return err
	}
	resolved, err := resolveSubscriptionCustomization(db, req.Renderer, req.Customization, true)
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
		item.ContentType = definition.contentType
	} else {
		item.ContentType = ""
	}
}

func presentSubscriptionTemplates(items []model.SubscriptionTemplate) {
	for index := range items {
		presentSubscriptionTemplate(&items[index])
	}
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
		query := h.db.Model(&model.SubscriptionTemplate{}).Where("is_active = ?", true)
		if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
			if len(keyword) > 100 {
				BadRequest(w, "search keyword is too long")
				return
			}
			pattern := "%" + keyword + "%"
			query = query.Where("name LIKE ? OR slug LIKE ? OR description LIKE ?", pattern, pattern, pattern)
		}
		var total int64
		if err := query.Count(&total).Error; err != nil {
			ServerError(w, err)
			return
		}
		items := make([]model.SubscriptionTemplate, 0)
		if err := query.Select("id", "name", "slug", "description", "renderer", "is_active", "sort_order", "revision", "created_at", "updated_at").
			Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
			ServerError(w, err)
			return
		}
		presentSubscriptionTemplates(items)
		OK(w, pagedData(items, total, offset, limit))
		return
	}
	items := make([]model.SubscriptionTemplate, 0)
	if err := h.db.Select("id", "name", "slug", "description", "renderer", "is_active", "sort_order", "revision", "created_at", "updated_at").
		Where("is_active = ?", true).Order("sort_order asc, id asc").Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	presentSubscriptionTemplates(items)
	OK(w, items)
}

func (h *handlers) AdminSubscriptionTemplateListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	if !parseBoolQuery(r.URL.Query().Get("paged")) {
		items := make([]model.SubscriptionTemplate, 0)
		if err := h.db.Select("id", "name", "slug", "description", "renderer", "is_active", "sort_order", "revision", "created_at", "updated_at").
			Order("sort_order asc, id asc").Find(&items).Error; err != nil {
			ServerError(w, err)
			return
		}
		presentSubscriptionTemplates(items)
		OK(w, items)
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.SubscriptionTemplate{})
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if len(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		pattern := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR slug LIKE ? OR description LIKE ?", pattern, pattern, pattern)
	}
	if activeValue := strings.TrimSpace(r.URL.Query().Get("active")); activeValue != "" {
		active, err := strconv.ParseBool(activeValue)
		if err != nil {
			BadRequest(w, "active must be true or false")
			return
		}
		query = query.Where("is_active = ?", active)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.SubscriptionTemplate, 0)
	if err := query.Select("id", "name", "slug", "description", "renderer", "is_active", "sort_order", "revision", "created_at", "updated_at").
		Order("sort_order asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	presentSubscriptionTemplates(items)
	OK(w, pagedData(items, total, offset, limit))
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
	var item model.SubscriptionTemplate
	if err := h.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
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
	req.Renderer = strings.ToLower(strings.TrimSpace(req.Renderer))
	if _, ok := subscriptionRenderer(req.Renderer); !ok {
		BadRequestFields(w, "订阅输出格式预览校验失败。", map[string]string{"renderer": "请选择系统支持的订阅输出格式。"})
		return
	}
	_, normalizedCustomization, err := normalizeSubscriptionCustomization(req.Renderer, req.Customization)
	if err != nil {
		BadRequestFields(w, "订阅输出格式预览校验失败。", map[string]string{"customization": err.Error()})
		return
	}
	resolvedCustomization, err := resolveSubscriptionCustomization(h.db, req.Renderer, normalizedCustomization, true)
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
	content, truncated := truncateTemplatePreview(rendered)
	OK(w, subscriptionTemplatePreview{
		Content: content, ContentType: contentType, Bytes: len(rendered),
		LineCount: strings.Count(rendered, "\n") + 1, Truncated: truncated,
	})
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
	if err := h.validateSubscriptionTemplateWithRuleSets(h.db, &req); err != nil {
		BadRequestError(w, err)
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	item := model.SubscriptionTemplate{
		ID: id, Name: req.Name, Slug: req.Slug, Description: req.Description,
		Renderer: req.Renderer, Customization: req.Customization,
		IsActive: active, SortOrder: req.SortOrder, Revision: 1,
	}
	action := "subscription_template.create"
	if id != 0 {
		action = "subscription_template.update"
	}
	var currentRevision uint64
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if _, err := resolveSubscriptionCustomization(tx, req.Renderer, req.Customization, true); err != nil {
			return validationError("订阅输出格式校验失败。", map[string]string{"customization": err.Error()})
		}
		if id == 0 {
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else {
			var existing model.SubscriptionTemplate
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, id).Error; err != nil {
				return err
			}
			currentRevision = existing.Revision
			if req.ExpectedRevision != nil && existing.Revision != *req.ExpectedRevision {
				return errSubscriptionTemplateRevisionConflict
			}
			item.CreatedAt = existing.CreatedAt
			item.Revision = existing.Revision + 1
			if err := tx.Save(&item).Error; err != nil {
				return err
			}
		}
		if err := syncSubscriptionTemplateRuleSetBindings(tx, item.ID, item.Customization); err != nil {
			return err
		}
		return createAuditLog(tx, claims, action, fmt.Sprintf("subscription_template:%d", item.ID), fmt.Sprintf("slug=%s revision=%d", item.Slug, item.Revision))
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(err, errSubscriptionTemplateRevisionConflict) {
			writeJSON(w, http.StatusConflict, "订阅模板已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
			return
		}
		var validation *requestValidationError
		if errors.As(err, &validation) {
			BadRequestError(w, validation)
			return
		}
		if isDuplicateError(err) {
			BadRequestFields(w, "订阅模板信息校验失败。", map[string]string{"slug": "链接标识已存在，请更换后重试。"})
			return
		}
		ServerError(w, err)
		return
	}
	presentSubscriptionTemplate(&item)
	OK(w, item)
}

var errSubscriptionTemplateRevisionConflict = errors.New("subscription template revision conflict")

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
	var item model.SubscriptionTemplate
	if err := h.db.First(&item, id).Error; err != nil {
		NotFound(w)
		return
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&item).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "subscription_template.delete", fmt.Sprintf("subscription_template:%d", item.ID), "slug="+item.Slug)
	}); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}

func (h *handlers) writeSubscriptionTemplate(ctx context.Context, w http.ResponseWriter, slug string, manifest subscriptionManifest) error {
	var item model.SubscriptionTemplate
	if err := h.db.Where("slug = ? AND is_active = ?", slug, true).First(&item).Error; err != nil {
		return err
	}
	data := subscriptionTemplateData{
		Version: manifest.Version, GeneratedAt: manifest.GeneratedAt, Subscription: manifest.Subscription,
		ProtocolEndpoints: make([]subscriptionTemplateEndpoint, 0, len(manifest.ProtocolEndpoints)),
	}
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err == nil {
		data.SiteName = installation.SiteName
	}
	for _, endpoint := range manifest.ProtocolEndpoints {
		config := make(map[string]interface{})
		if err := json.Unmarshal(endpoint.Config, &config); err != nil {
			return fmt.Errorf("decode endpoint %d client config: %w", endpoint.ID, err)
		}
		data.ProtocolEndpoints = append(data.ProtocolEndpoints, subscriptionTemplateEndpoint{
			ID: endpoint.ID, NodeID: endpoint.NodeID, SubscriptionID: endpoint.SubscriptionID, CredentialID: endpoint.CredentialID,
			Name: endpoint.Name, Region: endpoint.Region, Address: endpoint.Address, Port: endpoint.Port, PublicPort: endpoint.PublicPort,
			Protocol: endpoint.Protocol, MultiplierMilli: endpoint.MultiplierMilli, Config: config,
		})
	}
	rendered, contentType, err := renderSubscriptionWithStoredRuleSets(h.db, item.Renderer, item.Customization, data, false)
	if err != nil {
		return err
	}
	if err := h.validateZeroSubscriptionPreview(ctx, item.Renderer, rendered); err != nil {
		return err
	}
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("X-Zboard-Subscription-Template", item.Slug)
	w.Header().Set("X-Zboard-Subscription-Format", item.Renderer)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(rendered))
	return err
}
