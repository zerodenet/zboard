package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CommerceGroupAvailability is a read projection for product publication within
// the caller's transaction. Only explicit protocol membership grants access.
func CommerceGroupAvailability(tx *gorm.DB, id uint) (exists, enabled, hasEndpoint bool, err error) {
	var group model.NodeGroup
	result := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "is_enabled").Where("id = ?", id).Limit(1).Find(&group)
	if result.Error != nil {
		return false, false, false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, false, false, nil
	}
	if !group.IsEnabled {
		return true, false, false, nil
	}
	var endpoints []struct{ ID uint }
	err = tx.Table("node_group_endpoints").Clauses(clause.Locking{Strength: "SHARE"}).Select("protocol_endpoints.id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.is_active = ?", id, true).Limit(1).Find(&endpoints).Error
	return true, true, len(endpoints) > 0, err
}
