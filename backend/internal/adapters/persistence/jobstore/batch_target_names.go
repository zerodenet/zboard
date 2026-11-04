package jobstore

import (
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Resolve the requested page in groups, never one identity request per item.
// These are read projections, not additional retained copies of user data.
func batchTargetNames(tx *gorm.DB, items []jobs.BatchItemView) error {
	labels := map[string]string{"user": "用户", "subscription": "订阅", "node": "节点", "protocol_endpoint": "协议服务", "node_group": "节点组"}
	ids := map[string]map[uint]struct{}{}
	for _, item := range items {
		if labels[item.TargetType] == "" {
			continue
		}
		id, err := strconv.ParseUint(item.TargetID, 10, 64)
		if err != nil || id == 0 || uint64(uint(id)) != id {
			continue
		}
		if ids[item.TargetType] == nil {
			ids[item.TargetType] = map[uint]struct{}{}
		}
		ids[item.TargetType][uint(id)] = struct{}{}
	}
	type identity struct {
		ID                                      uint
		Name, Description, AccountName, SKUName string
	}
	resolved := map[string]map[uint]identity{}
	for kind, set := range ids {
		keys := make([]uint, 0, len(set))
		for id := range set {
			keys = append(keys, id)
		}
		query := tx
		switch kind {
		case "user":
			query = query.Unscoped().Model(&model.User{}).Select("id,email AS name,admin_note AS description,account_name")
		case "node":
			query = query.Unscoped().Model(&model.Node{}).Select("id,name")
		case "protocol_endpoint":
			query = query.Unscoped().Model(&model.ProtocolEndpoint{}).Select("id,name")
		case "node_group":
			query = query.Model(&model.NodeGroup{}).Select("id,name")
		case "subscription":
			query = query.Table("subscriptions").Select("subscriptions.id,plans.name AS name,users.email AS description,plan_skus.name AS sku_name").Joins("LEFT JOIN plans ON plans.id=subscriptions.plan_id").Joins("LEFT JOIN plan_skus ON plan_skus.id=subscriptions.plan_sku_id").Joins("LEFT JOIN users ON users.id=subscriptions.user_id")
		}
		resolved[kind] = map[uint]identity{}
		// Legacy detail reads can contain 10000 items; bound IN clauses as well as
		// the regular 100-row page without turning this into an N+1 lookup.
		for start := 0; start < len(keys); start += 500 {
			end := min(start+500, len(keys))
			column := "id"
			if kind == "subscription" {
				column = "subscriptions.id"
			}
			var rows []identity
			if err := query.Session(&gorm.Session{}).Where(column+" IN ?", keys[start:end]).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				resolved[kind][row.ID] = row
			}
		}
	}
	for i := range items {
		item := &items[i]
		kind := item.TargetType
		if labels[kind] == "" {
			continue
		}
		id, err := strconv.ParseUint(item.TargetID, 10, 64)
		if err != nil || id == 0 || uint64(uint(id)) != id {
			continue
		}
		row, ok := resolved[kind][uint(id)]
		if !ok {
			item.TargetName = labels[kind] + "已删除"
			item.TargetMissing = true
			continue
		}
		item.TargetName = strings.TrimSpace(row.Name)
		if item.TargetName == "" {
			item.TargetName = strings.TrimSpace(row.AccountName)
		}
		if item.TargetName == "" {
			item.TargetName = "未命名" + labels[kind]
		}
		description := []string{}
		for _, value := range []string{row.Description, row.SKUName} {
			value = strings.TrimSpace(value)
			if value != "" && value != item.TargetName {
				description = append(description, value)
			}
		}
		item.TargetDescription = strings.Join(description, " · ")
	}
	return nil
}
