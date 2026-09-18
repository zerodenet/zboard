package platformstore

import (
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// ApplyMigrationReview follows the authorized, audited manual Run resolution in
// the same transaction. It records the operator's verified target disposition;
// it neither retries the copy nor disables maintenance automatically.
func ApplyMigrationReview(tx *gorm.DB, in jobs.Review) error {
	var run jobstore.Record
	if err := tx.First(&run, "id = ?", in.RunID).Error; err != nil {
		return err
	}
	if run.Owner != "system" || run.Handler != "database_migration" {
		return nil
	}
	var input struct {
		TaskID uint `json:"task_id"`
	}
	if json.Unmarshal([]byte(run.Payload), &input) != nil || input.TaskID == 0 || run.Resource != jobs.MaintenanceResource || run.Key != fmt.Sprintf("database_migration:%d", input.TaskID) {
		return jobs.ErrOutcomeUnverified
	}
	var task model.Task
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND type = ?", input.TaskID, "database_migration").First(&task).Error; err != nil {
		return jobs.ErrOutcomeUnverified
	}
	status := int16(3)
	current := task.Current
	message := "operator verified: " + in.Reason
	if in.Outcome == jobs.Succeeded {
		status = 2
		current = task.Total
		message = ""
	}
	now := time.Now().UTC()
	if err := tx.Model(&task).Updates(map[string]interface{}{"status": status, "current": current, "errors": message, "content": platform.EncodeMigrationSecret(""), "finished_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&model.TaskItem{}).Where("task_id = ?", task.ID).Updates(map[string]interface{}{"status": status, "error": message, "finished_at": now}).Error
}
