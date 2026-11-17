package datastore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteObservationRetirementBudgetSchemaAuthorityAndOwnedCopy(t *testing.T) {
	assertObservationRetirementSchema(t, false)
}
func TestMySQLObservationRetirementBudgetSchemaAuthorityAndOwnedCopy(t *testing.T) {
	assertObservationRetirementSchema(t, true)
}
func assertObservationRetirementSchema(t *testing.T, mysql bool) {
	for _, loss := range []string{"budget-table", "budget-row", "retirement-table", "verification-table", "none"} {
		t.Run(loss, func(t *testing.T) {
			source := diagnosticSchemaDB(t, mysql)
			now := time.Now().UTC()
			p := network.EventPosition{ID: "retirement-schema-source", CoreInstanceID: "original-runtime", Sequence: 1, OccurredAt: now, ReceivedAt: now, AssetTag: strings.Repeat("a", 64)}
			input := network.ObservationSource{Observation: network.NodeObservation{NodeID: 9001, Latest: p}, Kind: "engine.warning", Payload: []byte("schema optional source")}
			var id string
			if e := source.Transaction(func(tx *gorm.DB) error { var e error; id, e = observationstore.Enqueue(tx, input); return e }); e != nil {
				t.Fatal(e)
			}
			var before model.NodeObservationMaterialBudget
			source.First(&before, "id = ?", 1)
			for range 2 {
				if e := datastore.RunMigrations(source); e != nil {
					t.Fatal(e)
				}
			}
			var after model.NodeObservationMaterialBudget
			source.First(&after, "id = ?", 1)
			if after != before {
				t.Fatal("repeat migration reseeded live budget", before, after)
			}
			target := diagnosticSchemaDB(t, mysql)
			if e := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
				tables, e := datastore.MigrationTables(tx)
				if e != nil {
					return e
				}
				if e := datastore.ClearCopyDestinationRows(tx, tables); e != nil {
					return e
				}
				return datastore.CopyApplicationData(source, tx)
			}); e != nil {
				t.Fatal(e)
			}
			var copied model.NodeObservationMaterialBudget
			target.First(&copied, "id = ?", 1)
			if copied != before {
				t.Fatal("copy changed budget", copied, before)
			}
			var original, copiedReceipt model.NodeObservationRetirement
			source.First(&original, "intent_id = ?", id)
			target.First(&copiedReceipt, "intent_id = ?", id)
			if copiedReceipt.HeaderSHA256 != original.HeaderSHA256 || copiedReceipt.RemainingBytes != original.RemainingBytes || copiedReceipt.SourceSummary != original.SourceSummary || copiedReceipt.State != original.State {
				t.Fatal("copy changed source/retirement authority", original, copiedReceipt)
			}
			switch loss {
			case "budget-table":
				if e := source.Exec("DROP TABLE node_observation_material_budget").Error; e != nil {
					t.Fatal(e)
				}
			case "budget-row":
				if e := source.Where("id = ?", 1).Delete(&model.NodeObservationMaterialBudget{}).Error; e != nil {
					t.Fatal(e)
				}
			case "retirement-table":
				if e := source.Exec("DROP TABLE node_observation_retirements").Error; e != nil {
					t.Fatal(e)
				}
			case "verification-table":
				if e := source.Exec("DROP TABLE node_observation_retirement_parts").Error; e != nil {
					t.Fatal(e)
				}
			case "none":
				return
			}
			if e := datastore.RunMigrations(source); e == nil {
				t.Fatal("recorded lost authority was recreated", loss)
			}
			if e := datastore.CopyApplicationData(source, target); e == nil {
				t.Fatal("copy ignored recorded source authority loss", loss)
			}
			var n int64
			if e := source.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Count(&n).Error; e != nil || n != 1 {
				t.Fatal("rejected recovery changed retained source", n, e)
			}
		})
	}
}
