package meteringstore

import (
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ResetNodeProjection(tx *gorm.DB, nodeID uint, coreInstanceID, eventID string, observedAt time.Time, source string) error {
	var currents []PrincipalFlowCurrent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("node_id,principal_key,user_id,subscription_id").Where("node_id = ?", nodeID).Find(&currents).Error; err != nil {
		return err
	}
	if len(currents) == 0 {
		return nil
	}
	users := make(map[uint]struct{})
	subscriptions := make(map[uint]struct{})
	for _, current := range currents {
		if current.UserID > 0 {
			users[current.UserID] = struct{}{}
		}
		if current.SubscriptionID > 0 {
			subscriptions[current.SubscriptionID] = struct{}{}
		}
	}
	if err := tx.Where("node_id = ?", nodeID).Delete(&PrincipalFlowCurrent{}).Error; err != nil {
		return err
	}
	userIDs := make([]uint, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	for _, userID := range userIDs {
		if err := RefreshScope(tx, ScopeUser, userID, nodeID, coreInstanceID, 0, eventID, observedAt, source); err != nil {
			return err
		}
	}
	subscriptionIDs := make([]uint, 0, len(subscriptions))
	for id := range subscriptions {
		subscriptionIDs = append(subscriptionIDs, id)
	}
	sort.Slice(subscriptionIDs, func(i, j int) bool { return subscriptionIDs[i] < subscriptionIDs[j] })
	for _, subscriptionID := range subscriptionIDs {
		if err := RefreshScope(tx, ScopeSubscription, subscriptionID, nodeID, coreInstanceID, 0, eventID, observedAt, source); err != nil {
			return err
		}
	}
	return nil
}

func RefreshScope(tx *gorm.DB, scopeType string, scopeID, nodeID uint, coreInstanceID string, revision uint64, eventID string, observedAt time.Time, source string) error {
	if scopeID == 0 {
		return nil
	}
	query := tx.Model(&PrincipalFlowCurrent{})
	switch scopeType {
	case ScopeUser:
		query = query.Where("user_id = ?", scopeID)
	case ScopeSubscription:
		query = query.Where("subscription_id = ?", scopeID)
	default:
		return fmt.Errorf("unsupported Principal flow scope %q", scopeType)
	}
	var total struct {
		ActiveFlows uint64 `gorm:"column:active_flows"`
	}
	if err := query.Select("COALESCE(SUM(active_flows), 0) AS active_flows").Scan(&total).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := recordPrincipalBucket(tx, scopeType, scopeID, observedAt, total.ActiveFlows, 1, now); err != nil {
		return err
	}
	current := PrincipalFlowScopeCurrent{ScopeType: scopeType, ScopeID: scopeID, ActiveFlows: total.ActiveFlows, UpdatedAt: now}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}}, DoUpdates: clause.AssignmentColumns([]string{"active_flows", "updated_at"})}).Create(&current).Error
}
