package entitlementstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	managedRuleRenderer     = "managed"
	managedRuleClientFormat = "managed_client_rules"
)

type SubscriptionRuleSets struct {
	DB      *gorm.DB
	Content entitlements.RuleSetContentStore
}

func subscriptionRuleSetModel(row entitlements.SubscriptionRuleSet) model.SubscriptionRuleSet {
	return model.SubscriptionRuleSet{ID: row.ID, Name: row.Name, Description: row.Description, Renderer: row.Renderer, Tag: row.Tag, URL: row.URL, Behavior: row.Behavior, Format: row.Format, Interval: row.Interval, IsActive: row.IsActive, Revision: row.Revision, UsageCount: row.UsageCount, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func subscriptionRuleSetView(row model.SubscriptionRuleSet) entitlements.SubscriptionRuleSet {
	return entitlements.SubscriptionRuleSet{ID: row.ID, Name: row.Name, Description: row.Description, Renderer: row.Renderer, Tag: row.Tag, URL: row.URL, Behavior: row.Behavior, Format: row.Format, Interval: row.Interval, IsActive: row.IsActive, Revision: row.Revision, UsageCount: row.UsageCount, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s SubscriptionRuleSets) ListRuleSets(ctx context.Context, actor uint, input entitlements.SubscriptionRuleSetQuery) (entitlements.SubscriptionRuleSetPage, error) {
	if _, err := templateAdmin(s.DB.WithContext(ctx), actor); err != nil {
		return entitlements.SubscriptionRuleSetPage{}, err
	}
	query := s.DB.WithContext(ctx).Model(&model.SubscriptionRuleSet{})
	if input.Keyword != "" {
		pattern := "%" + input.Keyword + "%"
		query = query.Where("name LIKE ? OR tag LIKE ? OR description LIKE ? OR url LIKE ?", pattern, pattern, pattern, pattern)
	}
	if len(input.Renderers) > 0 {
		query = query.Where("renderer IN ?", input.Renderers)
	}
	if input.ExcludeFormat != "" {
		query = query.Where("format <> ?", input.ExcludeFormat)
	}
	if input.Active != nil {
		query = query.Where("is_active = ?", *input.Active)
	}
	if input.ID != 0 {
		query = query.Where("id = ?", input.ID)
	}
	if len(input.IDs) > 0 {
		query = query.Where("id IN ?", input.IDs)
	}
	page := entitlements.SubscriptionRuleSetPage{}
	if err := query.Count(&page.Total).Error; err != nil {
		return page, err
	}
	var rows []model.SubscriptionRuleSet
	if err := query.Order("updated_at desc, id desc").Offset(input.Offset).Limit(input.Limit).Find(&rows).Error; err != nil {
		return page, err
	}
	if err := s.decorateRuleSetUsage(ctx, rows); err != nil {
		return page, err
	}
	page.Items = make([]entitlements.SubscriptionRuleSet, 0, len(rows))
	for _, row := range rows {
		page.Items = append(page.Items, subscriptionRuleSetView(row))
	}
	page.SiteURL, _ = s.RuleSetSiteURL(ctx)
	return page, nil
}

func (s SubscriptionRuleSets) GetRuleSet(ctx context.Context, actor, id uint) (entitlements.SubscriptionRuleSet, string, error) {
	if _, err := templateAdmin(s.DB.WithContext(ctx), actor); err != nil {
		return entitlements.SubscriptionRuleSet{}, "", err
	}
	var row model.SubscriptionRuleSet
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return entitlements.SubscriptionRuleSet{}, "", ruleSetNotFound(err)
	}
	rows := []model.SubscriptionRuleSet{row}
	if err := s.decorateRuleSetUsage(ctx, rows); err != nil {
		return entitlements.SubscriptionRuleSet{}, "", err
	}
	siteURL, _ := s.RuleSetSiteURL(ctx)
	return subscriptionRuleSetView(rows[0]), siteURL, nil
}

func (s SubscriptionRuleSets) GetPublicRuleSet(ctx context.Context, tag string) (entitlements.SubscriptionRuleSet, error) {
	var row model.SubscriptionRuleSet
	err := s.DB.WithContext(ctx).Where("renderer = ? AND tag = ? AND is_active = ?", managedRuleRenderer, tag, true).First(&row).Error
	if err != nil {
		return entitlements.SubscriptionRuleSet{}, ruleSetNotFound(err)
	}
	return subscriptionRuleSetView(row), nil
}

func (s SubscriptionRuleSets) RuleSetSiteURL(ctx context.Context) (string, error) {
	var installation model.Installation
	if err := s.DB.WithContext(ctx).Select("site_url").First(&installation, 1).Error; err != nil {
		return "", err
	}
	return installation.SiteURL, nil
}

func (s SubscriptionRuleSets) decorateRuleSetUsage(ctx context.Context, rows []model.SubscriptionRuleSet) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	type usageRow struct {
		ID    uint
		Count int64
	}
	var counts []usageRow
	if err := s.DB.WithContext(ctx).Model(&model.SubscriptionTemplateRuleSetBinding{}).
		Select("subscription_rule_set_id AS id, COUNT(*) AS count").Where("subscription_rule_set_id IN ?", ids).
		Group("subscription_rule_set_id").Scan(&counts).Error; err != nil {
		return err
	}
	byID := make(map[uint]int64, len(counts))
	for _, count := range counts {
		byID[count.ID] = count.Count
	}
	for index := range rows {
		rows[index].UsageCount = byID[rows[index].ID]
	}
	return nil
}

func (s SubscriptionRuleSets) SaveRuleSet(ctx context.Context, actor uint, input entitlements.SubscriptionRuleSet, expected *uint64, content []byte, replaceContent bool) (out entitlements.SubscriptionRuleSet, current uint64, err error) {
	var created, contentAttempted, previousFound bool
	var previous []byte
	var contentTag string
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := templateAdmin(tx, actor)
		if err != nil {
			return err
		}
		row := subscriptionRuleSetModel(input)
		action := "subscription_rule_set.create"
		if row.ID == 0 {
			row.Revision = 1
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			created = true
			contentAttempted, contentTag = true, row.Tag
			if err := s.Content.Write(ctx, row.Tag, content); err != nil {
				return err
			}
		} else {
			action = "subscription_rule_set.update"
			var existing model.SubscriptionRuleSet
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, row.ID).Error; err != nil {
				return ruleSetNotFound(err)
			}
			if existing.Renderer != managedRuleRenderer {
				return entitlements.ErrRuleSetLegacyReadOnly
			}
			current = existing.Revision
			if expected != nil && existing.Revision != *expected {
				return entitlements.ErrRuleSetConflict
			}
			if existing.Tag != row.Tag {
				return entitlements.ErrRuleSetTagImmutable
			}
			if !replaceContent {
				row.Format = existing.Format
			}
			if err := guardRuleSetClientCompatibility(tx, existing.ID, row.Format); err != nil {
				return err
			}
			if replaceContent {
				previous, previousFound, err = s.Content.Read(ctx, existing.Tag)
				if err != nil {
					return err
				}
				contentAttempted, contentTag = true, existing.Tag
				if err := s.Content.Write(ctx, existing.Tag, content); err != nil {
					return err
				}
			}
			row.CreatedAt = existing.CreatedAt
			row.Revision = existing.Revision + 1
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		detail := fmt.Sprintf("managed=true tag=%s revision=%d", row.Tag, row.Revision)
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("subscription_rule_set:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		out = subscriptionRuleSetView(row)
		return nil
	})
	if err != nil {
		s.rollbackContent(ctx, contentTag, created, contentAttempted, previous, previousFound)
	}
	return
}

func (s SubscriptionRuleSets) ReplaceRuleSetContent(ctx context.Context, actor uint, input entitlements.SubscriptionRuleSet, expected *uint64, content []byte) (out entitlements.SubscriptionRuleSet, current uint64, err error) {
	var previous []byte
	var contentAttempted, previousFound bool
	var contentTag string
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := templateAdmin(tx, actor)
		if err != nil {
			return err
		}
		var row model.SubscriptionRuleSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, input.ID).Error; err != nil {
			return ruleSetNotFound(err)
		}
		if row.Renderer != managedRuleRenderer {
			return entitlements.ErrRuleSetNotFound
		}
		current = row.Revision
		if expected != nil && row.Revision != *expected {
			return entitlements.ErrRuleSetConflict
		}
		if err := guardRuleSetClientCompatibility(tx, row.ID, input.Format); err != nil {
			return err
		}
		previous, previousFound, err = s.Content.Read(ctx, row.Tag)
		if err != nil {
			return err
		}
		contentAttempted, contentTag = true, row.Tag
		if err := s.Content.Write(ctx, row.Tag, content); err != nil {
			return err
		}
		updates := map[string]any{"revision": row.Revision + 1, "format": input.Format}
		if input.URL != row.URL || input.Behavior != row.Behavior {
			updates["url"] = input.URL
			updates["behavior"] = input.Behavior
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		row.Revision++
		row.Format = input.Format
		row.URL, row.Behavior = input.URL, input.Behavior
		detail := fmt.Sprintf("bytes=%d revision=%d", len(content), row.Revision)
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription_rule_set.content.update", Target: fmt.Sprintf("subscription_rule_set:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		out = subscriptionRuleSetView(row)
		return nil
	})
	if err != nil {
		s.rollbackContent(ctx, contentTag, false, contentAttempted, previous, previousFound)
	}
	return
}

func (s SubscriptionRuleSets) DeleteRuleSet(ctx context.Context, actor, id uint) (out entitlements.SubscriptionRuleSet, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.SubscriptionRuleSet
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return ruleSetNotFound(err)
		}
		admin, err := templateAdmin(tx, actor)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.SubscriptionTemplateRuleSetBinding{}).Where("subscription_rule_set_id = ?", id).Count(&row.UsageCount).Error; err != nil {
			return err
		}
		out = subscriptionRuleSetView(row)
		if row.UsageCount > 0 {
			return entitlements.ErrRuleSetInUse
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		detail := fmt.Sprintf("renderer=%s tag=%s", row.Renderer, row.Tag)
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription_rule_set.delete", Target: fmt.Sprintf("subscription_rule_set:%d", row.ID), Detail: detail}).Error
	})
	if err == nil && out.Renderer == managedRuleRenderer {
		err = s.Content.RemoveAll(ctx, out.Tag)
	}
	return
}

func (s SubscriptionRuleSets) rollbackContent(ctx context.Context, tag string, created, attempted bool, previous []byte, previousFound bool) {
	if created {
		_ = s.Content.RemoveAll(ctx, tag)
		return
	}
	if !attempted {
		return
	}
	if previousFound {
		_ = s.Content.Write(ctx, tag, previous)
	} else {
		_ = s.Content.RemoveSource(ctx, tag)
	}
}

func guardRuleSetClientCompatibility(tx *gorm.DB, id uint, format string) error {
	if format != managedRuleClientFormat {
		return nil
	}
	var names []string
	err := tx.Model(&model.SubscriptionTemplate{}).Joins("JOIN subscription_template_rule_set_bindings b ON b.subscription_template_id = subscription_templates.id").Where("b.subscription_rule_set_id = ? AND subscription_templates.renderer IN ?", id, []string{"znet-sink", "zero"}).Pluck("subscription_templates.name", &names).Error
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("%w 请先调整已引用的 Zero 模板：%s", entitlements.ErrRuleSetClientCompatibility, strings.Join(names, "、"))
	}
	return nil
}

func ruleSetNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entitlements.ErrRuleSetNotFound
	}
	return err
}

func (s SubscriptionRuleSets) ResolveRuleSets(ctx context.Context, ids []uint) ([]entitlements.SubscriptionRuleSet, string, error) {
	rows := make([]model.SubscriptionRuleSet, 0, len(ids))
	if len(ids) > 0 {
		if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, "", err
		}
	}
	var installation model.Installation
	if err := s.DB.WithContext(ctx).Select("site_url").First(&installation, 1).Error; err != nil {
		return nil, "", err
	}
	items := make([]entitlements.SubscriptionRuleSet, 0, len(rows))
	for _, row := range rows {
		items = append(items, subscriptionRuleSetView(row))
	}
	return items, installation.SiteURL, nil
}
