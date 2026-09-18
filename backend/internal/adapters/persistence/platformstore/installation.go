package platformstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type Installation struct{ DB *gorm.DB }

func (s Installation) Status(ctx context.Context) (platform.InstallationStatus, error) {
	var row model.Installation
	err := s.DB.WithContext(ctx).First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return platform.InstallationStatus{}, nil
	}
	if err != nil {
		return platform.InstallationStatus{}, err
	}
	return platform.InstallationStatus{Installed: true, SiteSettingsInput: platform.SiteSettingsInput{SiteName: row.SiteName, SiteURL: row.SiteURL, AllowRegistration: row.AllowRegistration}, InstalledAt: row.InstalledAt}, nil
}
func (s Installation) Create(ctx context.Context, in platform.InstallationInput, prefs platform.SetupPreferences, hash string) (platform.InstallationResult, error) {
	row := model.Installation{ID: 1, SiteName: in.SiteName, SiteURL: in.SiteURL, AllowRegistration: in.AllowRegistration, InstalledAt: time.Now().UTC()}
	admin := model.User{AccountName: in.AdminEmail, Email: in.AdminEmail, Password: hash, IsAdmin: true, Status: "active"}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Insert the fixed key before any reads: it serializes concurrent installers
		// without first establishing a stale MySQL repeatable-read snapshot.
		if err := tx.Create(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
				return platform.ErrAlreadyInstalled
			}
			return err
		}
		var count int64
		if err := tx.Unscoped().Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return platform.ErrAlreadyInstalled
		}
		if err := UpdateSiteConfigProjection(tx, in.SiteName, in.SiteURL, in.AllowRegistration); err != nil {
			return err
		}
		if err := upsertSetupSystemPreferences(tx, prefs); err != nil {
			return err
		}
		return tx.Create(&admin).Error
	})
	if err != nil {
		return platform.InstallationResult{}, err
	}
	return platform.InstallationResult{SiteName: row.SiteName, Account: identity.PublicAccount{ID: admin.ID, Email: admin.Email, IsAdmin: true, Status: admin.Status}, Preferences: prefs}, nil
}
