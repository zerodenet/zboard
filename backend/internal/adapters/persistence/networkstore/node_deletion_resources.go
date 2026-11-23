package networkstore

import (
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func finalizeManagedNodeDeletion(tx *gorm.DB, id uint) error {
	var certs []uint
	if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", id).Pluck("id", &certs).Error; err != nil {
		return err
	}
	if len(certs) > 0 {
		if err := tx.Where("managed_certificate_id IN ?", certs).Delete(&model.CertificateProtocolEndpoint{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.ManagedCertificate{}).Where("node_id = ?", id).Updates(map[string]any{"status": "deleted", "auto_renew": false, "provider_account_id": nil}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id = ?", id).Delete(&model.ManagedDNSRecord{}).Error; err != nil {
		return err
	}
	return nil
}

// The caller holds the job ledger lock. Record the panel-only removal in the
// same transaction so the next node deletion attempt can skip the missing DNS.
func keepDNSInStoppedNodeDeletion(tx *gorm.DB, nodeID, recordID uint) (bool, error) {
	var row model.NodeRetirement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "node_id = ?", nodeID).Error; err != nil {
		return false, err
	}
	if row.Mode != "delete" {
		return false, nil
	}
	var run jobstore.Record
	if err := tx.First(&run, "id = ?", row.RunID).Error; err != nil {
		return false, err
	}
	if jobs.State(run.State) != jobs.Failed && jobs.State(run.State) != jobs.Canceled {
		return false, nil
	}
	if err := validateRetirementPlan(row); err != nil {
		return false, err
	}
	steps := retirementView(row).Steps
	for i := range steps {
		step := &steps[i]
		if step.Kind != "dns" || step.ID != recordID {
			continue
		}
		if step.State == "succeeded" || step.State == "retained" {
			return true, nil
		}
		if step.State == "failed" && row.LastError == step.Message {
			row.LastError = ""
		}
		step.State = "retained"
		step.Message = "管理员已删除这条 DNS 的面板记录，跳过供应商清理。"
		encoded, _ := json.Marshal(steps)
		err := tx.Model(&row).Updates(map[string]any{"steps": string(encoded), "last_error": row.LastError}).Error
		return err == nil, err
	}
	return false, nil
}
