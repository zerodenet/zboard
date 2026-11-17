package networkstore

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestNodeAssetIdentityPersistenceAndCapturedCredential(t *testing.T) {
	a, b := administrationFixture(t)
	node := model.Node{Name: "asset-node", Config: "{}", NodeCredential: "original-ciphertext"}
	if err := a.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	original := node.AssetTag
	captured, err := (EventCredentials{DB: b}).EventNode(context.Background(), node.ID)
	if err != nil || captured.AssetTag != original || len(original) != 64 {
		t.Fatal("authentication failed to capture stored label", err)
	}
	// Create-only ORM metadata also protects updates that explicitly name it.
	if err := a.Model(&node).Updates(map[string]any{"asset_tag": "forged", "name": "asset-node-updated", "node_credential": "rotated-ciphertext"}).Error; err != nil {
		t.Fatal(err)
	}
	var after model.Node
	if err := b.First(&after, node.ID).Error; err != nil || after.AssetTag != original || after.NodeCredential != "rotated-ciphertext" {
		t.Fatal("ordinary update/credential rotation changed asset identity", err)
	}
	if captured.AssetTag != original || captured.Credential != "original-ciphertext" {
		t.Fatal("captured admission material changed retroactively")
	}
	if err := a.Delete(&model.Node{}, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	replacement := model.Node{ID: node.ID, Name: "replacement", Config: "{}"}
	if err := a.Create(&replacement).Error; err != nil || replacement.AssetTag == original {
		t.Fatal("numeric ID reuse reused the prior asset", err)
	}
	// This is not a deletion fence: the old captured label remains old.
	if captured.AssetTag != original {
		t.Fatal("old admission was relabeled")
	}
}

func TestNodeAssetInitializationResumesWithoutRekeyingOrPrematureReceipt(t *testing.T) {
	a, b := administrationFixture(t)
	const receipt = "0059_node_asset_identity.up.sql"
	rows := []model.Node{{Name: "already-generated", Config: "{}"}, {Name: "pending-one", Config: "{}"}, {Name: "pending-two", Config: "{}"}}
	for i := range rows {
		if err := a.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	known := rows[0].AssetTag
	if err := a.Exec("DELETE FROM schema_migrations WHERE version = ?", receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.Table("nodes").Where("id IN ?", []uint{rows[1].ID, rows[2].ID}).Update("asset_tag", "").Error; err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("test node asset initialization interrupted")
	writes := 0
	const hook = "test:node_asset_initialization"
	if err := a.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "nodes" {
			if values, ok := tx.Statement.Dest.(map[string]any); ok {
				if _, ok = values["asset_tag"]; ok {
					writes++
					if writes == 2 {
						tx.AddError(sentinel)
					}
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := datastore.RunMigrations(a)
	if removeErr := a.Callback().Update().Remove(hook); removeErr != nil {
		t.Fatal(removeErr)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("initialization did not stop at persisted partial boundary", err)
	}
	var count int64
	if err := b.Table("schema_migrations").Where("version = ?", receipt).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed initialization recorded receipt", err)
	}
	var first, pending model.Node
	if err := b.First(&first, rows[1].ID).Error; err != nil || first.AssetTag == "" {
		t.Fatal("first persisted label not visible to second pool", err)
	}
	if err := b.First(&pending, rows[2].ID).Error; err != nil || pending.AssetTag != "" {
		t.Fatal("failed write unexpectedly persisted", err)
	}
	if err := datastore.RunMigrations(b); err != nil {
		t.Fatal(err)
	}
	for _, saved := range []struct {
		id  uint
		tag string
	}{{rows[0].ID, known}, {rows[1].ID, first.AssetTag}} {
		var row model.Node
		if err := a.First(&row, saved.id).Error; err != nil || row.AssetTag != saved.tag {
			t.Fatal("re-entry replaced an existing label", err)
		}
	}
	// After receipt, an individual unknown row must remain unknown on repeat.
	if err := a.Table("nodes").Where("id = ?", rows[2].ID).Update("asset_tag", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(b); err != nil {
		t.Fatal(err)
	}
	if err := a.First(&pending, rows[2].ID).Error; err != nil || pending.AssetTag != "" {
		t.Fatal("repeat manufactured an identity for unknown row", err)
	}
	// Lost structure must be refused before SQLite AutoMigrate can repair it.
	if err := a.Migrator().DropColumn("nodes", "asset_tag"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(b); err == nil || !strings.Contains(err.Error(), "recorded node asset identity is missing") {
		t.Fatal("recorded identity loss was repaired instead of refused", err)
	}
	if b.Migrator().HasColumn("nodes", "asset_tag") {
		t.Fatal("preflight allowed column recreation")
	}
}

func TestNodeAssetSurvivesAdministrationAndCreationRollback(t *testing.T) {
	db, other := administrationFixture(t)
	seedNodeAdministrationAdmin(t, db)
	service := nodeAdministrationService(db)
	created, err := service.Create(context.Background(), 1, network.NodeCreateChange{Name: "owned-asset", Config: "{}", IsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var initial model.Node
	if err := other.First(&initial, created.ID).Error; err != nil || initial.AssetTag == "" {
		t.Fatal("administrative create lacked label", err)
	}
	name := "renamed-owned-asset"
	if _, err := service.Update(context.Background(), 1, network.NodeUpdateRequest{ID: created.ID, Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveSSH(context.Background(), 1, created.ID, network.NodeSSHConfigurationChange{SSHHost: "host", SSHPort: 22, SSHUser: "root", SSHAuthMethod: "password", SSHPwdCiphertext: "enc:test", SSHPrivilegeMode: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RotateCredential(context.Background(), 1, created.ID, network.NodeCredentialConnector, network.NodeCredentialChange{Ciphertext: "enc:rotated", Prefix: "new-connecto"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCredential(context.Background(), 1, created.ID, network.NodeCredentialConnector); err != nil {
		t.Fatal(err)
	}
	var after model.Node
	if err := other.First(&after, created.ID).Error; err != nil || after.AssetTag != initial.AssetTag {
		t.Fatal("business mutation rekeyed asset", err)
	}
	sentinel := errors.New("test asset-create audit failed")
	const hook = "test:asset_create_rollback"
	if err := db.Callback().Create().Before("gorm:create").Register(hook, func(tx *gorm.DB) {
		if audit, ok := tx.Statement.Dest.(*model.AuditLog); ok && audit.Action == "node.create" {
			tx.AddError(sentinel)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), 1, network.NodeCreateChange{Name: "rolled-back-asset", Config: "{}", IsEnabled: true})
	if removeErr := db.Callback().Create().Remove(hook); removeErr != nil {
		t.Fatal(removeErr)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("create failure not preserved", err)
	}
	var count int64
	if err := other.Model(&model.Node{}).Where("name = ?", "rolled-back-asset").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed create persisted asset label", err)
	}
	var kernels int64
	if err := other.Model(&model.NodeKernelState{}).Count(&kernels).Error; err != nil || kernels != 1 {
		t.Fatal("asset create rollback leaked kernel state", err)
	}
}

// The CI MySQL selection uses -run MySQL; reuse the same real behavior bodies
// rather than depending on generic tests to inherit an opt-in environment.
func TestMySQLNodeAssetFoundationPersistence(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	t.Run("identity_and_captured_credential", TestNodeAssetIdentityPersistenceAndCapturedCredential)
	t.Run("initialization_resume_and_receipt", TestNodeAssetInitializationResumesWithoutRekeyingOrPrematureReceipt)
	t.Run("administration_and_creation_rollback", TestNodeAssetSurvivesAdministrationAndCreationRollback)
}
