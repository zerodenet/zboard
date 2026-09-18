package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sort"
	"strings"
)

func usageReferences(db *gorm.DB, rows []metering.UsageRow, actor uint) ([]metering.UsageReference, error) {
	sets := map[string]map[uint]bool{"subscription": {}, "node": {}}
	for _, row := range rows {
		if row.SubscriptionID > 0 {
			sets["subscription"][row.SubscriptionID] = true
		}
		if row.NodeID > 0 {
			sets["node"][row.NodeID] = true
		}
	}
	result := make([]metering.UsageReference, 0)
	for _, kind := range []string{"subscription", "node"} {
		ids := make([]uint, 0, len(sets[kind]))
		for id := range sets[kind] {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if len(ids) == 0 {
			continue
		}
		refs := map[uint]metering.UsageReference{}
		if kind == "subscription" {
			var values []struct {
				ID                        uint
				Status, PlanName, SKUName string
			}
			if err := db.Table("subscriptions").Select("subscriptions.id AS id, subscriptions.status AS status, plans.name AS plan_name, plan_skus.name AS sku_name").Joins("LEFT JOIN plans ON plans.id = subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id = subscriptions.plan_sku_id").Where("subscriptions.user_id = ? AND subscriptions.id IN ?", actor, ids).Scan(&values).Error; err != nil {
				return nil, err
			}
			for _, v := range values {
				refs[v.ID] = metering.UsageReference{ID: v.ID, Kind: kind, Name: strings.TrimSpace(v.PlanName), Secondary: strings.TrimSpace(v.SKUName), Status: v.Status}
			}
		} else {
			var values []model.Node
			if err := db.Select("id, name, region, lifecycle_status").Where("id IN ?", ids).Find(&values).Error; err != nil {
				return nil, err
			}
			for _, v := range values {
				refs[v.ID] = metering.UsageReference{ID: v.ID, Kind: kind, Name: strings.TrimSpace(v.Name), Secondary: strings.TrimSpace(v.Region), Status: v.LifecycleStatus}
			}
		}
		for _, id := range ids {
			ref, ok := refs[id]
			if !ok {
				ref = metering.UsageReference{ID: id, Kind: kind, Missing: true}
			}
			result = append(result, ref)
		}
	}
	return result, nil
}
