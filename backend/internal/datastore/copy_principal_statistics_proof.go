package datastore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type principalStatisticsCopyProofKey struct{}
type principalStatisticsCopyProof struct{ Initialized, Consumed bool }

func capturePrincipalStatisticsCopyProof(tx *gorm.DB) (*gorm.DB, error) {
	proof := &principalStatisticsCopyProof{}
	tables, err := migrationTableSet(tx)
	if err != nil {
		return nil, err
	}
	if tables["principal_statistics_state"] && tables["schema_migrations"] {
		var receipt int64
		if err := tx.Table("schema_migrations").Where("version = ?", principalStatisticsVersion).Count(&receipt).Error; err != nil {
			return nil, err
		}
		var states []model.PrincipalStatisticsState
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&states).Error; err != nil {
			return nil, err
		}
		if receipt == 1 && len(states) == 1 {
			state := states[0]
			proof.Initialized = validatePrincipalStatisticsState(state) == nil && state.CreatedAt.Equal(state.UpdatedAt) && state.ID == 1 && state.LegacyUpperID == 0 && state.LegacyAfterID == 0 && state.LegacyExpectedRows == 0 && state.LegacyProjectedRows == 0 && state.ProjectedHead == 0 && state.Revision == 1
		}
		for _, table := range []string{"principal_flow_scope_observations", "principal_statistics_pending", "principal_statistics_samples", "principal_statistics_hourly", "principal_statistics_minutes"} {
			if !tables[table] {
				proof.Initialized = false
				continue
			}
			column := "observation_id"
			if table == "principal_flow_scope_observations" {
				column = "id"
			}
			if table == "principal_statistics_hourly" || table == "principal_statistics_minutes" {
				column = "scope_id"
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
	return tx.WithContext(context.WithValue(tx.Statement.Context, principalStatisticsCopyProofKey{}, proof)), nil
}
func requirePrincipalStatisticsCopyProof(tx *gorm.DB) (*principalStatisticsCopyProof, error) {
	proof, ok := tx.Statement.Context.Value(principalStatisticsCopyProofKey{}).(*principalStatisticsCopyProof)
	if !ok || proof == nil || !proof.Initialized || proof.Consumed {
		return nil, errors.New("legacy principal statistics copy requires original owned empty coverage initialization")
	}
	return proof, nil
}
