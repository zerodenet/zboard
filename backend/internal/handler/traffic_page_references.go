package handler

import "gorm.io/gorm"

type trafficPageReferences struct {
	Subscriptions map[string]entityReference `json:"subscriptions"`
	Nodes         map[string]entityReference `json:"nodes"`
}

// IDs come only from the already authorized and paginated usage rows. Chart
// rankings are deliberately not an entity directory. Even a misattributed raw
// row must never reveal the name of another account's subscription.
func accountTrafficPageReferences(db *gorm.DB, rows []trafficUsageBucket, userID uint) (trafficPageReferences, error) {
	subscriptions, nodes := make(map[uint]struct{}), make(map[uint]struct{})
	for _, row := range rows {
		if row.SubscriptionID > 0 {
			subscriptions[row.SubscriptionID] = struct{}{}
		}
		if row.NodeID > 0 {
			nodes[row.NodeID] = struct{}{}
		}
	}
	result := trafficPageReferences{Subscriptions: prefillEntityReferences("subscription", subscriptions), Nodes: prefillEntityReferences("node", nodes)}
	if err := resolveSubscriptionReferences(db.Where("subscriptions.user_id = ?", userID), result.Subscriptions, sortedEntityIDs(subscriptions)); err != nil {
		return result, err
	}
	if err := resolveNodeReferences(db, result.Nodes, sortedEntityIDs(nodes)); err != nil {
		return result, err
	}
	return result, nil
}
