package jobstore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"gorm.io/gorm"
)

// The schedule and execution ledger share the admission lock. Reuse only a clean
// terminal run with one successful/yielded attempt. Never erase retry/failure or
// unknown evidence. A fresh lease token fences completions from every earlier tick.
func (s *Store) reusePeriodicSuccess(tx *gorm.DB, schedule Schedule, next *Record) (bool, error) {
	if !schedule.CompactHistory || schedule.CompactRunID == "" {
		return false, nil
	}
	var old Record
	err := tx.First(&old, "id = ?", schedule.CompactRunID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if old.ScheduleID != schedule.ID || old.Owner != schedule.Owner || old.Handler != schedule.Handler || old.AttemptCount != 1 || old.FinishedAt == nil || (old.State != string(jobs.Succeeded) && old.State != string(jobs.Yielded)) {
		return false, nil
	}
	var attempt Attempt
	err = tx.Where("run_id = ? AND attempt_number = 1", old.ID).Take(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if attempt.Token != old.Token || attempt.State != old.State || attempt.FinishedAt == nil {
		return false, nil
	}
	if err := checkPendingAdmission(tx, next.Owner, next.Resource); err != nil {
		return false, err
	}
	next.ID = old.ID
	if err := tx.Where("token = ?", attempt.Token).Delete(&Attempt{}).Error; err != nil {
		return false, err
	}
	// Select all fields so zero attempt count and nil old lease/finish are cleared.
	if err := tx.Model(&Record{}).Where("id = ?", old.ID).Select("*").Updates(next).Error; err != nil {
		return false, err
	}
	return true, nil
}
