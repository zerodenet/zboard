package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type subscriptionRuleSetWriteReq struct {
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Tag              string  `json:"tag"`
	SourceURL        string  `json:"source_url"`
	SourceFormat     string  `json:"source_format"`
	Content          *string `json:"content"`
	SyncInterval     int     `json:"sync_interval"`
	IsActive         *bool   `json:"is_active"`
	ExpectedRevision *uint64 `json:"expected_revision"`

	// Compatibility fields accepted from the previous remote-provider form.
	Renderer string `json:"renderer"`
	URL      string `json:"url"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	Interval int    `json:"interval"`
}

// Preparation returns the validated canonical document so persistence does not
// repeat parsing, normalization and sorting for large inline rule sets.
func prepareSubscriptionRuleSet(req *subscriptionRuleSetWriteReq) (*managedRuleDocument, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Tag = strings.TrimSpace(req.Tag)
	req.SourceURL = strings.TrimSpace(req.SourceURL)
	if req.SourceURL == "" {
		req.SourceURL = strings.TrimSpace(req.URL)
	}
	req.SourceFormat = strings.ToLower(strings.TrimSpace(req.SourceFormat))
	if req.SourceFormat == "" {
		req.SourceFormat = inferManagedRuleSourceFormat(req)
	}
	format, formatErr := normalizeManagedRuleSourceFormat(req.SourceFormat)
	if formatErr == nil {
		req.SourceFormat = format
	}
	if req.SyncInterval == 0 {
		req.SyncInterval = req.Interval
	}
	if req.SyncInterval == 0 {
		req.SyncInterval = 86400
	}

	fields := map[string]string{}
	if req.Name == "" {
		fields["name"] = "请输入规则集名称。"
	} else if utf8.RuneCountInString(req.Name) > 80 {
		fields["name"] = "规则集名称不能超过 80 个字符。"
	}
	if utf8.RuneCountInString(req.Description) > 255 {
		fields["description"] = "规则集说明不能超过 255 个字符。"
	}
	if !subscriptionRuleSetTagPattern.MatchString(req.Tag) {
		fields["tag"] = "规则集标识仅允许字母、数字、点、下划线和连字符。"
	}
	if formatErr != nil {
		fields["source_format"] = formatErr.Error()
	}
	if req.SyncInterval < 60 || req.SyncInterval > 604800 {
		fields["sync_interval"] = "同步间隔必须在 60 秒到 7 天之间。"
	}
	if req.Content == nil && req.SourceURL == "" {
		fields["content"] = "请输入规则内容或远端来源地址。"
	}
	if req.SourceURL != "" {
		if _, err := validateManagedRuleImportURL(req.SourceURL); err != nil {
			fields["source_url"] = err.Error()
		}
	}
	var document *managedRuleDocument
	if req.Content != nil {
		if parsed, err := parseManagedRuleSource([]byte(*req.Content), managedRuleSourceZeroRuleIR); err != nil {
			fields["content"] = err.Error()
		} else {
			document = &parsed
		}
	}
	if len(fields) > 0 {
		return nil, validationError("规则集信息校验失败。", fields)
	}
	return document, nil
}

func inferManagedRuleSourceFormat(req *subscriptionRuleSetWriteReq) string {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	behavior := strings.ToLower(strings.TrimSpace(req.Behavior))
	switch format {
	case managedRuleSourceDomainList, managedRuleSourceCIDRList:
		return format
	case "text", "yaml":
		if behavior == "ipcidr" {
			return managedRuleSourceCIDRList
		}
		if behavior == "classical" {
			return managedRuleSourceClashClassical
		}
		return managedRuleSourceDomainList
	default:
		return managedRuleSourceZeroRuleIR
	}
}

func (h *handlers) AdminSubscriptionRuleSetListHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := entitlements.SubscriptionRuleSetQuery{Offset: offset, Limit: limit}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		query.Keyword = keyword
	}
	if renderer := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("renderer"))); renderer != "" {
		if renderer != managedRuleSetRenderer {
			if _, ok := subscriptionRenderer(renderer); !ok {
				BadRequest(w, "renderer is unsupported")
				return
			}
			query.Renderers = []string{renderer, managedRuleSetRenderer}
			if renderer == subscriptionRendererZNetSink || renderer == "zero" {
				query.ExcludeFormat = managedRuleSetFormatClient
			}
		} else {
			query.Renderers = []string{renderer}
		}
	}
	if activeValue := strings.TrimSpace(r.URL.Query().Get("active")); activeValue != "" {
		active, err := strconv.ParseBool(activeValue)
		if err != nil {
			BadRequest(w, "active must be true or false")
			return
		}
		query.Active = &active
	}
	if idValue := strings.TrimSpace(r.URL.Query().Get("id")); idValue != "" {
		id, err := strconv.ParseUint(idValue, 10, 64)
		if err != nil || id == 0 {
			BadRequest(w, "id must be a positive integer")
			return
		}
		query.ID = uint(id)
	}
	if idsValue := strings.TrimSpace(r.URL.Query().Get("ids")); idsValue != "" {
		parts := strings.Split(idsValue, ",")
		if len(parts) > maxSubscriptionRuleSets {
			BadRequest(w, "ids contains too many values")
			return
		}
		ids := make([]uint, 0, len(parts))
		for _, part := range parts {
			id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
			if err != nil || id == 0 {
				BadRequest(w, "ids must contain positive integers")
				return
			}
			ids = append(ids, uint(id))
		}
		query.IDs = ids
	}
	page, err := h.subscriptionRuleSets().List(r.Context(), claims.UserID, query)
	if errors.Is(err, entitlements.ErrAdministrativeRead) {
		Forbidden(w, "管理员权限已失效。")
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	presented := make([]managedRuleSetPresentation, 0, len(page.Items))
	for _, item := range page.Items {
		presented = append(presented, h.presentManagedRuleSetAt(subscriptionRuleSetModel(item), page.SiteURL))
	}
	OK(w, pagedData(presented, page.Total, offset, limit))
}

func (h *handlers) AdminSubscriptionRuleSetGetHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	item, siteURL, err := h.subscriptionRuleSets().Get(r.Context(), claims.UserID, id)
	if errors.Is(err, entitlements.ErrAdministrativeRead) {
		Forbidden(w, "管理员权限已失效。")
		return
	}
	if errors.Is(err, entitlements.ErrRuleSetNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, h.presentManagedRuleSetAt(subscriptionRuleSetModel(item), siteURL))
}

func (h *handlers) AdminSubscriptionRuleSetCreateHandler(w http.ResponseWriter, r *http.Request) {
	h.saveSubscriptionRuleSet(w, r, 0)
}

func (h *handlers) AdminSubscriptionRuleSetUpdateHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.saveSubscriptionRuleSet(w, r, id)
}

func (h *handlers) saveSubscriptionRuleSet(w http.ResponseWriter, r *http.Request, id uint) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	var req subscriptionRuleSetWriteReq
	if err := decodeBody(r, &req); err != nil {
		BadRequest(w, err.Error())
		return
	}
	document, err := prepareSubscriptionRuleSet(&req)
	if err != nil {
		BadRequestError(w, err)
		return
	}

	var normalized []byte
	if document != nil {
		normalized = encodeManagedCanonicalSource(*document)
	} else if id == 0 {
		raw, err := fetchManagedRuleSource(r.Context(), req.SourceURL)
		if err != nil {
			BadRequestFields(w, "远端规则导入失败。", map[string]string{"source_url": err.Error()})
			return
		}
		document, err := parseManagedRuleSource(raw, req.SourceFormat)
		if err != nil {
			BadRequestFields(w, "远端规则导入失败。", map[string]string{"source_format": err.Error()})
			return
		}
		normalized = encodeManagedCanonicalSource(document)
	}

	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	item := model.SubscriptionRuleSet{
		ID: id, Name: req.Name, Description: req.Description,
		Renderer: managedRuleSetRenderer, Tag: req.Tag, URL: req.SourceURL,
		Behavior: req.SourceFormat, Format: managedRuleSetFormatCanonical,
		Interval: req.SyncInterval, IsActive: active, Revision: 1,
	}
	if normalized != nil {
		item.Format, err = managedRuleStorageFormat(normalized)
		if err != nil {
			BadRequestFields(w, "规则内容无效。", map[string]string{"content": err.Error()})
			return
		}
	}
	saved, currentRevision, err := h.subscriptionRuleSets().Save(r.Context(), claims.UserID, subscriptionRuleSetCapability(item), req.ExpectedRevision, normalized, normalized != nil)
	if err != nil {
		switch {
		case errors.Is(err, entitlements.ErrAdministrativeRead):
			Forbidden(w, "管理员权限已失效。")
		case errors.Is(err, entitlements.ErrRuleSetNotFound):
			NotFound(w)
		case errors.Is(err, entitlements.ErrRuleSetConflict):
			writeJSON(w, http.StatusConflict, "规则集已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
		case errors.Is(err, entitlements.ErrRuleSetClientCompatibility):
			BadRequestFields(w, "规则集与已有模板不兼容。", map[string]string{"content": err.Error()})
		case errors.Is(err, entitlements.ErrRuleSetTagImmutable):
			BadRequestFields(w, "规则集信息校验失败。", map[string]string{"tag": "规则集标识用于公开地址，创建后不能修改。"})
		case errors.Is(err, entitlements.ErrRuleSetLegacyReadOnly):
			BadRequest(w, "旧版外部规则声明仅保留兼容读取，请新建自有规则集后替换模板引用。")
		case isDuplicateError(err):
			BadRequestFields(w, "规则集信息校验失败。", map[string]string{"tag": "该规则集标识已存在。"})
		default:
			ServerError(w, err)
		}
		return
	}
	record, siteURL, err := h.subscriptionRuleSets().Get(r.Context(), claims.UserID, saved.ID)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, h.presentManagedRuleSetAt(subscriptionRuleSetModel(record), siteURL))
}

var (
	errSubscriptionRuleSetRevisionConflict = entitlements.ErrRuleSetConflict
	errSubscriptionRuleSetTagImmutable     = entitlements.ErrRuleSetTagImmutable
	errSubscriptionRuleSetLegacyReadOnly   = entitlements.ErrRuleSetLegacyReadOnly
)

func (h *handlers) AdminSubscriptionRuleSetDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	deleted, err := h.subscriptionRuleSets().Delete(r.Context(), claims.UserID, id)
	item := subscriptionRuleSetModel(deleted)
	if err != nil {
		switch {
		case errors.Is(err, entitlements.ErrAdministrativeRead):
			Forbidden(w, "管理员权限已失效。")
		case errors.Is(err, entitlements.ErrRuleSetNotFound):
			NotFound(w)
		case errors.Is(err, entitlements.ErrRuleSetInUse):
			writeJSON(w, http.StatusConflict, "该规则集仍被订阅模板引用，请先从模板中移除。", map[string]interface{}{"usage_count": item.UsageCount})
		default:
			ServerError(w, err)
		}
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}

var errSubscriptionRuleSetInUse = entitlements.ErrRuleSetInUse

func resolveSubscriptionCustomizationWithRecordsAt(
	renderer string,
	raw json.RawMessage,
	records map[uint]model.SubscriptionRuleSet,
	siteURL string,
	requireActive bool,
) (json.RawMessage, error) {
	customization, normalized, err := normalizeSubscriptionCustomization(renderer, raw)
	if err != nil {
		return nil, err
	}
	hasReferences := false
	for index, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID == 0 {
			continue
		}
		hasReferences = true
		record, ok := records[ruleSet.RuleSetID]
		if !ok {
			return nil, fmt.Errorf("第 %d 个规则集引用不存在或已删除", index+1)
		}
		if requireActive && !record.IsActive {
			return nil, fmt.Errorf("规则集 %q 已停用，不能新增到模板", record.Name)
		}
		if record.Renderer == managedRuleSetRenderer {
			resolved, err := managedRuleCustomizationForRenderer(renderer, siteURL, record, ruleSet.Target)
			if err != nil {
				return nil, err
			}
			customization.RuleSets[index] = resolved
			continue
		}
		// Existing external provider declarations remain valid until administrators replace them.
		if record.Renderer != renderer {
			return nil, fmt.Errorf("规则集 %q 仅适用于 %s", record.Name, record.Renderer)
		}
		customization.RuleSets[index] = subscriptionRuleSetCustomization{
			Tag: record.Tag, URL: record.URL, Behavior: record.Behavior,
			Format: record.Format, Target: ruleSet.Target, Interval: record.Interval,
		}
	}
	if !hasReferences {
		return normalized, nil
	}
	resolved, err := json.Marshal(customization)
	if err != nil {
		return nil, fmt.Errorf("编码已解析规则集: %w", err)
	}
	_, normalizedResolved, err := normalizeSubscriptionCustomization(renderer, resolved)
	return normalizedResolved, err
}

func managedRuleCustomizationForRenderer(renderer, siteURL string, record model.SubscriptionRuleSet, target string) (subscriptionRuleSetCustomization, error) {
	resolved := subscriptionRuleSetCustomization{Tag: record.Tag, Target: target, Interval: record.Interval}
	switch renderer {
	case subscriptionRendererZNetSink:
		if record.Format == managedRuleSetFormatClient {
			return subscriptionRuleSetCustomization{}, fmt.Errorf("规则集 %q：%w", record.Name, errManagedRuleClientCompatibility)
		}
		resolved.URL = managedRuleZRSURL(siteURL, record.Tag)
		resolved.Format = managedRuleArtifactZRS
	case subscriptionRendererClash:
		resolved.URL = managedRulePublicURL(siteURL, record.Tag, managedRuleArtifactClashClassicalYAML)
		resolved.Behavior = "classical"
		resolved.Format = "yaml"
	case subscriptionRendererSingBox:
		resolved.URL = managedRulePublicURL(siteURL, record.Tag, managedRuleArtifactSingBoxSource)
		resolved.Format = "source"
	default:
		return subscriptionRuleSetCustomization{}, fmt.Errorf("规则集 %q 不支持订阅输出格式 %s", record.Name, renderer)
	}
	return resolved, nil
}

// Kept for focused unit tests and compatibility with existing callers.
func resolveSubscriptionCustomizationWithRecords(renderer string, raw json.RawMessage, records map[uint]model.SubscriptionRuleSet, requireActive bool) (json.RawMessage, error) {
	return resolveSubscriptionCustomizationWithRecordsAt(renderer, raw, records, "https://panel.example.com", requireActive)
}

func resolveSubscriptionCustomization(db *gorm.DB, renderer string, raw json.RawMessage, requireActive bool) (json.RawMessage, error) {
	customization, _, err := normalizeSubscriptionCustomization(renderer, raw)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(customization.RuleSets))
	for _, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID != 0 {
			ids = append(ids, ruleSet.RuleSetID)
		}
	}
	records := make(map[uint]model.SubscriptionRuleSet, len(ids))
	if len(ids) > 0 {
		items := make([]model.SubscriptionRuleSet, 0, len(ids))
		if err := db.Where("id IN ?", ids).Find(&items).Error; err != nil {
			return nil, err
		}
		for _, item := range items {
			records[item.ID] = item
		}
	}
	var installation model.Installation
	if err := db.Select("site_url").First(&installation, 1).Error; err != nil {
		return nil, fmt.Errorf("读取站点公开地址: %w", err)
	}
	return resolveSubscriptionCustomizationWithRecordsAt(renderer, raw, records, installation.SiteURL, requireActive)
}

func (h *handlers) resolveSubscriptionCustomization(ctx context.Context, renderer string, raw json.RawMessage, requireActive bool) (json.RawMessage, error) {
	customization, _, err := normalizeSubscriptionCustomization(renderer, raw)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(customization.RuleSets))
	for _, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID != 0 {
			ids = append(ids, ruleSet.RuleSetID)
		}
	}
	items, siteURL, err := h.subscriptionRuleSets().Resolve(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取订阅规则集: %w", err)
	}
	records := make(map[uint]model.SubscriptionRuleSet, len(items))
	for _, item := range items {
		record := subscriptionRuleSetModel(item)
		records[record.ID] = record
	}
	return resolveSubscriptionCustomizationWithRecordsAt(renderer, raw, records, siteURL, requireActive)
}

func subscriptionTemplateRuleSetBindings(customizationRaw json.RawMessage) ([]entitlements.SubscriptionTemplateBinding, error) {
	var customization subscriptionTemplateCustomization
	if err := json.Unmarshal(customizationRaw, &customization); err != nil {
		return nil, err
	}
	bindings := make([]entitlements.SubscriptionTemplateBinding, 0, len(customization.RuleSets))
	for position, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID == 0 {
			continue
		}
		bindings = append(bindings, entitlements.SubscriptionTemplateBinding{
			RuleSetID: ruleSet.RuleSetID,
			Action:    ruleSet.Target,
			Position:  position,
		})
	}
	return bindings, nil
}

func renderSubscriptionWithStoredRuleSets(
	db *gorm.DB,
	renderer string,
	customizationRaw json.RawMessage,
	data subscriptionTemplateData,
	requireActive bool,
) (string, string, error) {
	resolved, err := resolveSubscriptionCustomization(db, renderer, customizationRaw, requireActive)
	if err != nil {
		return "", "", err
	}
	return renderSubscriptionWithRenderer(renderer, resolved, data)
}

func (h *handlers) renderSubscriptionWithStoredRuleSets(ctx context.Context, renderer string, raw json.RawMessage, data subscriptionTemplateData, requireActive bool) (string, string, error) {
	resolved, err := h.resolveSubscriptionCustomization(ctx, renderer, raw, requireActive)
	if err != nil {
		return "", "", err
	}
	return renderSubscriptionWithRenderer(renderer, resolved, data)
}
