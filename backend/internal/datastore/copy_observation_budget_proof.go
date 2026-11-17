package datastore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type copiedObservationBudgetProofKey struct{}
type copiedObservationBudgetProof struct{ Initialized, Consumed bool }

// Capture the original empty prepared target BEFORE any default rows can be
// cleared. This private transaction capability is never produced by startup,
// an HTTP payload, an arbitrary copy call or a missing recorded singleton.
func captureCopiedObservationBudgetProof(tx *gorm.DB) (*gorm.DB, error) {
	proof := &copiedObservationBudgetProof{}
	tables, err := migrationTableSet(tx)
	if err != nil {
		return nil, err
	}
	if tables["node_observation_material_budget"] {
		var rows []model.NodeObservationMaterialBudget
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&rows).Error; err != nil {
			return nil, err
		}
		proof.Initialized = len(rows) == 1 && rows[0].ID == 1 && rows[0].RetainedBytes == 0 && rows[0].Revision == 1
		if !tables["schema_migrations"] {
			proof.Initialized = false
		} else {
			var receipt int64
			if err := tx.Table("schema_migrations").Where("version = ?", observationRetirementVersion).Count(&receipt).Error; err != nil {
				return nil, err
			}
			proof.Initialized = proof.Initialized && receipt == 1
		}
		for _, table := range []string{"node_observation_intents", "node_observation_parts", "node_observation_retirements", "node_observation_retirement_parts"} {
			if !tables[table] {
				proof.Initialized = false
				continue
			}
			column := "intent_id"
			if table == "node_observation_intents" {
				column = "id"
			}
			var found []map[string]interface{}
			result := tx.Table(table).Clauses(clause.Locking{Strength: "SHARE"}).Select(column).Limit(1).Find(&found)
			if result.Error != nil {
				return nil, result.Error
			}
			if result.RowsAffected > 0 {
				proof.Initialized = false
			}
		}
	}
	return tx.WithContext(context.WithValue(tx.Statement.Context, copiedObservationBudgetProofKey{}, proof)), nil
}
func requireCopiedObservationBudgetProof(tx *gorm.DB) (*copiedObservationBudgetProof, error) {
	proof, ok := tx.Statement.Context.Value(copiedObservationBudgetProofKey{}).(*copiedObservationBudgetProof)
	if !ok || proof == nil || !proof.Initialized || proof.Consumed {
		return nil, errors.New("legacy observation copy requires its original owned empty budget initialization; do not recreate lost recorded authority")
	}
	return proof, nil
}
