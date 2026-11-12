package datastore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const mailboxVersion = "0085_observation_mailboxes.up.sql"

func TestSQLiteObservationMailboxUpgradeCopyAndCascade(t *testing.T) { assertMailboxSchema(t, false) }
func TestMySQLObservationMailboxUpgradeCopyAndCascade(t *testing.T)  { assertMailboxSchema(t, true) }
func assertMailboxSchema(t *testing.T, mysql bool) {
	db := diagnosticSchemaDB(t, mysql)
	// An interrupted upgrade has already added inline columns but has no receipt,
	// tables or index. Re-running migration must finish without clearing state.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", mailboxVersion).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_observation_mailboxes", "node_observation_gc_states"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
	}
	node := model.Node{Name: "mailbox-copy", Config: "{}", IsEnabled: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	p := network.EventPosition{AssetTag: node.AssetTag, CoreInstanceID: strings.Repeat("long-runtime", 40), ID: "position", Sequence: 9, OccurredAt: at, ReceivedAt: at}
	source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p, StatsEvent: &p, Stats: network.NodeStats{ActiveSessions: 7, BytesUp: 110, BytesDown: 220}}}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return observationstore.EnqueueLatest(tx, []network.ObservationSource{source})
	}); err != nil {
		t.Fatal(err)
	}
	target := diagnosticSchemaDB(t, mysql)
	principalRuntimeCopy(t, db, target)
	if n, err := (observationstore.Worker{DB: target}).Drain(context.Background(), 8, observationstore.MaxDrainBytes); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var got model.Node
	if err := target.First(&got, node.ID).Error; err != nil || got.BytesUp != 110 || got.ActiveFlows != 7 {
		t.Fatal(got, err)
	}
	// A copied pending projection is acknowledged independently of the source.
	var pending int64
	if err := db.Model(&model.NodeObservationMailbox{}).Where("pending = ?", true).Count(&pending).Error; err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
	if err := target.Delete(&model.Node{}, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := target.Model(&model.NodeObservationMailbox{}).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatal("mailbox outlived its node", pending, err)
	}
	// Legacy auxiliary source retirement and terminal header GC also run on
	// actual MySQL. They cannot affect the copied mailbox or create fresh history.
	source.Observation.Latest.Sequence++
	source.Observation.StatsEvent = &source.Observation.Latest
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := observationstore.Enqueue(tx, source); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := (observationstore.Worker{DB: db}).Drain(t.Context(), 8, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	retention := observationstore.MaterialRetention{DB: db, Now: func() time.Time { return at.Add(8 * 24 * time.Hour) }}
	if _, err := retention.Retire(t.Context(), 8, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	retention.Now = func() time.Time { return at.Add(8*24*time.Hour + 2*time.Hour) }
	if _, err := retention.Retire(t.Context(), 8, observationstore.MaxDrainBytes); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_observation_intents", "node_observation_parts", "node_observation_retirements"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("legacy auxiliary source did not converge", table, count, err)
		}
	}
	// A recorded migration is not a license to silently repair lost constraints.
	if err := db.Migrator().DropIndex("node_observation_mailboxes", "idx_observation_mailbox_pending"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err == nil {
		t.Fatal("recorded malformed mailbox accepted")
	}
}
