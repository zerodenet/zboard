package datastore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedProxyPoolMigrationPreservesSourceAndOwnershipReferences(t *testing.T) {
	db, e := OpenWithDriver(DriverSQLite, filepath.Join(t.TempDir(), "pools.db"))
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if e := RunMigrations(db); e != nil {
		t.Fatal(e)
	}
	for _, node := range []model.Node{{ID: 1, Name: "old owner", Config: "{}"}, {ID: 2, Name: "other server", Config: "{}"}} {
		if e := db.Create(&node).Error; e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	pool := model.NodeProxyPool{NodeID: 1, Name: "old pool", Config: "encrypted-config", SubscriptionURL: "encrypted-source", Revision: 17, AutoSync: true, NextSyncAt: &now, LastSyncAt: &now}
	if e := db.Create(&pool).Error; e != nil {
		t.Fatal(e)
	}
	endpoint := model.ProtocolEndpoint{NodeID: 2, Name: "test", RuntimeKey: "migration-test", Protocol: "vless", Port: 443, PublicPort: 443, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if e := db.Create(&endpoint).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&model.NetworkEntry{Name: "old entry", NodeID: 2, EndpointID: endpoint.ID, Port: 12345, PublicPort: 12345, ProxyPoolID: &pool.ID}).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Where("version=?", sharedProxyPoolsVersion).Delete(&schemaMigration{}).Error; e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db); e != nil {
		t.Fatal(e)
	}
	var stored model.NodeProxyPool
	if e := db.First(&stored, pool.ID).Error; e != nil {
		t.Fatal(e)
	}
	if stored.NodeID != 0 || stored.Config != pool.Config || stored.SubscriptionURL != pool.SubscriptionURL || stored.Revision != 17 || !stored.NextSyncAt.Equal(now) || !stored.LastSyncAt.Equal(now) {
		t.Fatal("migration rewrote source", stored)
	}
	var refs []model.NodeProxyPoolBinding
	db.Order("node_id").Find(&refs)
	if len(refs) != 2 || refs[0].NodeID != 1 || refs[1].NodeID != 2 {
		t.Fatal("missing old references", refs)
	}
	if e := RunMigrations(db); e != nil {
		t.Fatal("repeat startup", e)
	}
	if e := db.Migrator().DropTable(&model.NodeProxyPoolBinding{}); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db); e == nil {
		t.Fatal("recorded references silently rebuilt")
	}
	if db.Migrator().HasTable(&model.NodeProxyPoolBinding{}) {
		t.Fatal("lost authority recreated")
	}
}
