package experiencestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Announcements struct{ DB *gorm.DB }

func currentExperienceUser(tx *gorm.DB, actor uint, requireAdmin bool) (model.User, error) {
	var user model.User
	query := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ?", actor, "active")
	if requireAdmin {
		query = query.Where("is_admin = ?", true)
	}
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return user, experience.ErrPermission
		}
		return user, err
	}
	return user, nil
}

func announcementView(row model.Announcement) experience.Announcement {
	return experience.Announcement{ID: row.ID, Title: row.Title, Content: row.Content, Severity: row.Severity, Audience: row.Audience, Status: row.Status, PopupEnabled: row.PopupEnabled, Dismissible: row.Dismissible, StartsAt: row.StartsAt, EndsAt: row.EndsAt, CreatedBy: row.CreatedBy, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s Announcements) Acknowledge(ctx context.Context, actor, id uint, revision uint64, now time.Time) (out experience.AnnouncementReceipt, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := currentExperienceUser(tx, actor, false)
		if err != nil {
			return err
		}
		var row model.Announcement
		if err := tx.First(&row, id).Error; err != nil {
			return announcementNotFound(err)
		}
		allowedAudience := row.Audience == "all" || (!user.IsAdmin && row.Audience == "user") || (user.IsAdmin && row.Audience == "admin")
		if row.Status != "published" || (row.StartsAt != nil && row.StartsAt.After(now)) || !allowedAudience {
			return experience.ErrNotFound
		}
		if row.Revision != revision {
			return experience.ErrConflict
		}
		receipt := model.AnnouncementRead{AnnouncementID: row.ID, UserID: user.ID, Revision: row.Revision, ReadAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "announcement_id"}, {Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"revision", "read_at", "updated_at"})}).Create(&receipt).Error; err != nil {
			return err
		}
		out = experience.AnnouncementReceipt{AnnouncementID: receipt.AnnouncementID, UserID: receipt.UserID, Revision: receipt.Revision, ReadAt: receipt.ReadAt, CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.UpdatedAt}
		return nil
	})
	return out, err
}

func (s Announcements) SaveAnnouncement(ctx context.Context, actor uint, change experience.AnnouncementChange, now time.Time) (out experience.Announcement, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := currentExperienceUser(tx, actor, true)
		if err != nil {
			return err
		}
		row := model.Announcement{ID: change.ID, Title: change.Title, Content: change.Content, Severity: change.Severity, Audience: change.Audience, Status: change.Status, StartsAt: change.StartsAt, EndsAt: change.EndsAt}
		action := "announcement.create"
		if row.ID == 0 {
			row.CreatedBy = admin.ID
			row.Revision = 1
			row.PopupEnabled = change.PopupEnabled != nil && *change.PopupEnabled
			row.Dismissible = true
			if change.Dismissible != nil {
				row.Dismissible = *change.Dismissible
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			action = "announcement.update"
			var existing model.Announcement
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, row.ID).Error; err != nil {
				return announcementNotFound(err)
			}
			if change.Expected == nil || existing.Revision != *change.Expected {
				return experience.ErrConflict
			}
			if existing.Status == "archived" || (existing.Status == "draft" && row.Status == "archived") || (existing.Status == "published" && row.Status == "draft") {
				return experience.ErrAnnouncementState
			}
			row.CreatedBy, row.CreatedAt = existing.CreatedBy, existing.CreatedAt
			row.PopupEnabled, row.Dismissible = existing.PopupEnabled, existing.Dismissible
			if change.PopupEnabled != nil {
				row.PopupEnabled = *change.PopupEnabled
			}
			if change.Dismissible != nil {
				row.Dismissible = *change.Dismissible
			}
			row.Revision = existing.Revision + 1
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: action, Target: fmt.Sprintf("announcement:%d", row.ID), Detail: "status=" + row.Status}).Error; err != nil {
			return err
		}
		out = announcementView(row)
		return nil
	})
	return out, err
}

func (s Announcements) DeleteAnnouncement(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := currentExperienceUser(tx, actor, true)
		if err != nil {
			return err
		}
		var row model.Announcement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return announcementNotFound(err)
		}
		if row.Status != "draft" && row.Status != "archived" {
			return experience.ErrAnnouncementState
		}
		if err := tx.Where("announcement_id = ?", row.ID).Delete(&model.AnnouncementRead{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "announcement.delete", Target: fmt.Sprintf("announcement:%d", row.ID), Detail: "draft deleted"}).Error
	})
}

func announcementNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return experience.ErrNotFound
	}
	return err
}

func announcementReads(ctx context.Context, db *gorm.DB, userID uint, records []model.Announcement) (map[uint]model.AnnouncementRead, error) {
	result := map[uint]model.AnnouncementRead{}
	if userID == 0 || len(records) == 0 {
		return result, nil
	}
	ids := make([]uint, 0, len(records))
	for _, row := range records {
		ids = append(ids, row.ID)
	}
	var rows []model.AnnouncementRead
	if err := db.WithContext(ctx).Where("user_id = ? AND announcement_id IN ?", userID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.AnnouncementID] = row
	}
	return result, nil
}

func announcementReadItems(records []model.Announcement, reads map[uint]model.AnnouncementRead, now time.Time) []experience.AnnouncementReadItem {
	items := make([]experience.AnnouncementReadItem, 0, len(records))
	for _, row := range records {
		receipt := reads[row.ID]
		read := receipt.Revision >= row.Revision
		var readAt *time.Time
		if read {
			value := receipt.ReadAt
			readAt = &value
		}
		items = append(items, experience.AnnouncementReadItem{Announcement: announcementView(row), Read: read, ReadAt: readAt, Active: row.EndsAt == nil || row.EndsAt.After(now)})
	}
	return items
}

func announcementScope(db *gorm.DB, input experience.AnnouncementAudienceQuery) *gorm.DB {
	query := db.Model(&model.Announcement{}).Where("status = ?", "published").Where("audience IN ?", input.Audiences).Where("starts_at IS NULL OR starts_at <= ?", input.Now)
	if input.ID != 0 {
		query = query.Where("id = ?", input.ID)
	}
	return query
}

func unreadScope(query *gorm.DB, userID uint, now time.Time) *gorm.DB {
	return query.Where("ends_at IS NULL OR ends_at > ?", now).Where(`NOT EXISTS (SELECT 1 FROM announcement_reads WHERE announcement_reads.announcement_id = announcements.id AND announcement_reads.user_id = ? AND announcement_reads.revision >= announcements.revision)`, userID)
}

func (s Announcements) ActiveAnnouncements(ctx context.Context, input experience.AnnouncementAudienceQuery) ([]experience.AnnouncementReadItem, error) {
	var rows []model.Announcement
	if err := announcementScope(s.DB.WithContext(ctx), input).Where("ends_at IS NULL OR ends_at > ?", input.Now).Order("popup_enabled DESC, COALESCE(starts_at, created_at) DESC, id DESC").Limit(5).Find(&rows).Error; err != nil {
		return nil, err
	}
	reads, err := announcementReads(ctx, s.DB, input.UserID, rows)
	if err != nil {
		return nil, err
	}
	return announcementReadItems(rows, reads, input.Now), nil
}

func (s Announcements) AnnouncementUnreadCount(ctx context.Context, input experience.AnnouncementAudienceQuery) (int64, error) {
	var count int64
	err := unreadScope(announcementScope(s.DB.WithContext(ctx), input), input.UserID, input.Now).Count(&count).Error
	return count, err
}

func (s Announcements) AnnouncementHistory(ctx context.Context, input experience.AnnouncementAudienceQuery) (experience.AnnouncementPage, error) {
	page := experience.AnnouncementPage{}
	base := announcementScope(s.DB.WithContext(ctx), input)
	if err := base.Count(&page.Total).Error; err != nil {
		return page, err
	}
	if err := unreadScope(announcementScope(s.DB.WithContext(ctx), input), input.UserID, input.Now).Count(&page.Unread).Error; err != nil {
		return page, err
	}
	var rows []model.Announcement
	if err := base.Order(clause.Expr{SQL: "CASE WHEN ends_at IS NULL OR ends_at > ? THEN 0 ELSE 1 END", Vars: []interface{}{input.Now}, WithoutParentheses: true}).Order("popup_enabled DESC").Order("CASE severity WHEN 'critical' THEN 4 WHEN 'warning' THEN 3 WHEN 'success' THEN 2 ELSE 1 END DESC").Order("starts_at DESC, id DESC").Offset(input.Offset).Limit(input.Limit).Find(&rows).Error; err != nil {
		return page, err
	}
	reads, err := announcementReads(ctx, s.DB, input.UserID, rows)
	if err != nil {
		return page, err
	}
	page.Items = announcementReadItems(rows, reads, input.Now)
	return page, nil
}

func (s Announcements) AdminAnnouncements(ctx context.Context, status string, offset, limit int) (experience.AnnouncementAdminPage, error) {
	query := s.DB.WithContext(ctx).Model(&model.Announcement{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	page := experience.AnnouncementAdminPage{}
	if err := query.Count(&page.Total).Error; err != nil {
		return page, err
	}
	var rows []model.Announcement
	if err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return page, err
	}
	page.Items = make([]experience.Announcement, 0, len(rows))
	for _, row := range rows {
		page.Items = append(page.Items, announcementView(row))
	}
	return page, nil
}
