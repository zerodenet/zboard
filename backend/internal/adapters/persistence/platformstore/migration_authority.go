package platformstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func RequireMigrationAdministrator(db *gorm.DB, actor uint) error {
	var user model.User
	result := db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").Limit(1).Find(&user)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return jobs.ErrPermission
	}
	return nil
}

func MigrationReservationActive(db *gorm.DB) (bool, error) {
	var row struct{ Present int }
	result := db.Table("job_runs").Select("1 AS present").Where("owner = ? AND handler = ? AND resource = ? AND state IN ?", "system", "database_migration", jobs.MaintenanceResource, []jobs.State{jobs.Queued, jobs.Running, jobs.Unknown}).Limit(1).Scan(&row)
	return row.Present == 1, result.Error
}
