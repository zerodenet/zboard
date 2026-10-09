package experiencestore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Files struct{ DB *gorm.DB }

func storedFile(row model.StoredFile) experience.StoredFile {
	return experience.StoredFile{ID: row.ID, OwnerID: row.OwnerID, Purpose: row.Purpose, Name: row.Name, ContentType: row.ContentType, Size: row.Size, CreatedAt: row.CreatedAt}
}

func (s Files) SaveFile(ctx context.Context, actor experience.TicketActor, file experience.StoredFile) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The installation row serializes quota checks across processes/users.
		var installation model.Installation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error; err != nil {
			return err
		}
		if _, err := currentExperienceUser(tx, actor.ID, file.Purpose == "site"); err != nil {
			return err
		}
		var total, owned int64
		if err := tx.Model(&model.StoredFile{}).Where("deleted_at IS NULL").Select("COALESCE(SUM(size), 0)").Scan(&total).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.StoredFile{}).Where("owner_id = ? AND deleted_at IS NULL", actor.ID).Select("COALESCE(SUM(size), 0)").Scan(&owned).Error; err != nil {
			return err
		}
		if total+file.Size > experience.MaxStoredFileBytes || owned+file.Size > experience.MaxUserFileBytes {
			return fmt.Errorf("%w: file storage quota exceeded", experience.ErrInvalid)
		}
		return tx.Create(&model.StoredFile{ID: file.ID, OwnerID: actor.ID, Purpose: file.Purpose, Name: file.Name, ContentType: file.ContentType, Size: file.Size, CreatedAt: file.CreatedAt}).Error
	})
}

func (s Files) ReadFile(ctx context.Context, actor experience.TicketActor, id string, publicOnly bool) (experience.StoredFile, error) {
	tx := s.DB.WithContext(ctx)
	var row model.StoredFile
	if err := tx.Where("id = ? AND deleted_at IS NULL", id).First(&row).Error; err != nil {
		return experience.StoredFile{}, announcementNotFound(err)
	}
	if row.Purpose == "site" {
		return storedFile(row), nil
	}
	if publicOnly || actor.ID == 0 {
		return experience.StoredFile{}, experience.ErrNotFound
	}
	user, err := currentExperienceUser(tx, actor.ID, actor.IsAdmin)
	if err != nil {
		return experience.StoredFile{}, err
	}
	if user.IsAdmin && actor.IsAdmin {
		return storedFile(row), nil
	}
	var owner uint
	var links int64
	if err := tx.Model(&model.TicketAttachment{}).Where("file_id = ?", id).Count(&links).Error; err != nil {
		return experience.StoredFile{}, err
	}
	if links > 0 {
		if err := tx.Table("ticket_attachments a").Select("t.user_id").Joins("JOIN ticket_messages m ON m.id = a.message_id").Joins("JOIN tickets t ON t.id = m.ticket_id").Where("a.file_id = ?", id).Scan(&owner).Error; err != nil {
			return experience.StoredFile{}, err
		}
	} else {
		owner = row.OwnerID
	}
	if owner != actor.ID {
		return experience.StoredFile{}, experience.ErrTicketForbidden
	}
	return storedFile(row), nil
}

func (s Files) DeleteFile(ctx context.Context, actor experience.TicketActor, id string, now time.Time) (out experience.StoredFile, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := currentExperienceUser(tx, actor.ID, actor.IsAdmin); err != nil {
			return err
		}
		var row model.StoredFile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return announcementNotFound(err)
		}
		if row.OwnerID != actor.ID && !actor.IsAdmin {
			return experience.ErrTicketForbidden
		}
		var count int64
		if err := tx.Model(&model.TicketAttachment{}).Where("file_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return experience.ErrFileInUse
		}
		if row.Purpose == "site" {
			if err := tx.Model(&model.SystemConfig{}).Where("config_key IN ? AND value = ?", []string{"site_logo", "site_logo_dark", "site_favicon"}, storedFile(row).URL()).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return experience.ErrFileInUse
			}
		}
		out = storedFile(row)
		return tx.Model(&row).Update("deleted_at", now).Error
	})
	return out, err
}
