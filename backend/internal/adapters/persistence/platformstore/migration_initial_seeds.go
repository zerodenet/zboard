package platformstore

import (
	"fmt"
	"regexp"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Receipt ownership alone does not prove that writes observed during the DDL
// interval belong to the initializer. Admit only the initializer's actual seed
// content before sealing the manifest. This guard is never an empty-DB waiver.
func validateInitialMigrationSeeds(db *gorm.DB) error {
	reject := func() error {
		return fmt.Errorf("%w: destination contains changed or non-initialization seed data; no rows were cleared", platform.ErrMigrationTargetOccupied)
	}
	if datastore.IsSQLite(db) {
		var configs []model.SystemConfig
		if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&configs).Error; err != nil {
			return err
		}
		if len(configs) != 1 {
			return reject()
		}
		config := configs[0]
		if config.ConfigKey != entitlements.RecoveryGraceDaysKey || config.Name != "订阅续费恢复窗口（天）" || config.Value != "7" || config.ValueType != "int" || config.Revision != 1 || config.IsPublic || config.IsSecret || config.Description != "到期后停服，可在此天数内续费恢复；默认7天，范围0–3650，0表示不保留恢复窗口。缩短窗口会使超过新期限的旧订阅结束并清理凭据，历史订单和账务保留。主动退订不进入恢复窗口。" {
			return reject()
		}
		for _, table := range []string{"email_templates", "subscription_templates"} {
			var count int64
			if err := db.Table(table).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return reject()
			}
		}
		var ledger []struct {
			ID       uint
			Revision uint64
			Instance string
		}
		if err := db.Table("traffic_ledger_revision").Limit(2).Find(&ledger).Error; err != nil {
			return err
		}
		if len(ledger) != 1 || ledger[0].ID != 1 || ledger[0].Revision != 0 || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(ledger[0].Instance) {
			return reject()
		}
	} else {
		matches, err := matchesMySQLMigrationSeeds(db)
		if err != nil {
			return err
		}
		if !matches {
			return reject()
		}
	}
	var statistics []model.PrincipalStatisticsState
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&statistics).Error; err != nil {
		return err
	}
	if len(statistics) != 1 {
		return reject()
	}
	state := statistics[0]
	if state.ID != 1 || state.LegacyUpperID != 0 || state.LegacyAfterID != 0 || state.LegacyExpectedRows != 0 || state.LegacyProjectedRows != 0 || state.ProjectedHead != 0 || state.Revision != 1 || state.CreatedAt.IsZero() || !state.CreatedAt.Equal(state.UpdatedAt) {
		return reject()
	}
	var flowStates []model.FlowStartStatisticsState
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&flowStates).Error; err != nil {
		return err
	}
	if len(flowStates) != 1 {
		return reject()
	}
	flowState := flowStates[0]
	if flowState.ID != 1 || flowState.LegacyUpperID != 0 || flowState.LegacyAfterID != 0 || flowState.LegacyExpectedRows != 0 || flowState.LegacyProjectedRows != 0 || flowState.ProjectedHead != 0 || flowState.Revision != 1 || flowState.CreatedAt.IsZero() || !flowState.CreatedAt.Equal(flowState.UpdatedAt) {
		return reject()
	}
	var observationBudget []model.NodeObservationMaterialBudget
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&observationBudget).Error; err != nil {
		return err
	}
	if len(observationBudget) != 1 || observationBudget[0].ID != 1 || observationBudget[0].RetainedBytes != 0 || observationBudget[0].Revision != 1 {
		return reject()
	}
	var budgets []jobstore.Budget
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&budgets).Error; err != nil {
		return err
	}
	if len(budgets) != 1 || budgets[0].ID != 1 || budgets[0].Capacity != 4 || budgets[0].Revision != 0 || budgets[0].DispatchVirtualTime != 0 {
		return reject()
	}
	var groups []jobstore.ExecutionGroup
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&groups).Error; err != nil {
		return err
	}
	if len(groups) != 1 || groups[0].ID != "external" || groups[0].Capacity != 3 {
		return reject()
	}
	var scans []model.AutomaticRenewalScan
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(2).Find(&scans).Error; err != nil {
		return err
	}
	if len(scans) != 1 || scans[0].ID != 1 || scans[0].Sequence != 0 {
		return reject()
	}
	matched, err := menustore.MatchesInitializationSeed(db)
	if err != nil {
		return err
	}
	if !matched {
		return reject()
	}
	return nil
}
