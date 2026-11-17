package datastore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type flowStartStatisticsCopyProofKey struct{}
type flowStartStatisticsCopyProof struct{ Initialized, Consumed bool }

func captureFlowStartStatisticsCopyProof(tx *gorm.DB) (*gorm.DB, error) {
	proof := &flowStartStatisticsCopyProof{}
	tables, err := migrationTableSet(tx)
	if err != nil {
		return nil, err
	}
	if tables["flow_start_statistics_state"] && tables["schema_migrations"] {
		var receipt int64
		if err := tx.Table("schema_migrations").Where("version = ?", flowStartStatisticsVersion).Count(&receipt).Error; err != nil {
			return nil, err
		}
		var states []model.FlowStartStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&states).Error; err != nil {
			return nil, err
		}
		if receipt == 1 && len(states) == 1 {
			state := states[0]
			proof.Initialized = validateFlowStartStatisticsState(state) == nil && state.CreatedAt.Equal(state.UpdatedAt) && state.ID == 1 && state.LegacyUpperID == 0 && state.LegacyAfterID == 0 && state.LegacyExpectedRows == 0 && state.LegacyProjectedRows == 0 && state.ProjectedHead == 0 && state.Revision == 1
		}
		for _, table := range []string{"subscription_flow_start_events", "flow_start_statistics_pending", "flow_start_statistics_points", "flow_start_statistics_minutes"} {
			if !tables[table] {
				proof.Initialized = false
				continue
			}
			column := "source_id"
			if table == "subscription_flow_start_events" {
				column = "id"
			}
			if table == "flow_start_statistics_minutes" {
				column = "subscription_id"
			}
			var heads []map[string]interface{}
			q := tx.Table(table).Clauses(clause.Locking{Strength: "SHARE"}).Select(column).Limit(1).Find(&heads)
			if q.Error != nil {
				return nil, q.Error
			}
			if q.RowsAffected > 0 {
				proof.Initialized = false
			}
		}
	}
	return tx.WithContext(context.WithValue(tx.Statement.Context, flowStartStatisticsCopyProofKey{}, proof)), nil
}
func requireFlowStartStatisticsCopyProof(tx *gorm.DB) (*flowStartStatisticsCopyProof, error) {
	proof, ok := tx.Statement.Context.Value(flowStartStatisticsCopyProofKey{}).(*flowStartStatisticsCopyProof)
	if !ok || proof == nil || !proof.Initialized || proof.Consumed {
		return nil, errors.New("legacy flow-start statistics copy requires original owned empty coverage initialization")
	}
	return proof, nil
}
