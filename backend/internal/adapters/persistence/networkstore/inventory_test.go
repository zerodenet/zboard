package networkstore

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type inventoryQueryCounter struct {
	logger.Interface
	count atomic.Int64
}

func (c *inventoryQueryCounter) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	c.count.Add(1)
	c.Interface.Trace(ctx, begin, query, err)
}

func TestRuntimeNodesUsesOneBatchQuery(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "network-inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	nodes := []model.Node{
		{Name: "batch-a", Config: "{}", IsEnabled: true, NodeCredential: "secret-a"},
		{Name: "batch-b", Config: "{}", IsEnabled: true, NodeCredential: "secret-b"},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	counter := &inventoryQueryCounter{Interface: db.Logger}
	queryDB := db.Session(&gorm.Session{Logger: counter})
	rows, err := (Inventory{DB: queryDB}).RuntimeNodes(context.Background(), []uint{nodes[0].ID, nodes[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got != 1 {
		t.Fatalf("runtime node query count = %d, want 1", got)
	}
	if rows[nodes[0].ID].NodeCredentialCiphertext != "secret-a" || rows[nodes[1].ID].NodeCredentialCiphertext != "secret-b" {
		t.Fatalf("runtime nodes = %+v", rows)
	}
}
