package datastore_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteLegacyObservationCopyMeasuresActualPartsAndRequiresOwnedSeed(t *testing.T) {
	assertLegacyObservationBudgetCopy(t, false)
}
func TestMySQLLegacyObservationCopyMeasuresActualPartsAndRequiresOwnedSeed(t *testing.T) {
	assertLegacyObservationBudgetCopy(t, true)
}
func assertLegacyObservationBudgetCopy(t *testing.T, mysql bool) {
	// Only this isolated fixture removes the newer authority to model a genuine
	// pre0067 source. Published migrations and real databases remain untouched.
	source := diagnosticSchemaDB(t, mysql)
	at := time.Now().UTC().Truncate(time.Millisecond)
	position := network.EventPosition{AssetTag: strings.Repeat("a", 64), ID: "pre67-opaque-binary-source", CoreInstanceID: "pre67-runtime", Sequence: 1, OccurredAt: at, ReceivedAt: at}
	input := network.ObservationSource{Observation: network.NodeObservation{NodeID: 7001, Latest: position}, Kind: "engine.warning", Payload: append([]byte{0, 255, 17}, bytes.Repeat([]byte("binary-source"), 6000)...)}
	var id string
	if err := source.Transaction(func(tx *gorm.DB) error { var err error; id, err = observationstore.Enqueue(tx, input); return err }); err != nil {
		t.Fatal(err)
	}
	var original []model.NodeObservationPart
	if err := source.Where("intent_id = ?", id).Order("ordinal").Find(&original).Error; err != nil || len(original) < 2 {
		t.Fatal("actual multisegment fixture missing", len(original), err)
	}
	var actual uint64
	for _, part := range original {
		actual += uint64(len(part.Data))
	}
	if actual == 0 {
		t.Fatal("missing real retained bytes")
	}
	if err := source.Exec("DELETE FROM schema_migrations WHERE version IN ?", []string{"0067_node_observation_retirement_budget.up.sql", "0068_coverage_runtime_identity.up.sql", "0069_principal_runtime_identity.up.sql"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_observation_retirement_parts", "node_observation_retirements", "node_observation_material_budget"} {
		if err := source.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"commit", "initialization_rollback", "changed_budget_bytes", "changed_budget_revision", "lost_initial_budget", "no_copy_owner"} {
		t.Run(mode, func(t *testing.T) {
			target := diagnosticSchemaDB(t, mysql)
			switch mode {
			case "changed_budget_bytes":
				if err := target.Model(&model.NodeObservationMaterialBudget{}).Where("id = ?", 1).Update("retained_bytes", uint64(1)).Error; err != nil {
					t.Fatal(err)
				}
			case "changed_budget_revision":
				if err := target.Model(&model.NodeObservationMaterialBudget{}).Where("id = ?", 1).Update("revision", uint64(2)).Error; err != nil {
					t.Fatal(err)
				}
			case "lost_initial_budget":
				if err := target.Where("id = ?", 1).Delete(&model.NodeObservationMaterialBudget{}).Error; err != nil {
					t.Fatal(err)
				}
			}
			sentinel := errors.New("owned budget initialization interrupted")
			if mode == "initialization_rollback" {
				const hook = "test:legacy_budget_copy_failure"
				if err := target.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
					if tx.Statement.Table == "node_observation_material_budget" {
						tx.AddError(sentinel)
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer target.Callback().Create().Remove(hook)
			}
			copyFn := func(tx *gorm.DB) error {
				tables, err := datastore.MigrationTables(tx)
				if err != nil {
					return err
				}
				if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
					return err
				}
				return datastore.CopyApplicationData(source, tx)
			}
			var err error
			if mode == "no_copy_owner" {
				err = target.Transaction(copyFn)
			} else {
				err = datastore.WithCopyDestination(t.Context(), target, copyFn)
			}
			if mode != "commit" {
				if err == nil {
					t.Fatal("legacy parts copied without original owned initialization evidence")
				}
				if mode == "initialization_rollback" && !errors.Is(err, sentinel) {
					t.Fatal("did not reach actual owned initializer failure", err)
				}
				var parts int64
				if err := target.Model(&model.NodeObservationPart{}).Count(&parts).Error; err != nil || parts != 0 {
					t.Fatal("failed copy committed partial binary sources", parts, err)
				}
				var budget model.NodeObservationMaterialBudget
				got := target.First(&budget, "id = ?", 1)
				if mode == "lost_initial_budget" {
					if !errors.Is(got.Error, gorm.ErrRecordNotFound) {
						t.Fatal("lost recorded row was recreated", got.Error)
					}
					return
				}
				expectedBytes, expectedRevision := uint64(0), uint64(1)
				if mode == "changed_budget_bytes" {
					expectedBytes = 1
				}
				if mode == "changed_budget_revision" {
					expectedRevision = 2
				}
				if got.Error != nil || budget.RetainedBytes != expectedBytes || budget.Revision != expectedRevision {
					t.Fatal("rollback lost original target authority", budget, got.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var budget model.NodeObservationMaterialBudget
			if err := target.First(&budget, "id = ?", 1).Error; err != nil || budget.RetainedBytes != actual || budget.Revision != 1 {
				t.Fatal("legacy source imported as a false zero budget", budget, actual, err)
			}
			var copied []model.NodeObservationPart
			if err := target.Where("intent_id = ?", id).Order("ordinal").Find(&copied).Error; err != nil || len(copied) != len(original) {
				t.Fatal("copied binary parts missing", err)
			}
			for i, part := range copied {
				if part.IntentID != original[i].IntentID || part.Ordinal != original[i].Ordinal || !bytes.Equal(part.Data, original[i].Data) {
					t.Fatal("binary source changed", i)
				}
			}
			for range 2 {
				if err := datastore.RunMigrations(target); err != nil {
					t.Fatal("reopen of imported authority failed", err)
				}
			}
			if err := target.Transaction(func(tx *gorm.DB) error {
				got, err := observationstore.Enqueue(tx, input)
				if err == nil && got != id {
					return errors.New("legacy replay changed original identity")
				}
				return err
			}); err != nil {
				t.Fatal("imported legacy material could not be verified on replay", err)
			}
			var reopened model.NodeObservationMaterialBudget
			if err := target.First(&reopened, "id = ?", 1).Error; err != nil || reopened != budget {
				t.Fatal("restart reseeded legacy imported authority", reopened, err)
			}
		})
	}
	// Recorded source loss is never classified as this genuinely legacy case.
	if err := source.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)", "0067_node_observation_retirement_budget.up.sql", at).Error; err != nil {
		t.Fatal(err)
	}
	target := diagnosticSchemaDB(t, mysql)
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error { return datastore.CopyApplicationData(source, tx) }); err == nil || !strings.Contains(err.Error(), "recorded observation retirement authority") {
		t.Fatal("lost recorded source table treated as legacy", err)
	}
	var retained []model.NodeObservationPart
	if err := source.Where("intent_id = ?", id).Order("ordinal").Find(&retained).Error; err != nil || len(retained) != len(original) {
		t.Fatal(fmt.Sprintf("source altered during copy: %v", err))
	}
}
