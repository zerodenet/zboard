package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Failed retirements may refresh credentials for the same pinned host. They
// cannot rebind the target or clear host trust, and active/unknown work is fenced.
func retirementSSHRepairAllowed(tx *gorm.DB, node model.Node, change network.NodeSSHConfigurationChange) bool {
	if node.LifecycleStatus != "deleting" || change.ResetHostKey || node.SSHHost != change.SSHHost || node.SSHPort != change.SSHPort {
		return false
	}
	var retirement model.NodeRetirement
	if tx.First(&retirement, "node_id = ?", node.ID).Error != nil {
		return false
	}
	var run jobstore.Record
	if tx.First(&run, "id = ?", retirement.RunID).Error != nil {
		return false
	}
	return run.State == "failed" || run.State == "canceled"
}
