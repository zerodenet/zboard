package observabilitystore

import (
	"context"
	"database/sql"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"gorm.io/gorm"
)

// Fixed top ten, a single aggregate per dimension and names joined only after
// limiting. Read selected dimensions from the same reporting snapshot.
func (s Dashboard) LoadTrafficRankings(ctx context.Context, period observability.DashboardPeriod, selected ...string) (observability.DashboardTrafficRankings, error) {
	out := observability.DashboardTrafficRankings{Period: period, Nodes: []observability.DashboardTrafficRanking{}, Users: []observability.DashboardTrafficRanking{}, AsOf: time.Now().UTC()}
	err := s.readDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, dimension := range []struct {
			column, table, name, fallback string
			items                         *[]observability.DashboardTrafficRanking
		}{
			{"node_id", "nodes", "name", "节点", &out.Nodes}, {"user_id", "users", "email", "用户", &out.Users},
		} {
			if len(selected) > 0 && selected[0] != "" && ((selected[0] == "nodes" && dimension.column != "node_id") || (selected[0] == "users" && dimension.column != "user_id")) {
				continue
			}
			current := meteringstore.TrafficAggregateSource(tx, period.From, period.To, meteringstore.TrafficScope{}).Select(dimension.column + " AS id, used_bytes AS traffic_bytes, 0 AS previous_traffic_bytes, record_count AS current_count")
			previous := meteringstore.TrafficAggregateSource(tx, period.PreviousFrom, period.PreviousTo, meteringstore.TrafficScope{}).Select(dimension.column + " AS id, 0 AS traffic_bytes, used_bytes AS previous_traffic_bytes, 0 AS current_count")
			aggregate := tx.Table("(? UNION ALL ?) AS periods", current, previous).
				Select("id, SUM(traffic_bytes) AS traffic_bytes, SUM(previous_traffic_bytes) AS previous_traffic_bytes").
				Where("id > 0").Group("id").Having("SUM(current_count) > 0").
				Order("traffic_bytes DESC, id ASC").Limit(10)
			query := tx.Table("(?) AS ranking", aggregate).Select("ranking.*, COALESCE(NULLIF(TRIM(reference." + dimension.name + "), ''), '') AS name").Joins("LEFT JOIN " + dimension.table + " reference ON reference.id = ranking.id").Order("ranking.traffic_bytes DESC, ranking.id ASC")
			if err := query.Scan(dimension.items).Error; err != nil {
				return err
			}
			for i := range *dimension.items {
				if (*dimension.items)[i].Name == "" {
					(*dimension.items)[i].Name = dimension.fallback
				}
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, err
}
