package handler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func openPublishTestHandlers(t *testing.T, path string) *handlers {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandlers(db, "0123456789abcdef0123456789abcdef", newTestCredentialCipher(t), "", "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func newPublishFixture(t *testing.T) (*handlers, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "publish.db")
	h := openPublishTestHandlers(t, path)
	if err := h.db.Create(&model.Node{ID: 1, Name: "publish-test"}).Error; err != nil {
		t.Fatal(err)
	}
	return h, path
}
func mustEnqueuePublish(t *testing.T, h *handlers, endpoint uint) {
	t.Helper()
	if err := enqueueNodeConfigPublish(h.db, 1, endpoint, 0); err != nil {
		t.Fatal(err)
	}
}
func mustClaimPublish(t *testing.T, h *handlers, now time.Time) model.NodeConfigPublish {
	t.Helper()
	item, ok, err := claimNodeConfigPublish(h.db, now)
	if err != nil || !ok {
		t.Fatalf("claim: %t %v", ok, err)
	}
	return item
}
func assertNoPublishClaim(t *testing.T, h *handlers, now time.Time) {
	t.Helper()
	_, ok, err := claimNodeConfigPublish(h.db, now)
	if err != nil || ok {
		t.Fatalf("unexpected claim: %t %v", ok, err)
	}
}

func TestDurablePublishCoalescesAndPreservesInflightUpdates(t *testing.T) {
	for _, failure := range []error{nil, errors.New("offline")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			h, _ := newPublishFixture(t)
			mustEnqueuePublish(t, h, 11)
			mustEnqueuePublish(t, h, 12)
			now := time.Now().UTC().Add(time.Second)
			first := mustClaimPublish(t, h, now)
			if first.EndpointID != 12 || first.Generation != 2 {
				t.Fatalf("coalesced request: %+v", first)
			}
			mustEnqueuePublish(t, h, 13)
			assertNoPublishClaim(t, h, now)
			if err := finishNodeConfigPublish(h.db, first, now, failure); err != nil {
				t.Fatal(err)
			}
			next := mustClaimPublish(t, h, now)
			if next.EndpointID != 13 || next.Generation != 3 || next.Attempts != 0 {
				t.Fatalf("new request lost: %+v", next)
			}
			if err := finishNodeConfigPublish(h.db, next, now, nil); err != nil {
				t.Fatal(err)
			}
			assertNoPublishClaim(t, h, now.Add(time.Hour))
		})
	}
}

func TestDurablePublishFailureSurvivesReopenAndBacksOff(t *testing.T) {
	h, path := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	now := time.Now().UTC().Add(time.Second)
	item := mustClaimPublish(t, h, now)
	if err := finishNodeConfigPublish(h.db, item, now, errors.New("node offline")); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := h.db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := openPublishTestHandlers(t, path)
	assertNoPublishClaim(t, restarted, now.Add(4*time.Second))
	retry := mustClaimPublish(t, restarted, now.Add(5*time.Second))
	if retry.Attempts != 1 || retry.LastError != "node offline" {
		t.Fatalf("retry state lost: %+v", retry)
	}
	if err := finishNodeConfigPublish(restarted.db, retry, now.Add(5*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, restarted, now.Add(time.Hour))
}

func TestDurablePublishRecoversExpiredLeaseAndFencesOldAcknowledgement(t *testing.T) {
	h, path := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	now := time.Now().UTC().Add(time.Second)
	old := mustClaimPublish(t, h, now)
	restarted := openPublishTestHandlers(t, path)
	assertNoPublishClaim(t, restarted, now.Add(nodePublishLease-time.Second))
	current := mustClaimPublish(t, restarted, now.Add(nodePublishLease+time.Second))
	for _, failure := range []error{nil, errors.New("late error")} {
		if err := finishNodeConfigPublish(h.db, old, now.Add(nodePublishLease+time.Second), failure); err != nil {
			t.Fatal(err)
		}
		var stored model.NodeConfigPublish
		if err := h.db.First(&stored, 1).Error; err != nil || stored.LeaseToken != current.LeaseToken {
			t.Fatalf("stale acknowledgement stole claim: %+v %v", stored, err)
		}
	}
}

func TestPublishWorkerScansPersistedWorkWithoutNewRequest(t *testing.T) {
	h, path := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	restarted := openPublishTestHandlers(t, path)
	delivered := make(chan uint, 1)
	restarted.startNodePublishWorker(func(ctx context.Context, item model.NodeConfigPublish) error { delivered <- item.NodeID; return nil })
	defer restarted.CloseNodePublishWorker()
	select {
	case node := <-delivered:
		if node != 1 {
			t.Fatalf("node=%d", node)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not recover stored work")
	}
}

func TestPublishQueueParticipatesInTransactionAndNodeDeletion(t *testing.T) {
	h, _ := newPublishFixture(t)
	sentinel := errors.New("rollback")
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := enqueueNodeConfigPublish(tx, 1, 11, 0); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, time.Now().Add(time.Hour))
	mustEnqueuePublish(t, h, 11)
	if err := h.db.Delete(&model.Node{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, time.Now().Add(time.Hour))
}

func TestPublishExecutorRetainsFailureAndUnacknowledgedSuccess(t *testing.T) {
	h, _ := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	now := time.Now().UTC().Add(time.Second)
	first := mustClaimPublish(t, h, now)
	h.executeNodePublish(context.Background(), first, func(context.Context) error { return errors.New("offline") })
	var failed model.NodeConfigPublish
	if err := h.db.First(&failed, 1).Error; err != nil || failed.Attempts != 1 || failed.LeaseToken != "" {
		t.Fatalf("failed execution lost retry: %+v %v", failed, err)
	}
	retry := mustClaimPublish(t, h, now.Add(time.Minute))
	if err := h.db.Exec(`CREATE TRIGGER reject_publish_ack BEFORE DELETE ON node_config_publishes
 BEGIN SELECT RAISE(ABORT, 'ack unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	h.executeNodePublish(context.Background(), retry, func(context.Context) error { return nil })
	if err := h.db.Exec("DROP TRIGGER reject_publish_ack").Error; err != nil {
		t.Fatal(err)
	}
	recovered := mustClaimPublish(t, h, now.Add(time.Minute+nodePublishLease+time.Second))
	if recovered.LeaseToken == retry.LeaseToken {
		t.Fatal("unacknowledged work was not reclaimed")
	}
}

func TestPublicationSchemaAddsQueueToExistingDatabase(t *testing.T) {
	h, _ := newPublishFixture(t)
	if err := h.db.Migrator().DropTable(&model.NodeConfigPublish{}); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(h.db); err != nil {
		t.Fatal(err)
	}
	if !h.db.Migrator().HasIndex(&model.NodeConfigPublish{}, "idx_node_publish_due") {
		t.Fatal("upgrade omitted due-work index")
	}
	var node model.Node
	if err := h.db.First(&node, 1).Error; err != nil || node.Name != "publish-test" {
		t.Fatal("schema upgrade changed existing node")
	}
	mustEnqueuePublish(t, h, 11)
	mustClaimPublish(t, h, time.Now().UTC().Add(time.Second))
}

func TestPublishClaimRechecksBackoffAfterCandidateRead(t *testing.T) {
	h, _ := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	now := time.Now().UTC().Add(time.Second)
	changed := false
	if err := h.db.Callback().Query().After("gorm:query").Register("test:delay_candidate", func(tx *gorm.DB) {
		if tx.Statement.Table == "node_config_publishes" && !changed {
			changed = true
			if err := h.db.Model(&model.NodeConfigPublish{}).Where("node_id = ?", 1).Update("next_attempt_at", now.Add(time.Hour)).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	assertNoPublishClaim(t, h, now)
}

func TestIndependentPublishWorkersCannotClaimSameNode(t *testing.T) {
	h, path := newPublishFixture(t)
	other := openPublishTestHandlers(t, path)
	mustEnqueuePublish(t, h, 11)
	now := time.Now().UTC().Add(time.Second)
	start := make(chan struct{})
	results := make(chan bool, 4)
	failures := make(chan error, 4)
	for _, worker := range []*handlers{h, other, h, other} {
		go func(h *handlers) {
			<-start
			_, ok, err := claimNodeConfigPublish(h.db, now)
			results <- ok
			failures <- err
		}(worker)
	}
	close(start)
	claims := 0
	for i := 0; i < 4; i++ {
		if <-results {
			claims++
		}
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if claims != 1 {
		t.Fatalf("simultaneous claims=%d, want 1", claims)
	}
}

func TestPublishShutdownRetainsInterruptedExecution(t *testing.T) {
	h, _ := newPublishFixture(t)
	mustEnqueuePublish(t, h, 11)
	started := make(chan struct{})
	h.startNodePublishWorker(func(ctx context.Context, item model.NodeConfigPublish) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	defer h.CloseNodePublishWorker()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	h.CloseNodePublishWorker()
	var pending model.NodeConfigPublish
	if err := h.db.First(&pending, 1).Error; err != nil || pending.LeaseToken != "" || pending.Attempts != 1 {
		t.Fatalf("shutdown lost pending work: %+v %v", pending, err)
	}
}
