package platformstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s Settings) UpdateSite(ctx context.Context, actor uint, in platform.SiteSettingsInput) (platform.SiteSettingsView, error) {
	var installation model.Installation
	err := jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrSettingsPermission
			}
			return err
		}
		current, err := maintenanceData(tx, true)
		if err != nil {
			return err
		}
		state := current.State()
		if state.MigrationInProgress || state.MigrationCutoverPending {
			return platform.ErrMaintenanceBusy
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error; err != nil {
			return err
		}
		installation.SiteName, installation.SiteURL, installation.AllowRegistration = in.SiteName, in.SiteURL, in.AllowRegistration
		if err := tx.Save(&installation).Error; err != nil {
			return err
		}
		if err := UpdateSiteConfigProjection(tx, in.SiteName, in.SiteURL, in.AllowRegistration); err != nil {
			return err
		}
		var user model.User
		if err := tx.Select("id", "email").First(&user, actor).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "system.settings.update", Target: "installation:1", Detail: fmt.Sprintf("allow_registration=%t", in.AllowRegistration)}).Error
	})
	if err != nil {
		return platform.SiteSettingsView{}, err
	}
	return platform.SiteSettingsView{SiteSettingsInput: in, InstalledAt: installation.InstalledAt, UpdatedAt: installation.UpdatedAt}, nil
}

// Installation bootstrap uses the same ordered projection inside its transaction.
func UpdateSiteConfigProjection(tx *gorm.DB, name, address string, registration bool) error {
	for _, pair := range [][2]string{{"site_name", name}, {"site_url", address}, {"register_switch", strconv.FormatBool(registration)}} {
		if err := tx.Model(&model.SystemConfig{}).Where("config_key = ?", pair[0]).Updates(map[string]any{"value": pair[1], "revision": gorm.Expr("revision + 1")}).Error; err != nil {
			return err
		}
	}
	return nil
}
