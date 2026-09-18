package entitlementstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionTemplates struct{ DB *gorm.DB }

func subscriptionTemplateModel(row entitlements.SubscriptionTemplate) model.SubscriptionTemplate {
	return model.SubscriptionTemplate{ID: row.ID, Name: row.Name, Slug: row.Slug, Description: row.Description, Renderer: row.Renderer, Customization: row.Customization, IsActive: row.IsActive, SortOrder: row.SortOrder, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func subscriptionTemplateView(row model.SubscriptionTemplate) entitlements.SubscriptionTemplate {
	return entitlements.SubscriptionTemplate{ID: row.ID, Name: row.Name, Slug: row.Slug, Description: row.Description, Renderer: row.Renderer, Customization: row.Customization, IsActive: row.IsActive, SortOrder: row.SortOrder, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func templateAdmin(tx *gorm.DB, actor uint) (model.User, error) {
	var user model.User
	if actor == 0 {
		return user, entitlements.ErrAdministrativeRead
	}
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return user, entitlements.ErrAdministrativeRead
	}
	return user, err
}

func (s SubscriptionTemplates) ReconcileTemplateDefaults(ctx context.Context, updates []entitlements.SubscriptionTemplateDefault) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, update := range updates {
			result := tx.Model(&model.SubscriptionTemplate{}).Where("id = ?", update.ID).Updates(map[string]any{"renderer": update.Renderer, "customization": update.Customization})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return entitlements.ErrTemplateNotFound
			}
		}
		return nil
	})
}

func (s SubscriptionTemplates) SaveTemplate(ctx context.Context, actor uint, input entitlements.SubscriptionTemplate, expected *uint64, bindings []entitlements.SubscriptionTemplateBinding) (out entitlements.SubscriptionTemplate, current uint64, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := templateAdmin(tx, actor)
		if err != nil {
			return err
		}
		if err := validateTemplateBindings(tx, bindings); err != nil {
			return err
		}
		row := subscriptionTemplateModel(input)
		action := "subscription_template.create"
		if row.ID == 0 {
			row.Revision = 1
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			action = "subscription_template.update"
			var existing model.SubscriptionTemplate
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, row.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return entitlements.ErrTemplateNotFound
				}
				return err
			}
			current = existing.Revision
			if expected != nil && existing.Revision != *expected {
				return entitlements.ErrTemplateConflict
			}
			row.CreatedAt = existing.CreatedAt
			row.Revision = existing.Revision + 1
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		if err := replaceTemplateBindings(tx, row.ID, bindings); err != nil {
			return err
		}
		detail := fmt.Sprintf("slug=%s revision=%d", row.Slug, row.Revision)
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("subscription_template:%d", row.ID), Detail: detail}).Error; err != nil {
			return err
		}
		out = subscriptionTemplateView(row)
		return nil
	})
	return
}

func validateTemplateBindings(tx *gorm.DB, bindings []entitlements.SubscriptionTemplateBinding) error {
	ids := make([]uint, 0, len(bindings))
	seen := make(map[uint]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.RuleSetID == 0 {
			continue
		}
		if _, ok := seen[binding.RuleSetID]; !ok {
			seen[binding.RuleSetID] = struct{}{}
			ids = append(ids, binding.RuleSetID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.SubscriptionRuleSet{}).Where("id IN ? AND is_active = ?", ids, true).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(ids)) {
		return entitlements.ErrTemplateRuleSet
	}
	return nil
}

func replaceTemplateBindings(tx *gorm.DB, templateID uint, bindings []entitlements.SubscriptionTemplateBinding) error {
	if err := tx.Where("subscription_template_id = ?", templateID).Delete(&model.SubscriptionTemplateRuleSetBinding{}).Error; err != nil {
		return err
	}
	rows := make([]model.SubscriptionTemplateRuleSetBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.RuleSetID == 0 {
			continue
		}
		rows = append(rows, model.SubscriptionTemplateRuleSetBinding{SubscriptionTemplateID: templateID, SubscriptionRuleSetID: binding.RuleSetID, Action: binding.Action, Position: binding.Position})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func (s SubscriptionTemplates) DeleteTemplate(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.SubscriptionTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return entitlements.ErrTemplateNotFound
			}
			return err
		}
		admin, err := templateAdmin(tx, actor)
		if err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "subscription_template.delete", Target: fmt.Sprintf("subscription_template:%d", row.ID), Detail: "slug=" + row.Slug}).Error
	})
}

func ensureClientTemplates(tx *gorm.DB, definitions []entitlements.SubscriptionTemplate) error {
	for _, definition := range definitions {
		var existing model.SubscriptionTemplate
		err := tx.Where("slug = ?", definition.Slug).First(&existing).Error
		switch {
		case err == nil:
			continue
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		row := subscriptionTemplateModel(definition)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s SubscriptionTemplates) EnsureClientTemplates(ctx context.Context, definitions []entitlements.SubscriptionTemplate) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ensureClientTemplates(tx, definitions) })
}

func (s SubscriptionTemplates) SeedClientTemplates(ctx context.Context, definitions []entitlements.SubscriptionTemplate, action, detail string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var seeded int64
		if err := tx.Model(&model.AuditLog{}).Where("action = ?", action).Count(&seeded).Error; err != nil {
			return err
		}
		if seeded > 0 {
			return nil
		}
		if err := ensureClientTemplates(tx, definitions); err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{Actor: "system", Action: action, Target: "subscription_templates", Detail: detail}).Error
	})
}

func (s SubscriptionTemplates) ListTemplates(ctx context.Context, input entitlements.SubscriptionTemplateQuery) (entitlements.SubscriptionTemplatePage, error) {
	query := s.DB.WithContext(ctx).Model(&model.SubscriptionTemplate{})
	if input.Active != nil {
		query = query.Where("is_active = ?", *input.Active)
	}
	if input.Search != "" {
		pattern := "%" + input.Search + "%"
		query = query.Where("name LIKE ? OR slug LIKE ? OR description LIKE ?", pattern, pattern, pattern)
	}
	page := entitlements.SubscriptionTemplatePage{}
	if input.Paged {
		if err := query.Count(&page.Total).Error; err != nil {
			return page, err
		}
		query = query.Offset(input.Offset).Limit(input.Limit)
	}
	var rows []model.SubscriptionTemplate
	if err := query.Order("sort_order asc, id asc").Find(&rows).Error; err != nil {
		return page, err
	}
	page.Items = make([]entitlements.SubscriptionTemplate, 0, len(rows))
	for _, row := range rows {
		page.Items = append(page.Items, subscriptionTemplateView(row))
	}
	return page, nil
}

func (s SubscriptionTemplates) TemplateByID(ctx context.Context, id uint) (entitlements.SubscriptionTemplate, error) {
	var row model.SubscriptionTemplate
	if err := s.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return entitlements.SubscriptionTemplate{}, entitlements.ErrTemplateNotFound
		}
		return entitlements.SubscriptionTemplate{}, err
	}
	return subscriptionTemplateView(row), nil
}

func (s SubscriptionTemplates) ActiveTemplateBySlug(ctx context.Context, slug string) (entitlements.SubscriptionTemplateRenderSource, error) {
	var row model.SubscriptionTemplate
	if err := s.DB.WithContext(ctx).Where("slug = ? AND is_active = ?", slug, true).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return entitlements.SubscriptionTemplateRenderSource{}, entitlements.ErrTemplateNotFound
		}
		return entitlements.SubscriptionTemplateRenderSource{}, err
	}
	var installation model.Installation
	err := s.DB.WithContext(ctx).Select("site_name").First(&installation, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return entitlements.SubscriptionTemplateRenderSource{}, err
	}
	return entitlements.SubscriptionTemplateRenderSource{Template: subscriptionTemplateView(row), SiteName: installation.SiteName}, nil
}
