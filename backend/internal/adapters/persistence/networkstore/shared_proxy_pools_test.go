package networkstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"os"
	"testing"
	"time"
)

func TestSharedProxyPoolIndependentReuseFanoutAndDetach(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if e := db.Model(&model.User{}).Where("id=1").Update("is_admin", true).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&model.Node{ID: 2, Name: "second", Config: "{}"}).Error; e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	service := proxyPoolMutationService(db)
	config := "udp-capable"
	pool, e := service.Save(ctx, 1, network.ProxyPoolMutationRequest{Name: "independent", Config: &config})
	if e != nil {
		t.Fatal(e)
	}
	var owner *uint
	if e := db.Raw("SELECT node_id FROM node_proxy_pools WHERE id=?", pool.ID).Scan(&owner).Error; e != nil || owner != nil {
		t.Fatal("pool still owned", owner, e)
	}
	for _, node := range []uint{1, 2, 2} {
		if e := service.SetNodeReference(ctx, 1, pool.ID, node, true); e != nil {
			t.Fatal(e)
		}
	}
	q := ProxyPoolQueries{DB: db}
	for _, node := range []uint{0, 1, 2} {
		rows, e := q.ListProxyPools(ctx, 1, node)
		if e != nil || len(rows) != 1 || len(rows[0].Nodes) != 2 {
			t.Fatalf("node %d rows=%+v err=%v", node, rows, e)
		}
	}
	if _, e := service.Save(ctx, 1, network.ProxyPoolMutationRequest{ID: pool.ID, Name: "shared updated", ExpectedRevision: pool.Revision}); e != nil {
		t.Fatal(e)
	}
	var publications []model.NodeConfigPublish
	if e := db.Order("node_id").Find(&publications).Error; e != nil || len(publications) != 2 {
		t.Fatal(publications, e)
	}
	for _, p := range publications {
		if p.Generation != 1 {
			t.Fatal("duplicate publication", p)
		}
	}
	// Unchanged sync advances liveness without allocating another revision,
	// publication or audit row; encryption stays stable despite randomized ciphers.
	store := ProxyPoolSubscriptions{DB: db}
	actor := network.ProxyPoolSubscriptionActor{System: true}
	if e := db.Model(&model.NodeProxyPool{}).Where("id=?", pool.ID).Updates(map[string]any{"auto_sync": true, "subscription_url": "enc:https://example.test/sub", "sync_interval_seconds": 300}).Error; e != nil {
		t.Fatal(e)
	}
	snapshot, e := store.LoadProxyPoolSubscription(ctx, actor, pool.ID)
	if e != nil {
		t.Fatal(e)
	}
	var audits int64
	db.Model(&model.AuditLog{}).Count(&audits)
	now := time.Now().UTC().Truncate(time.Millisecond)
	updated, e := store.CommitProxyPoolSubscription(ctx, actor, snapshot, snapshot.ConfigCiphertext, 3, network.ProxyPoolConfigurationFacts{SupportsDatagram: true}, now)
	if e != nil || updated.Revision != snapshot.Pool.Revision || updated.LastSyncAt == nil || !updated.LastSyncAt.Equal(now) || updated.NextSyncAt == nil || !updated.NextSyncAt.Equal(now.Add(300*time.Second)) {
		t.Fatal(updated, e)
	}
	if e := store.RecordProxyPoolSubscriptionFailure(ctx, actor, snapshot, "older fetch failed", now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	var synced model.NodeProxyPool
	if e := db.First(&synced, pool.ID).Error; e != nil || synced.LastSyncError != "" || !synced.NextSyncAt.Equal(now.Add(300*time.Second)) {
		t.Fatal("old failure hid successful sync", e)
	}
	var after int64
	db.Model(&model.AuditLog{}).Count(&after)
	if after != audits {
		t.Fatal("unchanged sync allocated audit", audits, after)
	}
	db.Order("node_id").Find(&publications)
	for _, p := range publications {
		if p.Generation != 1 {
			t.Fatal("unchanged sync republished", p)
		}
	}
	if e := service.SetNodeReference(ctx, 1, pool.ID, 1, false); e != nil {
		t.Fatal(e)
	}
	rows, e := q.ListProxyPools(ctx, 1, 2)
	if e != nil || len(rows) != 1 || len(rows[0].Nodes) != 1 || rows[0].Nodes[0].ID != 2 {
		t.Fatal(rows, e)
	}
	var stored model.NodeProxyPool
	if e := db.First(&stored, pool.ID).Error; e != nil || stored.Config != "enc:udp-capable" {
		t.Fatal("detach erased source", e)
	}
}

func TestSharedProxyPoolNodeCreateReferencesAreAtomic(t *testing.T) {
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	if e := db.Model(&model.User{}).Where("id=1").Update("is_admin", true).Error; e != nil {
		t.Fatal(e)
	}
	config := "udp-capable"
	pool, e := proxyPoolMutationService(db).Save(t.Context(), 1, network.ProxyPoolMutationRequest{Name: "new", Config: &config})
	if e != nil {
		t.Fatal(e)
	}
	store := NodeAdministration{DB: db}
	created, e := store.CreateNodeAdministration(t.Context(), 1, network.NodeCreateChange{Name: "new server", Config: "{}", ProxyPoolIDs: []uint{pool.ID, pool.ID}})
	if e != nil {
		t.Fatal(e)
	}
	var n int64
	db.Model(&model.NodeProxyPoolBinding{}).Where("node_id=?", created.ID).Count(&n)
	if n != 1 {
		t.Fatal("reference not deduplicated", n)
	}
	if _, e := store.CreateNodeAdministration(t.Context(), 1, network.NodeCreateChange{Name: "invalid server", Config: "{}", ProxyPoolIDs: []uint{pool.ID, 99999}}); e == nil {
		t.Fatal("invalid pool accepted")
	}
	db.Model(&model.Node{}).Where("name=?", "invalid server").Count(&n)
	if n != 0 {
		t.Fatal("failed creation left node", n)
	}
	if _, e := (NodeRemoval{DB: db}).RemoveNode(t.Context(), 1, created.ID); e != nil {
		t.Fatal(e)
	}
	var remaining model.NodeProxyPool
	if e := db.First(&remaining, pool.ID).Error; e != nil || remaining.Config != "enc:udp-capable" {
		t.Fatal("node removal destroyed global source", e)
	}
	db.Model(&model.NodeProxyPoolBinding{}).Where("node_id=?", created.ID).Count(&n)
	if n != 0 {
		t.Fatal("archived node still attached", n)
	}
}

func TestMySQLSharedProxyPoolLegacyOwnershipUpgrade(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("isolated MySQL required")
	}
	db, _ := administrationFixture(t)
	seedLegacyResources(t, db)
	at := time.Now().UTC().Truncate(time.Millisecond)
	pool := model.NodeProxyPool{NodeID: 1, Name: "legacy owned", Config: "opaque-config", SubscriptionURL: "opaque-source", Revision: 35, LastSyncAt: &at, NextSyncAt: &at}
	if e := db.Create(&pool).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("DELETE FROM schema_migrations WHERE version=?", "0094_shared_proxy_pools.up.sql").Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Migrator().DropTable(&model.NodeProxyPoolBinding{}); e != nil {
		t.Fatal(e)
	}
	if e := db.Exec("ALTER TABLE node_proxy_pools MODIFY COLUMN node_id bigint unsigned NOT NULL").Error; e != nil {
		t.Fatal(e)
	}
	if e := datastore.RunMigrations(db); e != nil {
		t.Fatal(e)
	}
	var current model.NodeProxyPool
	if e := db.First(&current, pool.ID).Error; e != nil || current.NodeID != 0 || current.Config != pool.Config || current.SubscriptionURL != pool.SubscriptionURL || current.Revision != 35 || !current.NextSyncAt.Equal(at) {
		t.Fatal("legacy source changed", e)
	}
	var count int64
	if e := db.Model(&model.NodeProxyPoolBinding{}).Where("node_id=1 AND proxy_pool_id=?", pool.ID).Count(&count).Error; e != nil || count != 1 {
		t.Fatal("old reference lost", e, count)
	}
	if e := datastore.RunMigrations(db); e != nil {
		t.Fatal("repeat startup", e)
	}
}
