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

type Maintenance struct{ DB *gorm.DB }

func (s Maintenance) Read(ctx context.Context) (out platform.MaintenanceData, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		out, err = maintenanceData(tx, false)
		return err
	})
	return out, err
}

func maintenanceData(tx *gorm.DB, lock bool) (platform.MaintenanceData, error) {
	out := platform.MaintenanceData{Revisions: map[string]uint64{}}
	var configs []model.SystemConfig
	query := tx.Where("config_key IN ?", []string{"maintenance_enabled", "maintenance_title", "maintenance_message", "maintenance_task_id"})
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Find(&configs).Error; err != nil {
		return out, err
	}
	var taskID uint64
	for _, config := range configs {
		out.Revisions[config.ConfigKey] = config.Revision
		switch config.ConfigKey {
		case "maintenance_enabled":
			out.Enabled, _ = strconv.ParseBool(config.Value)
		case "maintenance_title":
			out.Title = config.Value
		case "maintenance_message":
			out.Message = config.Value
		case "maintenance_task_id":
			taskID, _ = strconv.ParseUint(config.Value, 10, 64)
		}
	}
	if taskID > 0 {
		var task model.Task
		result := tx.Select("id", "status").Where("id = ? AND type = ?", taskID, "database_migration").First(&task)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return out, result.Error
		}
		out.MigrationCompleted = result.Error == nil && task.Status == 2
	}
	// Legacy migration intents also prevent disabling the gate, even when the
	// maintenance_task_id pointer is absent or stale.
	var active int64
	if err := tx.Model(&model.Task{}).Where("type = ? AND status IN ?", "database_migration", []int16{0, 1}).Count(&active).Error; err != nil {
		return out, err
	}
	reserved, err := MigrationReservationActive(tx)
	out.MigrationActive = active > 0 || reserved
	return out, err
}

func (s Maintenance) Apply(ctx context.Context, actor uint, in platform.MaintenanceUpdate) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrMaintenancePermission
			}
			return err
		}
		current, err := maintenanceData(tx, true)
		if err != nil {
			return err
		}
		if err := platform.ValidateMaintenanceTransition(current, in); err != nil {
			return err
		}
		values := map[string]string{"maintenance_enabled": strconv.FormatBool(in.Enabled), "maintenance_title": in.Title, "maintenance_message": in.Message}
		for _, key := range []string{"maintenance_enabled", "maintenance_title", "maintenance_message"} {
			result := tx.Model(&model.SystemConfig{}).Where("config_key = ? AND revision = ?", key, in.ExpectedRevisions[key]).Updates(map[string]any{"value": values[key], "revision": gorm.Expr("revision + 1")})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return platform.ErrMaintenanceRevision
			}
		}
		var user model.User
		if err := tx.Select("id", "email").First(&user, actor).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "maintenance.update", Target: "system:maintenance", Detail: fmt.Sprintf("enabled=%t", in.Enabled)}).Error
	})
}

func (s Maintenance) Patch(ctx context.Context, actor uint, in platform.MaintenanceSetting) error {
	return jobstore.New(s.DB).WithLedgerLock(ctx, func(tx *gorm.DB) error {
		if err := RequireMigrationAdministrator(tx, actor); err != nil {
			if errors.Is(err, jobs.ErrPermission) {
				return platform.ErrMaintenancePermission
			}
			return err
		}
		current, err := maintenanceData(tx, true)
		if err != nil {
			return err
		}
		if err := platform.ValidateMaintenanceSetting(current, in); err != nil {
			return err
		}
		revision := current.Revisions[in.Key]
		result := tx.Model(&model.SystemConfig{}).Where("config_key = ? AND revision = ?", in.Key, revision).Updates(map[string]any{"value": in.Value, "revision": gorm.Expr("revision + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return platform.ErrMaintenanceRevision
		}
		var user model.User
		if err := tx.Select("id", "email").First(&user, actor).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "system.config.update", Target: "system_config:" + in.Key, Detail: fmt.Sprintf("revision=%d", revision+1)}).Error
	})
}
