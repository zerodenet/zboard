package platformstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMigrationCopierNodeObservationHandoff(t *testing.T) { observationCopyMatrix(t, false, false) }
func TestMySQLMigrationCopierNodeObservationHandoff(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) { observationCopyMatrix(t, !targetMySQL, targetMySQL) })
	}
}
func observationCopyMatrix(t *testing.T, sourceMySQL, targetMySQL bool) {
	for _, mode := range []string{"current_pending_held_tombstone", "retired_receipt", "legacy_unattributed_floor", "recorded_material_loss", "recorded_marker_loss", "recorded_retirement_header_loss", "owned_completion_rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := newMigrationCopyFixture(t, sourceMySQL, targetMySQL)
			node := model.Node{Name: "handoff-copy", Config: "{}", IsEnabled: true}
			if err := f.source.Create(&node).Error; err != nil {
				t.Fatal(err)
			}
			at := time.Unix(1700000000, 0).UTC()
			p := network.EventPosition{AssetTag: node.AssetTag, ID: "copy-stats", CoreInstanceID: "exact-runtime", Sequence: 99, OccurredAt: at, ReceivedAt: at}
			source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 3}}, Kind: "stats.sampled", Payload: []byte(`{"active_sessions":3,"bytes_up":0,"bytes_down":0,"raw":"\u0000\u00ff"}`)}
			original := source
			var originalID string
			if err := f.source.Transaction(func(tx *gorm.DB) error {
				var err error
				originalID, err = observationstore.Enqueue(tx, source)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// One applied source and its tombstone, one held unknown source, one pending
			// source prove that no disposition/lease/reference is silently regenerated.
			if _, err := (observationstore.Worker{DB: f.source}).Drain(context.Background(), 32, observationstore.MaxDrainBytes); err != nil {
				t.Fatal(err)
			}
			if mode == "retired_receipt" {
				newer := source
				next := p
				next.ID = "copy-newer"
				next.Sequence = 100
				newer.Observation.Latest = next
				newer.Observation.StatsEvent = &next
				if err := f.source.Transaction(func(tx *gorm.DB) error { _, err := observationstore.Enqueue(tx, newer); return err }); err != nil {
					t.Fatal(err)
				}
				if _, err := (observationstore.Worker{DB: f.source}).Drain(t.Context(), 32, observationstore.MaxDrainBytes); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC().Add(8 * 24 * time.Hour)
				if out, err := (observationstore.MaterialRetention{DB: f.source, Now: func() time.Time { return now }}).Retire(t.Context(), 32, observationstore.MaxDrainBytes); err != nil || out.Retired != 1 {
					t.Fatal("real compact source setup", out, err)
				}
			}
			if err := f.source.Transaction(func(tx *gorm.DB) error { return observationstore.Tombstone(tx, node.ID, node.AssetTag) }); err != nil {
				t.Fatal(err)
			}
			unknown := source
			unknown.Observation.Latest.AssetTag = ""
			unknown.Observation.StatsEvent = nil
			unknown.Kind = "legacy"
			if err := f.source.Transaction(func(tx *gorm.DB) error { _, err := observationstore.Enqueue(tx, unknown); return err }); err != nil {
				t.Fatal(err)
			}
			if _, err := (observationstore.Worker{DB: f.source}).Drain(context.Background(), 32, observationstore.MaxDrainBytes); err != nil {
				t.Fatal(err)
			}
			source.Observation.Latest.ID = "pending"
			source.Observation.Latest.Sequence = 100
			source.Observation.StatsEvent = nil
			source.Kind = "flow.updated"
			if err := f.source.Transaction(func(tx *gorm.DB) error { _, err := observationstore.Enqueue(tx, source); return err }); err != nil {
				t.Fatal(err)
			}
			var damagedBudgetBefore model.NodeObservationMaterialBudget
			var damagedPartsBefore int64
			if mode == "recorded_retirement_header_loss" {
				if err := f.source.First(&damagedBudgetBefore, "id = ?", 1).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.source.Model(&model.NodeObservationPart{}).Count(&damagedPartsBefore).Error; err != nil || damagedPartsBefore == 0 {
					t.Fatal("recorded loss fixture lacks surviving material", err)
				}
			}
			if mode == "legacy_unattributed_floor" {
				if err := f.source.Exec("DELETE FROM schema_migrations WHERE version IN ?", []string{"0060_node_observation_handoff.up.sql", "0061_node_observation_material_retention.up.sql", "0067_node_observation_retirement_budget.up.sql"}).Error; err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"node_observation_retirement_parts", "node_observation_retirements", "node_observation_material_budget", "node_observation_states", "node_observation_parts", "node_observation_intents"} {
					if err := f.source.Migrator().DropTable(table); err != nil {
						t.Fatal(err)
					}
				}
				if err := f.source.Table("zero_event_node_cursors").Create(map[string]any{"node_id": node.ID, "core_instance_id": "unattributed", "sequence": 88, "config_revision": 1, "occurred_at": at, "updated_at": at}).Error; err != nil {
					t.Fatal(err)
				}
			} else if mode == "recorded_retirement_header_loss" {
				// A recorded0067 source really lost its headers. This remains
				// refusal, never the genuinely pre0060 fixture branch above.
				if err := f.source.Migrator().DropTable("node_observation_intents"); err != nil {
					t.Fatal(err)
				}
			} else if mode == "recorded_marker_loss" {
				if err := f.source.Migrator().DropIndex("node_observation_intents", "idx_observation_material_review"); err != nil {
					t.Fatal(err)
				}
				if err := f.source.Migrator().DropColumn("node_observation_intents", "material_retired_at"); err != nil {
					t.Fatal(err)
				}
			} else if mode == "recorded_material_loss" {
				if err := f.source.Migrator().DropColumn("node_observation_parts", "data"); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func(db *gorm.DB) any {
				t.Helper()
				var h []model.NodeObservationIntent
				var p []model.NodeObservationPart
				var s []model.NodeObservationState
				if err := db.Order("id").Find(&h).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Order("intent_id,ordinal").Find(&p).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Order("node_id,asset_tag").Find(&s).Error; err != nil {
					t.Fatal(err)
				}
				// SQL drivers can return distinct Location objects for the same UTC
				// instant. Canonicalize location only: preserve all timestamp precision,
				// bytes, dispositions, leases and identity fields in the comparison.
				for i := range h {
					h[i].AvailableAt = h[i].AvailableAt.UTC()
					h[i].LeaseUntil = h[i].LeaseUntil.UTC()
					h[i].CreatedAt = h[i].CreatedAt.UTC()
					h[i].UpdatedAt = h[i].UpdatedAt.UTC()
					h[i].MaterialReviewAt = h[i].MaterialReviewAt.UTC()
					if h[i].MaterialRetiredAt != nil {
						v := h[i].MaterialRetiredAt.UTC()
						h[i].MaterialRetiredAt = &v
					}
				}
				for i := range s {
					s[i].UpdatedAt = s[i].UpdatedAt.UTC()
					for _, value := range []**time.Time{&s[i].CounterWrittenAt, &s[i].LatestOccurredAt, &s[i].LatestReceivedAt, &s[i].StatsOccurredAt, &s[i].StatsReceivedAt} {
						if *value != nil {
							utc := (*value).UTC()
							*value = &utc
						}
					}
				}
				return []any{h, p, s}
			}
			var before any
			if mode != "legacy_unattributed_floor" && mode != "recorded_material_loss" && mode != "recorded_marker_loss" && mode != "recorded_retirement_header_loss" {
				before = snapshot(f.source)
			}
			injected := errors.New("owned handoff completion failure")
			copier := f.copier(false)
			if mode == "owned_completion_rollback" {
				copier = MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
					if err := prepareMigrationCopyTestSchema(db); err != nil {
						return err
					}
					return db.Callback().Update().Before("gorm:update").Register("test:handoff_completion", func(tx *gorm.DB) {
						values, ok := tx.Statement.Dest.(map[string]any)
						if tx.Statement.Table != migrationPreparationTable || !ok || values["state"] != "completed" {
							return
						}
						var n int64
						if err := tx.Session(&gorm.Session{NewDB: true}).Model(&model.NodeObservationIntent{}).Count(&n).Error; err != nil {
							tx.AddError(err)
						} else if n != 3 {
							tx.AddError(errors.New("owned transaction cannot see three copied sources"))
						} else {
							tx.AddError(injected)
						}
					})
				}}
			}
			err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID)
			target := f.destination(t)
			if mode == "recorded_material_loss" || mode == "recorded_marker_loss" || mode == "recorded_retirement_header_loss" || mode == "owned_completion_rollback" {
				if err == nil {
					t.Fatal("lost material/owned completion accepted")
				}
				if mode == "recorded_retirement_header_loss" && !strings.Contains(err.Error(), "source headers missing") {
					t.Fatal("recorded header loss was not classified as lost authority", err)
				}
				if mode == "recorded_retirement_header_loss" {
					var sourceBudget, targetBudget model.NodeObservationMaterialBudget
					var surviving int64
					if err := f.source.First(&sourceBudget, "id = ?", 1).Error; err != nil || sourceBudget != damagedBudgetBefore {
						t.Fatal("copy refusal rewrote source budget", sourceBudget, err)
					}
					if err := f.source.Model(&model.NodeObservationPart{}).Count(&surviving).Error; err != nil || surviving != damagedPartsBefore {
						t.Fatal("copy refusal discarded surviving source material", surviving, err)
					}
					if err := target.First(&targetBudget, "id = ?", 1).Error; err != nil || targetBudget.RetainedBytes != 0 || targetBudget.Revision != 1 {
						t.Fatal("copy refusal changed owned target seed", targetBudget, err)
					}
				}
				if mode == "owned_completion_rollback" && !errors.Is(err, injected) {
					t.Fatal("missing intended same-TX failure", err)
				}
				var n int64
				if err := target.Model(&model.NodeObservationIntent{}).Count(&n).Error; err != nil || n != 0 {
					t.Fatal("failed copy left partial source history", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "retired_receipt" {
					if err := target.Transaction(func(tx *gorm.DB) error {
						got, err := observationstore.Enqueue(tx, original)
						if err == nil && got != originalID {
							return fmt.Errorf("compact copied replay changed identity")
						}
						return err
					}); err != nil {
						t.Fatal("copied compact receipt cannot replay", err)
					}
				}
				if mode == "legacy_unattributed_floor" {
					var floor model.NodeObservationState
					if err := target.First(&floor, "node_id = ? AND asset_tag = ?", node.ID, "").Error; err != nil || floor.FloorReference == "" || floor.StatsReference != "" || floor.LatestReference != "" || floor.CurrentKnown {
						t.Fatal("legacy copy forged attributed position", floor, err)
					}
					var budget model.NodeObservationMaterialBudget
					var retained struct{ Bytes uint64 }
					expression := "OCTET_LENGTH(data)"
					if datastore.IsSQLite(target) {
						expression = "length(CAST(data AS BLOB))"
					}
					if err := target.Model(&model.NodeObservationPart{}).Select("COALESCE(SUM(" + expression + "),0) AS bytes").Scan(&retained).Error; err != nil {
						t.Fatal(err)
					}
					if err := target.First(&budget, "id = ?", 1).Error; err != nil || retained.Bytes == 0 || budget.RetainedBytes != retained.Bytes {
						t.Fatal("owned imported legacy floor was given a false zero budget", budget, retained, err)
					}

				} else if !reflect.DeepEqual(before, snapshot(target)) {
					t.Fatal("copy changed material/position/ownership/disposition")
				}
			}
			if before != nil && !reflect.DeepEqual(before, snapshot(f.source)) {
				t.Fatal("copy modified original observation authority")
			}
			assertMigrationCopySourcePreserved(t, f)
		})
	}
}
