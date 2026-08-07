package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func validateSubscriptionRuleSet(req *subscriptionRuleSetWriteReq) error {
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
	if req.Content != nil {
		if _, err := parseManagedRuleSource([]byte(*req.Content), managedRuleSourceZeroRuleIR); err != nil {
			fields["content"] = err.Error()
		}
	}
	if len(fields) > 0 {
		return validationError("规则集信息校验失败。", fields)
	}
	return nil
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

func subscriptionRuleSetUsageCounts(db *gorm.DB, ids []uint) (map[uint]int64, error) {
	counts := make(map[uint]int64, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	type usageRow struct {
		ID    uint
		Count int64
	}
	rows := make([]usageRow, 0, len(ids))
	err := db.Model(&model.SubscriptionTemplateRuleSetBinding{}).
		Select("subscription_rule_set_id AS id, COUNT(*) AS count").
		Where("subscription_rule_set_id IN ?", ids).
		Group("subscription_rule_set_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.ID] = row.Count
	}
	return counts, nil
}

func presentSubscriptionRuleSetUsage(db *gorm.DB, items []model.SubscriptionRuleSet) error {
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	counts, err := subscriptionRuleSetUsageCounts(db, ids)
	if err != nil {
		return err
	}
	for index := range items {
		items[index].UsageCount = counts[items[index].ID]
	}
	return nil
}

func (h *handlers) AdminSubscriptionRuleSetListHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	offset, limit, err := parsePagination(r.URL.Query().Get("offset"), r.URL.Query().Get("limit"))
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	query := h.db.Model(&model.SubscriptionRuleSet{})
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			BadRequest(w, "search keyword is too long")
			return
		}
		pattern := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR tag LIKE ? OR description LIKE ? OR url LIKE ?", pattern, pattern, pattern, pattern)
	}
	if renderer := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("renderer"))); renderer != "" {
		if renderer != managedRuleSetRenderer {
			if _, ok := subscriptionRenderer(renderer); !ok {
				BadRequest(w, "renderer is unsupported")
				return
			}
			query = query.Where("renderer IN ?", []string{renderer, managedRuleSetRenderer})
		} else {
			query = query.Where("renderer = ?", renderer)
		}
	}
	if activeValue := strings.TrimSpace(r.URL.Query().Get("active")); activeValue != "" {
		active, err := strconv.ParseBool(activeValue)
		if err != nil {
			BadRequest(w, "active must be true or false")
			return
		}
		query = query.Where("is_active = ?", active)
	}
	if idValue := strings.TrimSpace(r.URL.Query().Get("id")); idValue != "" {
		id, err := strconv.ParseUint(idValue, 10, 64)
		if err != nil || id == 0 {
			BadRequest(w, "id must be a positive integer")
			return
		}
		query = query.Where("id = ?", id)
	}
	if idsValue := strings.TrimSpace(r.URL.Query().Get("ids")); idsValue != "" {
		parts := strings.Split(idsValue, ",")
		if len(parts) > maxSubscriptionRuleSets {
			BadRequest(w, "ids contains too many values")
			return
		}
		ids := make([]uint64, 0, len(parts))
		for _, part := range parts {
			id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
			if err != nil || id == 0 {
				BadRequest(w, "ids must contain positive integers")
				return
			}
			ids = append(ids, id)
		}
		query = query.Where("id IN ?", ids)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		ServerError(w, err)
		return
	}
	items := make([]model.SubscriptionRuleSet, 0)
	if err := query.Order("updated_at desc, id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		ServerError(w, err)
		return
	}
	if err := presentSubscriptionRuleSetUsage(h.db, items); err != nil {
		ServerError(w, err)
		return
	}
	presented := make([]managedRuleSetPresentation, 0, len(items))
	for _, item := range items {
		presented = append(presented, h.presentManagedRuleSet(item))
	}
	OK(w, pagedData(presented, total, offset, limit))
}

func (h *handlers) AdminSubscriptionRuleSetGetHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/subscription-rule-sets/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	var item model.SubscriptionRuleSet
	if err := h.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	items := []model.SubscriptionRuleSet{item}
	if err := presentSubscriptionRuleSetUsage(h.db, items); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, h.presentManagedRuleSet(items[0]))
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
	if err := validateSubscriptionRuleSet(&req); err != nil {
		BadRequestError(w, err)
		return
	}

	var normalized []byte
	if req.Content != nil {
		document, _ := parseManagedRuleSource([]byte(*req.Content), managedRuleSourceZeroRuleIR)
		normalized = encodeManagedCanonicalSource(document)
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
	action := "subscription_rule_set.create"
	if id != 0 {
		action = "subscription_rule_set.update"
	}
	var currentRevision uint64
	var previousContent []byte
	var previousContentExists bool
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if id == 0 {
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if err := h.writeManagedRuleSource(item.Tag, normalized); err != nil {
				return err
			}
		} else {
			var existing model.SubscriptionRuleSet
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, id).Error; err != nil {
				return err
			}
			if existing.Renderer != managedRuleSetRenderer {
				return errSubscriptionRuleSetLegacyReadOnly
			}
			currentRevision = existing.Revision
			if req.ExpectedRevision != nil && existing.Revision != *req.ExpectedRevision {
				return errSubscriptionRuleSetRevisionConflict
			}
			if existing.Tag != item.Tag {
				return errSubscriptionRuleSetTagImmutable
			}
			item.CreatedAt = existing.CreatedAt
			item.Revision = existing.Revision + 1
			if normalized != nil {
				if previous, readErr := h.readManagedRuleSource(existing.Tag); readErr == nil {
					previousContent, previousContentExists = previous, true
				}
				if err := h.writeManagedRuleSource(existing.Tag, normalized); err != nil {
					return err
				}
			}
			if err := tx.Save(&item).Error; err != nil {
				return err
			}
		}
		return createAuditLog(tx, claims, action, fmt.Sprintf("subscription_rule_set:%d", item.ID), fmt.Sprintf("managed=true tag=%s revision=%d", item.Tag, item.Revision))
	})
	if err != nil {
		if id == 0 {
			_ = h.removeManagedRuleSetFiles(item.Tag)
		} else if normalized != nil && previousContentExists {
			_ = h.writeManagedRuleSource(item.Tag, previousContent)
		}
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(w)
		case errors.Is(err, errSubscriptionRuleSetRevisionConflict):
			writeJSON(w, http.StatusConflict, "规则集已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": currentRevision})
		case errors.Is(err, errSubscriptionRuleSetTagImmutable):
			BadRequestFields(w, "规则集信息校验失败。", map[string]string{"tag": "规则集标识用于公开地址，创建后不能修改。"})
		case errors.Is(err, errSubscriptionRuleSetLegacyReadOnly):
			BadRequest(w, "旧版外部规则声明仅保留兼容读取，请新建自有规则集后替换模板引用。")
		case isDuplicateError(err):
			BadRequestFields(w, "规则集信息校验失败。", map[string]string{"tag": "该规则集标识已存在。"})
		default:
			ServerError(w, err)
		}
		return
	}
	items := []model.SubscriptionRuleSet{item}
	if err := presentSubscriptionRuleSetUsage(h.db, items); err != nil {
		ServerError(w, err)
		return
	}
	OK(w, h.presentManagedRuleSet(items[0]))
}

var (
	errSubscriptionRuleSetRevisionConflict = errors.New("subscription rule set revision conflict")
	errSubscriptionRuleSetTagImmutable     = errors.New("subscription rule set tag is immutable")
	errSubscriptionRuleSetLegacyReadOnly   = errors.New("legacy subscription rule set is read only")
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
	var item model.SubscriptionRuleSet
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return err
		}
		counts, err := subscriptionRuleSetUsageCounts(tx, []uint{id})
		if err != nil {
			return err
		}
		if counts[id] > 0 {
			item.UsageCount = counts[id]
			return errSubscriptionRuleSetInUse
		}
		if err := tx.Delete(&item).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "subscription_rule_set.delete", fmt.Sprintf("subscription_rule_set:%d", item.ID), fmt.Sprintf("renderer=%s tag=%s", item.Renderer, item.Tag))
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(w)
		case errors.Is(err, errSubscriptionRuleSetInUse):
			writeJSON(w, http.StatusConflict, "该规则集仍被订阅模板引用，请先从模板中移除。", map[string]interface{}{"usage_count": item.UsageCount})
		default:
			ServerError(w, err)
		}
		return
	}
	if item.Renderer == managedRuleSetRenderer {
		if err := h.removeManagedRuleSetFiles(item.Tag); err != nil {
			ServerError(w, err)
			return
		}
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true})
}

var errSubscriptionRuleSetInUse = errors.New("subscription rule set is in use")

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
		resolved.URL = managedRulePublicURL(siteURL, record.Tag, managedRuleArtifactZeroRuleIR)
		resolved.Format = managedRuleSourceZeroRuleIR
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

func syncSubscriptionTemplateRuleSetBindings(db *gorm.DB, templateID uint, customizationRaw json.RawMessage) error {
	var customization subscriptionTemplateCustomization
	if err := json.Unmarshal(customizationRaw, &customization); err != nil {
		return err
	}
	if err := db.Where("subscription_template_id = ?", templateID).Delete(&model.SubscriptionTemplateRuleSetBinding{}).Error; err != nil {
		return err
	}
	bindings := make([]model.SubscriptionTemplateRuleSetBinding, 0, len(customization.RuleSets))
	for position, ruleSet := range customization.RuleSets {
		if ruleSet.RuleSetID == 0 {
			continue
		}
		bindings = append(bindings, model.SubscriptionTemplateRuleSetBinding{
			SubscriptionTemplateID: templateID,
			SubscriptionRuleSetID:  ruleSet.RuleSetID,
			Action:                 ruleSet.Target,
			Position:               position,
		})
	}
	if len(bindings) == 0 {
		return nil
	}
	return db.Create(&bindings).Error
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
