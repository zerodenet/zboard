package networkstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

type ObservationIntentOperations struct{ DB *gorm.DB }

func (s ObservationIntentOperations) ListObservationIntents(ctx context.Context, actor uint, input network.ObservationIntentQuery) (out network.ObservationIntentPage, err error) {
	q, after, err := input.Parse()
	if err != nil {
		return out, err
	}
	if s.DB == nil {
		return out, network.ErrNodeAdministrationUnavailable
	}
	out = network.ObservationIntentPage{Status: q.Status, Items: []network.ObservationIntentItem{}}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Same current active-administrator DB predicate as node administration;
		// no reliance on a stale browser/JWT administrator bit alone.
		if _, err := nodeAdministrator(tx, actor); err != nil {
			return err
		}
		match := "current_node.asset_tag = intents.asset_tag"
		stateMatch := "state.asset_tag = intents.asset_tag"
		if tx.Dialector.Name() == "mysql" {
			match = "BINARY current_node.asset_tag = BINARY intents.asset_tag"
			// The plain equality is a necessary condition of byte equality and
			// exposes both composite-PK columns without weakening the binary fence.
			// Actual optimizer use still requires an observed execution plan.
			stateMatch = "state.asset_tag = intents.asset_tag AND BINARY state.asset_tag = BINARY intents.asset_tag"
		}
		relation := "CASE WHEN intents.asset_tag = '' THEN 'unknown' WHEN state.deleted = 1 THEN 'asset_deleted' WHEN current_node.id IS NULL THEN 'node_missing' WHEN current_node.asset_tag = '' THEN 'unknown' WHEN " + match + " THEN 'same_asset' ELSE 'different_asset' END AS asset_relation"
		query := tx.Table("node_observation_intents AS intents").Select("intents.id,intents.node_id,intents.status,intents.reason,intents.attempts,intents.material_bytes,intents.part_count,intents.created_at AS enqueued_at,intents.available_at,intents.updated_at AS disposition_updated_at,intents.lease_until,"+relation).
			Joins("LEFT JOIN nodes AS current_node ON current_node.id = intents.node_id").
			Joins("LEFT JOIN node_observation_states AS state ON state.node_id = intents.node_id AND "+stateMatch).
			Where("intents.status = ?", q.Status)
		if after != nil {
			query = query.Where("(intents.available_at > ? OR (intents.available_at = ? AND intents.id > ?))", after.AvailableAt, after.AvailableAt, after.ID)
		}
		// Explicit columns only. Never load model headers with token/asset fields,
		// never select parts or join through the existence of a current node.
		var rows []network.ObservationIntentItem
		if err := query.Order("intents.available_at ASC,intents.id ASC").Limit(q.Limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		out.HasMore = len(rows) > q.Limit
		if out.HasMore {
			rows = rows[:q.Limit]
		}
		for _, r := range rows {
			item := r
			if item.Status != "claimed" {
				item.LeaseUntil = nil
			}
			out.Items = append(out.Items, item)
		}
		if out.HasMore {
			last := out.Items[len(out.Items)-1]
			cursor, err := network.ObservationIntentNextCursor(q.Status, last.AvailableAt, last.ID)
			if err != nil {
				return errors.New("invalid stored observation pagination position")
			}
			out.NextCursor = cursor
		}
		return nil
	})
	if err != nil {
		return network.ObservationIntentPage{}, err
	}
	return out, nil
}
