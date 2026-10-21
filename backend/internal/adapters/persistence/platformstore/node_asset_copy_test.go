package platformstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestMigrationCopierNodeAssetFoundation(t *testing.T) { nodeAssetCopyMatrix(t, false, false) }
func TestMySQLMigrationCopierNodeAssetFoundation(t *testing.T) {
	for _, targetMySQL := range []bool{false, true} {
		t.Run(fmt.Sprint(targetMySQL), func(t *testing.T) { nodeAssetCopyMatrix(t, !targetMySQL, targetMySQL) })
	}
}
func nodeAssetCopyMatrix(t *testing.T, sourceMySQL, targetMySQL bool) {
	for _, mode := range []string{"current_including_unknown", "legacy_future_labels", "recorded_column_loss", "legacy_completion_rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := newMigrationCopyFixture(t, sourceMySQL, targetMySQL)
			now := time.Now().UTC().Truncate(time.Millisecond)
			nodes := []model.Node{{Name: "copy-asset-known", CreatedAt: now, UpdatedAt: now, Config: `{"source":"canonical-a"}`}, {Name: "copy-asset-unknown", CreatedAt: now, UpdatedAt: now, Config: `{"source":"canonical-b"}`}}
			for i := range nodes {
				if err := f.source.Create(&nodes[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "current_including_unknown" {
				if err := f.source.Table("nodes").Where("id = ?", nodes[1].ID).Update("asset_tag", "").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if strings.HasPrefix(mode, "legacy_") {
					if err := f.source.Exec("DELETE FROM schema_migrations WHERE version = ?", "0059_node_asset_identity.up.sql").Error; err != nil {
						t.Fatal(err)
					}
				}
				if err := f.source.Migrator().DropColumn("nodes", "asset_tag"); err != nil {
					t.Fatal(err)
				}
			}
			before := nodeAssetCopyRawState(t, f.source)
			canonical := nodeAssetCopyCanonical(t, f.source)
			sentinel := errors.New("test asset-copy owned completion failed")
			copier := f.copier(false)
			if mode == "legacy_completion_rollback" {
				copier = MigrationCopier{DB: f.source, PrepareSchema: func(db *gorm.DB) error {
					if err := prepareMigrationCopyTestSchema(db); err != nil {
						return err
					}
					return db.Callback().Update().Before("gorm:update").Register("test:asset_copy_completion", func(tx *gorm.DB) {
						values, ok := tx.Statement.Dest.(map[string]any)
						if tx.Statement.Table != migrationPreparationTable || !ok || values["state"] != "completed" {
							return
						}
						var coverage struct{ Total, Known int64 }
						if err := tx.Session(&gorm.Session{NewDB: true}).Table("nodes").Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN asset_tag <> '' THEN 1 ELSE 0 END),0) AS known").Scan(&coverage).Error; err != nil {
							tx.AddError(err)
							return
						}
						if coverage.Total != 2 || coverage.Known != 2 {
							tx.AddError(errors.New("copy completion lacks two visible future labels inside owned transaction"))
							return
						}
						tx.AddError(sentinel)
					})
				}}
			}
			err := copier.Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID)
			if !reflect.DeepEqual(before, nodeAssetCopyRawState(t, f.source)) {
				t.Fatal("copy modified source node data/columns")
			}
			target := f.destination(t)
			if mode == "recorded_column_loss" || mode == "legacy_completion_rollback" {
				if mode == "recorded_column_loss" {
					if err == nil || !strings.Contains(err.Error(), "recorded node asset identity is missing") {
						t.Fatal("lost identity treated as legacy", err)
					}
				} else {
					if !errors.Is(err, sentinel) {
						t.Fatal("completion failed before expected target label proof", err)
					}
				}
				var count int64
				if err := target.Model(&model.Node{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("failed target transaction committed labels", err)
				}
				receipt, err := loadMigrationPreparation(target, false)
				if err != nil {
					t.Fatal(err)
				}
				if err := verifyPreparationManifest(target, receipt, false); err != nil {
					t.Fatal("failed copy lost original prepared ownership", err)
				}
				if mode == "recorded_column_loss" {
					return
				}
				if err := f.copier(false).Copy(context.Background(), f.target, f.task.ID, f.claim.Run.ID); err != nil {
					t.Fatal("legacy copy retry", err)
				}
			} else if err != nil {
				t.Fatal("owned asset copy", err)
			}
			var copied []model.Node
			if err := target.Order("id").Find(&copied).Error; err != nil || len(copied) != 2 {
				t.Fatal("copied assets missing", err)
			}
			for i, row := range copied {
				if row.ID != nodes[i].ID || row.Name != nodes[i].Name || nodeAssetCopyJSON(t, row.Config) != canonical[i].Config || row.NodeCredential != nodes[i].NodeCredential {
					t.Fatal("asset labeling changed canonical source fields")
				}
				if mode == "current_including_unknown" {
					want := nodes[i].AssetTag
					if i == 1 {
						want = ""
					}
					if row.AssetTag != want {
						t.Fatal("copy rekeyed current or guessed unknown label")
					}
				} else if len(row.AssetTag) != 64 || i > 0 && row.AssetTag == copied[0].AssetTag {
					t.Fatal("legacy target did not establish distinct future labels")
				}
			}
			if !reflect.DeepEqual(canonical, nodeAssetCopyCanonical(t, target)) {
				t.Fatal("asset copy changed canonical current Node model")
			}
			var done migrationPreparation
			if err := target.First(&done, 1).Error; err != nil || done.State != "completed" {
				t.Fatal("asset copy did not finish owned receipt", err)
			}
			if !reflect.DeepEqual(before, nodeAssetCopyRawState(t, f.source)) {
				t.Fatal("target completion changed source assets")
			}
			assertMigrationCopySourcePreserved(t, f)
		})
	}
}
func nodeAssetCopyRawState(t *testing.T, db *gorm.DB) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := db.Table("nodes").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func nodeAssetCopyCanonical(t *testing.T, db *gorm.DB) []model.Node {
	t.Helper()
	var nodes []model.Node
	if err := db.Order("id").Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	for i := range nodes {
		nodes[i].AssetTag = ""
		nodes[i].Config = nodeAssetCopyJSON(t, nodes[i].Config)
		nodes[i].CreatedAt = nodes[i].CreatedAt.UTC()
		nodes[i].UpdatedAt = nodes[i].UpdatedAt.UTC()
	}
	return nodes
}

// Compare JSON values across storage engines without losing integer precision.
// Raw source rows are also compared unchanged before/after the whole copy.
func nodeAssetCopyJSON(t *testing.T, value string) string {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(canonical)
}
