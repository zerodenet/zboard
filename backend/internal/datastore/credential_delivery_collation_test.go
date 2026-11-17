package datastore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMySQLCredentialDeliveryMixedCollationsPreserveExactEvidence(t *testing.T) {
	db := diagnosticSchemaDB(t, true)
	// Baseline columns use utf8mb4_0900_ai_ci; an existing database's default
	// can supply a different collation to newly materialized receipt tables.
	if err := db.Exec("ALTER TABLE node_credential_delivery_states MODIFY config_sha256 VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL").Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "collation-fixture", Address: "127.0.0.1", Config: "{}"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	manifest, err := network.EncodeCredentialDeliveryManifest(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	digest := strings.Repeat("ab", 32)
	deployment := model.ProtocolDeployment{NodeID: node.ID, DesiredConfigSHA256: digest, AppliedConfigSHA256: digest, Status: "succeeded", CredentialTargets: manifest, FinishedAt: &at}
	if err := db.Create(&deployment).Error; err != nil {
		t.Fatal(err)
	}
	receipt := model.NodeCredentialDeliveryState{NodeID: node.ID, DeploymentID: deployment.ID, ConfigSHA256: digest, ConfirmedAt: at}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal("valid mixed-collation evidence rejected", err)
		}
	}
	// Case-insensitive collations must not accept a different byte string as an
	// exact configuration confirmation, or rewrite it to manufacture a match.
	conflict := strings.ToUpper(digest)
	if err := db.Model(&receipt).UpdateColumn("config_sha256", conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "missing or mismatched") {
		t.Fatal("different configuration bytes accepted", err)
	}
	var retained model.NodeCredentialDeliveryState
	if err := db.First(&retained, "node_id = ?", node.ID).Error; err != nil || retained.ConfigSHA256 != conflict {
		t.Fatal("failed confirmation check changed original evidence", err)
	}
}
