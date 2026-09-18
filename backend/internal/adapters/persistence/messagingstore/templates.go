package messagingstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Templates struct{ DB *gorm.DB }

func templateAdministrator(tx *gorm.DB, actor uint) (model.User, error) {
	var user model.User
	res := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").Limit(1).Find(&user)
	if res.Error != nil {
		return user, res.Error
	}
	if res.RowsAffected != 1 {
		return user, messaging.ErrTemplatePermission
	}
	return user, nil
}
func templateView(row model.EmailTemplate) messaging.Template {
	return messaging.Template{ID: row.ID, Name: row.Name, Slug: row.Slug, Category: row.Category, TriggerKey: row.TriggerKey, SubjectTemplate: row.SubjectTemplate, BodyTemplate: row.BodyTemplate, IsActive: row.IsActive, SortOrder: row.SortOrder, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func (s Templates) List(ctx context.Context, actor uint, category string) ([]messaging.Template, error) {
	out := make([]messaging.Template, 0)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := templateAdministrator(tx, actor); err != nil {
			return err
		}
		q := tx.Model(&model.EmailTemplate{})
		if category != "" {
			q = q.Where("category = ?", category)
		}
		var rows []model.EmailTemplate
		if err := q.Order("category asc, sort_order asc, id asc").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			out = append(out, templateView(row))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s Templates) Save(ctx context.Context, actor, id uint, in messaging.TemplateWrite) (messaging.Template, error) {
	var row model.EmailTemplate
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := templateAdministrator(tx, actor)
		if err != nil {
			return err
		}
		active := in.IsActive == nil || *in.IsActive
		action := "email_template.create"
		if id != 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
				return err
			}
			if in.ExpectedRevision != nil && row.Revision != *in.ExpectedRevision {
				return &messaging.TemplateConflict{CurrentRevision: row.Revision}
			}
			if row.Category == messaging.TemplateRegistration {
				in.Category, in.Slug = row.Category, row.Slug
			} else if in.Category != messaging.TemplateOperational {
				return &messaging.TemplateValidation{Fields: map[string]string{"category": "运营模板不能转换为注册通知模板。"}}
			}
			action = "email_template.update"
		}
		row.Name, row.Slug, row.Category = in.Name, in.Slug, in.Category
		row.SubjectTemplate, row.BodyTemplate, row.IsActive, row.SortOrder = in.SubjectTemplate, in.BodyTemplate, active, in.SortOrder
		row.Revision++
		if id == 0 {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			// GORM's true default must not override an explicitly disabled template.
			if !active {
				if err := tx.Model(&row).Update("is_active", false).Error; err != nil {
					return err
				}
				row.IsActive = false
			}
		} else if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: action, Target: fmt.Sprintf("email_template:%d", row.ID), Detail: fmt.Sprintf("category=%s slug=%s revision=%d", row.Category, row.Slug, row.Revision)}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = messaging.ErrTemplateNotFound
	}
	if err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique constraint")) {
		err = messaging.ErrTemplateSlug
	}
	if err != nil {
		return messaging.Template{}, err
	}
	return templateView(row), nil
}
func (s Templates) Delete(ctx context.Context, actor, id uint) error {
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := templateAdministrator(tx, actor)
		if err != nil {
			return err
		}
		var row model.EmailTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return err
		}
		if row.Category == messaging.TemplateRegistration {
			return messaging.ErrTemplateProtected
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "email_template.delete", Target: fmt.Sprintf("email_template:%d", id), Detail: "slug=" + row.Slug}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return messaging.ErrTemplateNotFound
	}
	return err
}
func (s Templates) PreviewVariables(ctx context.Context, actor uint) (map[string]string, error) {
	out := map[string]string{"site_name": "Zboard", "site_url": "https://panel.example.com", "user_email": "member@example.com", "account_name": "member@example.com", "registered_at": "2026-08-24 12:00 UTC", "current_date": "2026-08-24"}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := templateAdministrator(tx, actor); err != nil {
			return err
		}
		var site model.Installation
		if err := tx.First(&site, 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if name := strings.TrimSpace(site.SiteName); name != "" {
			out["site_name"] = name
		}
		out["site_url"] = strings.TrimSpace(site.SiteURL)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
