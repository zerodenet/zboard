package handler

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The opt-in account must be able to create/drop databases. Only the random
// schema created here is ever migrated or dropped; a supplied DBName is ignored.
func newMySQLPublishHandlers(t *testing.T) (*handlers, string) {
	t.Helper()
	dsn := os.Getenv("ZBOARD_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set ZBOARD_TEST_MYSQL_DSN to run isolated real MySQL integration tests")
	}
	config, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName, config.ParseTime, config.Loc = "", true, time.UTC
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
	admin, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := "zboard_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + schema + "`"); err != nil {
			t.Errorf("clean test schema: %v", err)
		}
	})
	config.DBName = schema
	db, err := datastore.Open(config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	for _, migrate := range []func(*gorm.DB) error{
		datastore.RunMigrations, datastore.ReconcileCommerceSchema, datastore.ReconcileSubscriptionAccessSchema,
		datastore.ReconcileZeroEventSchema, datastore.ReconcileTrafficReadSchema,
		datastore.ReconcileOperationsSchema, datastore.ReconcileFairUseTelemetrySchema,
	} {
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	h, err := NewHandlers(db, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	return h, config.FormatDSN()
}

func TestMySQLPublishQueueConcurrencyAndRecovery(t *testing.T) {
	h, dsn := newMySQLPublishHandlers(t)
	if err := h.db.Create(&model.Node{ID: 1, Name: "mysql-publish", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	// Upgrade an already-recorded baseline without replaying or losing node data.
	if err := h.db.Exec("DROP TABLE node_config_publishes").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(h.db); err != nil {
		t.Fatal(err)
	}
	var node model.Node
	if err := h.db.First(&node, 1).Error; err != nil || node.Name != "mysql-publish" {
		t.Fatalf("upgrade lost node: %v", err)
	}
	other, err := datastore.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := other.DB()
	t.Cleanup(func() { _ = pool.Close() })
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			db := h.db
			if i%2 == 1 {
				db = other
			}
			errs <- enqueueNodeConfigPublish(db, 1, uint(i+1), 0)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var pending model.NodeConfigPublish
	if err := other.First(&pending, 1).Error; err != nil || pending.Generation != 16 {
		t.Fatalf("concurrent requests lost: %+v %v", pending, err)
	}
	now := time.Now().UTC().Add(time.Second).Truncate(time.Millisecond)
	claims := make(chan model.NodeConfigPublish, 16)
	errs = make(chan error, 16)
	start = make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			db := h.db
			if i%2 == 1 {
				db = other
			}
			item, ok, err := claimNodeConfigPublish(db, now)
			errs <- err
			if ok {
				claims <- item
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(claims) != 1 {
		t.Fatalf("duplicate ownership: %d claims", len(claims))
	}
	old := <-claims
	if err := enqueueNodeConfigPublish(other, 1, 99, 0); err != nil {
		t.Fatal(err)
	}
	if err := finishNodeConfigPublish(h.db, old, now, nil); err != nil {
		t.Fatal(err)
	}
	current := mustClaimPublish(t, h, now)
	if current.Generation != 17 || current.EndpointID != 99 {
		t.Fatalf("new generation lost: %+v", current)
	}
	recovered, ok, err := claimNodeConfigPublish(other, now.Add(nodePublishLease+time.Second))
	if err != nil || !ok {
		t.Fatalf("lease recovery: %t %v", ok, err)
	}
	if err := finishNodeConfigPublish(h.db, current, now, errors.New("late failure")); err != nil {
		t.Fatal(err)
	}
	if err := other.First(&pending, 1).Error; err != nil || pending.LeaseToken != recovered.LeaseToken {
		t.Fatalf("stale worker overwrote owner: %+v %v", pending, err)
	}
	retryAt := now.Add(nodePublishLease + time.Second)
	if err := finishNodeConfigPublish(other, recovered, retryAt, errors.New("offline")); err != nil {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, retryAt.Add(4*time.Second))
	retry := mustClaimPublish(t, h, retryAt.Add(5*time.Second))
	if retry.Attempts != 1 || retry.LastError != "offline" {
		t.Fatalf("retry lost: %+v", retry)
	}
	if err := finishNodeConfigPublish(h.db, retry, retryAt, nil); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("rollback")
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := enqueueNodeConfigPublish(tx, 1, 100, 0); err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, now.Add(time.Hour))
	mustEnqueuePublish(t, h, 101)
	if err := h.db.Delete(&model.Node{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, now.Add(time.Hour))
}
