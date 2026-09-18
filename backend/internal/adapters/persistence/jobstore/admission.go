package jobstore

import (
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// The caller holds the deployment-wide budget lock before reading or inserting.
func checkPendingAdmission(tx *gorm.DB, owner, resource string) error {
	// Maintenance must remain reachable when ordinary producers fill the queue.
	// At most one queued maintenance request may use the reserved slot.
	var count int64
	pendingStates := []jobs.State{jobs.Queued, jobs.RetryWait}
	q := tx.Model(&Record{}).Where("state IN ?", pendingStates)
	if resource == jobs.MaintenanceResource {
		if err := q.Where("resource = ?", resource).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return jobs.ErrBackpressure
		}
		return nil
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count >= jobs.MaxPending {
		return jobs.ErrBackpressure
	}
	if !strings.HasPrefix(owner, "plugin:") {
		return nil
	}
	if err := tx.Model(&Record{}).Where("state IN ? AND owner LIKE ?", pendingStates, "plugin:%").Count(&count).Error; err != nil {
		return err
	}
	if count >= jobs.MaxPluginPending {
		return jobs.ErrBackpressure
	}
	if err := tx.Model(&Record{}).Where("state IN ? AND owner = ?", pendingStates, owner).Count(&count).Error; err != nil {
		return err
	}
	if count >= jobs.MaxPluginOwnerPending {
		return jobs.ErrBackpressure
	}
	return nil
}
