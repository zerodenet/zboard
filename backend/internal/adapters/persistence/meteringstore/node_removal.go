package meteringstore

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// RemoveNode clears current projections within the caller's resource transaction.
// Historical observations remain available for trend replay.
func RemoveNode(tx *gorm.DB, nodeID uint) (int64, int64, error) {
	var count int64
	if err := tx.Model(&PrincipalFlowCurrent{}).Where("node_id = ?", nodeID).Count(&count).Error; err != nil {
		return 0, 0, err
	}
	now := time.Now().UTC()
	if err := ResetNodeProjection(tx, nodeID, "", fmt.Sprintf("node-delete-%d-%d", nodeID, now.UnixMilli()), now, "node_deleted"); err != nil {
		return 0, 0, err
	}
	result := tx.Where("node_id = ?", nodeID).Delete(&PrincipalFlowNodeGeneration{})
	return count, result.RowsAffected, result.Error
}
