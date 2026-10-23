package networkstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/migrations"
	"gorm.io/gorm"
)

func seedDeliveryCredential(t *testing.T, db *gorm.DB, id uint, now time.Time) model.ProtocolCredential {
	t.Helper()
	sub := model.Subscription{ID: id, UserID: 1, PlanID: 1, PlanSKUID: 1, NodeGroupID: 1, Status: "active", FlowTotal: 100, Config: "{}", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour)}
	row := model.ProtocolCredential{ID: id, SubscriptionID: id, UserID: 1, NodeID: 1, ProtocolEndpointID: 1, CredentialID: strings.Repeat("x", int(id)), PrincipalKey: strings.Repeat("p", int(id)), Secret: "encrypted-test-material", DeliveryRequired: true, Status: "active", ExpiresAt: sub.EndAt}
	for _, value := range []any{&sub, &row} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	return row
}
func deliveryTarget(t *testing.T, db *gorm.DB, row model.ProtocolCredential) network.CredentialDeliveryTarget {
	var endpoint model.ProtocolEndpoint
	if err := db.First(&endpoint, row.ProtocolEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	return network.CredentialDeliveryTarget{EndpointSHA256: network.CredentialEndpointFingerprint(network.RuntimeConfigurationEndpoint{ID: endpoint.ID, NodeID: endpoint.NodeID, Protocol: endpoint.Protocol, Address: endpoint.Address, ListenAddress: endpoint.ListenAddress, Port: endpoint.Port, PublicPort: endpoint.PublicPort, ServerConfig: endpoint.ServerConfig, EgressConfig: endpoint.EgressConfig, ClientConfig: endpoint.ClientConfig}), CredentialID: row.ID, ProtocolEndpointID: row.ProtocolEndpointID, MaterialSHA256: network.CredentialMaterialFingerprint(row.NodeID, row.ProtocolEndpointID, row.ID, row.PrincipalKey, row.Secret, row.ListenPort, row.PublicPort), PolicyRevision: 1}
}
func deliveryStatuses(t *testing.T, db *gorm.DB, expected map[uint]string) {
	t.Helper()
	var rows []model.ProtocolCredential
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	statuses, err := credentialstore.DeliveryStatuses(db, rows)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range expected {
		if statuses[id] != want {
			t.Fatalf("credential%d delivery=%s want=%s", id, statuses[id], want)
		}
	}
}
func TestCredentialDeliveryMySQLAndSQLiteFreezesBindingsAndRecoversProjection(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	ctx := context.Background()
	first := seedDeliveryCredential(t, db, 1, now)
	state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: db}, Now: func() time.Time { return now }}
	started, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1, TriggerEndpointID: 1})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	frozen, err := state.SetDesired(ctx, started.Deployment, sha, []network.CredentialDeliveryTarget{deliveryTarget(t, db, first)})
	if err != nil {
		t.Fatal(err)
	}
	second := seedDeliveryCredential(t, db, 2, now)
	if _, err := state.SetDesired(ctx, frozen, sha, []network.CredentialDeliveryTarget{deliveryTarget(t, db, first), deliveryTarget(t, db, second)}); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatal("old identity accepted new credential set", err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "pending", 2: "pending"})
	completion := network.ConfigurationPublicationCompletion{ConfigSHA: strings.Repeat("b", 64), LastHealthyAt: now}
	if _, err := state.Complete(ctx, frozen, completion); !errors.Is(err, network.ErrConfigurationDeploymentLost) {
		t.Fatal("health confirmed wrong config", err)
	}
	completion.ConfigSHA = sha
	if _, err := state.Complete(ctx, frozen, completion); err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "confirmed", 2: "pending"})
	// Rebuild uses original immutable evidence and its original timestamp,
	// including an interruption leaving both auxiliary tables present but empty.
	for _, table := range []string{"credential_delivery_confirmations", "node_credential_delivery_states"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
		deliveryStatuses(t, db, map[uint]string{1: "confirmed", 2: "pending"})
	}
	if err := db.Where("1 = 1").Delete(&model.CredentialDeliveryConfirmation{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1 = 1").Delete(&model.NodeCredentialDeliveryState{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "confirmed", 2: "pending"})
	// Observations do not own confirmed delivery.
	if err := db.Model(&model.Node{}).Where("id = ?", 1).Updates(map[string]any{"last_seen_at": nil, "is_online": false}).Error; err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "confirmed", 2: "pending"})
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", 1).Update("port", 8443).Error; err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "pending", 2: "pending"})
	if err := db.Model(&model.ProtocolEndpoint{}).Where("id = ?", 1).Update("port", 443).Error; err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "confirmed", 2: "pending"})
	// Replacing material does not inherit the old binding's receipt.
	if err := db.Model(&model.ProtocolCredential{}).Where("id = ?", 1).Update("secret", "different-encrypted-material").Error; err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "pending", 2: "pending"})
	// A conclusive newer config excluding a credential invalidates its previous
	// applied projection; a timeout alone never does this.
	next, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1, TriggerEndpointID: 1})
	if err != nil {
		t.Fatal(err)
	}
	next.Deployment, err = state.SetDesired(ctx, next.Deployment, strings.Repeat("c", 64), []network.CredentialDeliveryTarget{deliveryTarget(t, db, second)})
	if err != nil {
		t.Fatal(err)
	}
	pending, uncertainErr := state.Uncertain(ctx, next.Deployment, network.ConfigurationOutcomeUnknown(errors.New("lost native ack")), "")
	if !errors.Is(uncertainErr, network.ErrConfigurationOutcomeUnknown) {
		t.Fatal(uncertainErr)
	}
	next.Deployment = pending
	third := seedDeliveryCredential(t, db, 3, now)
	if err := state.Resolve(ctx, next.Deployment, true, "exact frozen SHA verified"); err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "pending", 2: "confirmed", third.ID: "pending"})
	if err := db.Migrator().DropColumn(&model.ProtocolDeployment{}, "credential_targets"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "source evidence") {
		t.Fatal("lost source was fabricated", err)
	}
}

func TestCredentialDeliveryMySQLAndSQLiteConfirmationFailureIsAtomic(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	row := seedDeliveryCredential(t, db, 1, now)
	ctx := context.Background()
	state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: db}}
	started, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1, TriggerEndpointID: 1})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesired(ctx, started.Deployment, strings.Repeat("d", 64), []network.CredentialDeliveryTarget{deliveryTarget(t, db, row)})
	if err != nil {
		t.Fatal(err)
	}
	trigger := "CREATE TRIGGER reject_delivery BEFORE INSERT ON credential_delivery_confirmations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'test delivery failure'"
	if db.Dialector.Name() == "sqlite" {
		trigger = "CREATE TRIGGER reject_delivery BEFORE INSERT ON credential_delivery_confirmations BEGIN SELECT RAISE(ABORT,'test delivery failure'); END"
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	completion := network.ConfigurationPublicationCompletion{ConfigSHA: frozen.DesiredConfigSHA256, LastHealthyAt: now, ManagedPrincipalAccess: true, MieruAccess: true}
	if _, err := state.Complete(ctx, frozen, completion); err == nil {
		t.Fatal("failed confirmation was committed")
	}
	var deployment model.ProtocolDeployment
	if err := db.First(&deployment, frozen.ID).Error; err != nil || deployment.Status != "running" || deployment.AppliedConfigSHA256 != "" {
		t.Fatal("partial applied state", deployment.Status, err)
	}
	var node model.Node
	if err := db.First(&node, 1).Error; err != nil || node.LastSyncAt != nil {
		t.Fatal("node falsely applied", err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "pending"})
	if err := db.Exec("DROP TRIGGER reject_delivery").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, completion); err != nil {
		t.Fatal(err)
	}
	deliveryStatuses(t, db, map[uint]string{1: "confirmed"})
}

func TestNetworkEntryDeliveryMySQLAndSQLiteConfirmationIsAtomicAndFrozen(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	ctx := context.Background()
	front := model.Node{ID: 2, Name: "delivery-front", Config: "{}"}
	if err := db.Create(&front).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{ID: 1, NodeID: front.ID, EndpointID: 1, Name: "managed", Network: "tcp", Address: "front.example.test", Port: 8443, PublicPort: 8443, Enabled: true, DeliveryRequired: true}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	target := network.NetworkEntryDeliveryTarget{NetworkEntryID: 1, MaterialSHA256: network.NetworkEntryDeliveryFingerprint(network.RuntimeConfigurationNetworkEntry{ID: entry.ID, NodeID: front.ID, EndpointID: 1, Network: entry.Network, Address: entry.Address, ListenAddress: entry.ListenAddress, Port: entry.Port, PublicPort: entry.PublicPort, Landing: network.RuntimeConfigurationLanding{Protocol: "shadowsocks", Address: "test", Port: 443, PublicPort: 443}})}
	state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: db}}
	started, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: front.ID})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesiredWithTargets(ctx, started.Deployment, strings.Repeat("e", 64), network.ConfigurationDeliveryTargets{Credentials: []network.CredentialDeliveryTarget{}, NetworkEntries: []network.NetworkEntryDeliveryTarget{target}})
	if err != nil {
		t.Fatal(err)
	}
	trigger := "CREATE TRIGGER reject_entry_delivery BEFORE INSERT ON network_entry_delivery_confirmations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'test entry delivery failure'"
	if db.Dialector.Name() == "sqlite" {
		trigger = "CREATE TRIGGER reject_entry_delivery BEFORE INSERT ON network_entry_delivery_confirmations BEGIN SELECT RAISE(ABORT,'test entry delivery failure'); END"
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	completion := network.ConfigurationPublicationCompletion{ConfigSHA: frozen.DesiredConfigSHA256, LastHealthyAt: now}
	if _, err := state.Complete(ctx, frozen, completion); err == nil {
		t.Fatal("entry projection failure committed application")
	}
	var pending model.ProtocolDeployment
	if err := db.First(&pending, frozen.ID).Error; err != nil || pending.Status != "running" || pending.AppliedConfigSHA256 != "" {
		t.Fatal("partial deployment escaped", pending.Status, err)
	}
	if err := db.Exec("DROP TRIGGER reject_entry_delivery").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, completion); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.NetworkEntryDeliveryConfirmation{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var proof model.NetworkEntryDeliveryConfirmation
	if err := db.First(&proof, entry.ID).Error; err != nil || proof.MaterialSHA256 != target.MaterialSHA256 || proof.ConfigSHA256 != frozen.DesiredConfigSHA256 {
		t.Fatal("entry proof was not reconstructed from its source", err)
	}
}

func TestCredentialDeliveryDDLMySQLAndSQLiteMatchesRecovery(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	legacy := seedDeliveryCredential(t, db, 1, now)
	// Disposable, empty-of-delivery-evidence fixture representing schema0046.
	// This does not remove a deployed migration marker or real source evidence.
	if err := db.Model(&model.ProtocolCredential{}).Where("id = ?", legacy.ID).Update("delivery_required", false).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"credential_delivery_confirmations", "node_credential_delivery_states", "network_entry_delivery_confirmations"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []struct{ table, column string }{{"protocol_deployments", "credential_targets"}, {"protocol_credentials", "delivery_required"}, {"network_entries", "delivery_required"}} {
		if err := db.Migrator().DropColumn(source.table, source.column); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropIndex("protocol_deployments", "idx_deployment_node_status_id"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "0047_credential_delivery.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	file := "0047_credential_delivery.up.sql"
	if db.Dialector.Name() == "sqlite" {
		file = "sqlite/0047_credential_delivery.sql"
	}
	sql, err := migrations.Files.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(sql), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var current model.ProtocolCredential
	if err := db.First(&current, legacy.ID).Error; err != nil || current.DeliveryRequired || current.Secret != legacy.Secret || current.PrincipalKey != legacy.PrincipalKey || current.Status != "active" {
		t.Fatal("DDL fabricated delivery or changed canonical facts", err)
	}
	deliveryStatuses(t, db, map[uint]string{legacy.ID: "legacy_unknown"})
}

func TestCredentialDeliveryRecoveryMySQLAndSQLiteCannotReplaceNewerConfirmation(t *testing.T) {
	for _, mode := range []string{"newer_commits_after_source_scan", "publisher_waits_for_recovery"} {
		t.Run(mode, func(t *testing.T) {
			db, other := administrationFixture(t)
			now := seedPrincipalIndexOwners(t, db)
			row := seedDeliveryCredential(t, db, 1, now)
			ctx := context.Background()
			state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: other}}
			first, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := state.SetDesired(ctx, first.Deployment, strings.Repeat("a", 64), []network.CredentialDeliveryTarget{deliveryTarget(t, db, row)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := state.Complete(ctx, frozen, network.ConfigurationPublicationCompletion{ConfigSHA: frozen.DesiredConfigSHA256, LastHealthyAt: now}); err != nil {
				t.Fatal(err)
			}
			next, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1})
			if err != nil {
				t.Fatal(err)
			}
			next.Deployment, err = state.SetDesired(ctx, next.Deployment, strings.Repeat("f", 64), []network.CredentialDeliveryTarget{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Where("node_id = ?", 1).Delete(&model.NodeCredentialDeliveryState{}).Error; err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once, unblockOnce sync.Once
			unblock := func() { unblockOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			if err := db.Callback().Query().After("gorm:query").Register("test_delivery_recovery_fence", func(query *gorm.DB) {
				match := strings.Contains(query.Statement.SQL.String(), "MAX(id)")
				if mode == "publisher_waits_for_recovery" {
					_, transactional := query.Statement.ConnPool.(gorm.TxCommitter)
					match = query.Statement.Table == "nodes" && transactional
				}
				if match {
					once.Do(func() { close(entered); <-release })
				}
			}); err != nil {
				t.Fatal(err)
			}
			recovered := make(chan error, 1)
			go func() { recovered <- datastore.RunMigrations(db) }()
			select {
			case <-entered:
			case <-time.After(15 * time.Second):
				t.Fatal("recovery did not reach intended fence")
			}
			published := make(chan error, 1)
			go func() {
				_, err := state.Complete(ctx, next.Deployment, network.ConfigurationPublicationCompletion{ConfigSHA: next.Deployment.DesiredConfigSHA256, LastHealthyAt: now})
				published <- err
			}()
			if mode == "publisher_waits_for_recovery" {
				select {
				case err := <-published:
					t.Fatal("publisher bypassed recovery node lock", err)
				case <-time.After(100 * time.Millisecond):
				}
				unblock()
				if err := <-published; err != nil {
					t.Fatal(err)
				}
			} else {
				select {
				case err := <-published:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("source scan held an unexpected writer lock")
				}
				unblock()
			}
			if err := <-recovered; err != nil {
				t.Fatal(err)
			}
			var current model.NodeCredentialDeliveryState
			if err := db.First(&current, 1).Error; err != nil || current.ConfigSHA256 != next.Deployment.DesiredConfigSHA256 || current.DeploymentID != next.Deployment.ID {
				t.Fatal("older recovery replaced latest confirmation", current.DeploymentID, err)
			}
			deliveryStatuses(t, db, map[uint]string{row.ID: "pending"})
		})
	}
}

func TestCredentialDeliveryMySQLAndSQLiteRejectsMissingConfirmedSource(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	row := seedDeliveryCredential(t, db, 1, now)
	ctx := context.Background()
	state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: db}}
	start, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesired(ctx, start.Deployment, strings.Repeat("a", 64), []network.CredentialDeliveryTarget{deliveryTarget(t, db, row)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, network.ConfigurationPublicationCompletion{ConfigSHA: frozen.DesiredConfigSHA256, LastHealthyAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.ProtocolDeployment{}, frozen.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil || !strings.Contains(err.Error(), "source evidence") {
		t.Fatal("lost immutable source was silently trusted", err)
	}
	var current model.ProtocolCredential
	if err := db.First(&current, row.ID).Error; err != nil || current.Secret != row.Secret || current.Status != "active" {
		t.Fatal("failed preflight modified authorization", err)
	}
}

func TestCredentialDeliveryMySQLAndSQLiteEndpointRemovalPreservesNodeEvidence(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	row := seedDeliveryCredential(t, db, 1, now)
	ctx := context.Background()
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	state := network.ConfigurationPublicationState{Repository: ConfigurationPublicationState{DB: db}}
	start, err := state.Begin(ctx, network.ConfigurationPublicationRequest{NodeID: 1, TriggerEndpointID: 1})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := state.SetDesired(ctx, start.Deployment, strings.Repeat("a", 64), []network.CredentialDeliveryTarget{deliveryTarget(t, db, row)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Complete(ctx, frozen, network.ConfigurationPublicationCompletion{ConfigSHA: frozen.DesiredConfigSHA256, LastHealthyAt: now}); err != nil {
		t.Fatal(err)
	}
	var before model.ProtocolDeployment
	if err := db.First(&before, frozen.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (ProtocolEndpointRemoval{DB: db}).RemoveProtocolEndpoint(ctx, 1, 1, now); err != nil {
		t.Fatal(err)
	}
	var after model.ProtocolDeployment
	if err := db.First(&after, frozen.ID).Error; err != nil || after.ProtocolEndpointID != 0 || after.CredentialTargets != before.CredentialTargets || after.Status != "succeeded" || after.AppliedConfigSHA256 != before.AppliedConfigSHA256 {
		t.Fatal("endpoint deletion erased node confirmation evidence", after.Status, err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal("legitimate resource deletion blocked recovery", err)
	}
	var queued model.NodeConfigPublish
	if err := db.First(&queued, 1).Error; err != nil {
		t.Fatal("endpoint removal lost replacement intent", err)
	}
}

func TestCredentialDeliveryMySQLAndSQLiteRepairsMissingGateWithoutLosingLease(t *testing.T) {
	db, _ := administrationFixture(t)
	now := seedPrincipalIndexOwners(t, db)
	credential := seedDeliveryCredential(t, db, 1, now)
	front := model.Node{ID: 2, Name: "repair-front", Config: "{}"}
	if err := db.Create(&front).Error; err != nil {
		t.Fatal(err)
	}
	entry := model.NetworkEntry{ID: 1, NodeID: 2, EndpointID: 1, Name: "repair-entry", Network: "tcp", Address: "front.example.test", Port: 8443, PublicPort: 8443, Enabled: true, DeliveryRequired: true}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	lease := now.Add(time.Hour)
	queued := model.NodeConfigPublish{NodeID: 1, Generation: 9, Phase: "connections", AppliedConfigSHA256: strings.Repeat("a", 64), Reconciling: true, NextAttemptAt: now, LeaseUntil: lease, LeaseToken: "held-repair-test"}
	if err := db.Create(&queued).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"protocol_credentials", "network_entries"} {
		if err := db.Migrator().DropColumn(table, "delivery_required"); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	var repaired model.ProtocolCredential
	if err := db.First(&repaired, credential.ID).Error; err != nil || !repaired.DeliveryRequired || repaired.Secret != credential.Secret {
		t.Fatal("repair weakened gate or changed material", err)
	}
	deliveryStatuses(t, db, map[uint]string{credential.ID: "pending"})
	var held model.NodeConfigPublish
	if err := db.First(&held, 1).Error; err != nil || held.Generation != 10 || held.Phase != "" || held.AppliedConfigSHA256 != "" || held.LeaseToken != queued.LeaseToken || !held.LeaseUntil.Equal(lease) || !held.Reconciling {
		t.Fatal("repair lost lease/unknown intent or repeated enqueue", held.Generation, err)
	}
	var pending model.NodeConfigPublish
	if err := db.First(&pending, 2).Error; err != nil || pending.Generation != 1 || pending.Phase != "" {
		t.Fatal("managed listener verification intent missing", err)
	}
	var repairedEntry model.NetworkEntry
	if err := db.First(&repairedEntry, entry.ID).Error; err != nil || !repairedEntry.DeliveryRequired {
		t.Fatal("listener repair defaulted to legacy availability", err)
	}
}
