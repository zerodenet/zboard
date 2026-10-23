package handler

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/clause"
)

func TestMySQLAccountingUserForeignKeyDoesNotInvertCommerceLockOrder(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	held := h.db.Begin()
	if held.Error != nil {
		t.Fatal(held.Error)
	}
	defer held.Rollback()
	var user model.User
	if err := held.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, credential.UserID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 1)) }()
	var schema string
	if err := h.db.Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int64
		if err := h.db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='users'`, schema).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Probe exactly the subsequent commerce lock, without creating a test-only
	// timeout/deadlock cycle. Always release and join the accounting goroutine.
	probe := h.db.Begin()
	var sub model.Subscription
	probeErr := probe.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&sub, credential.SubscriptionID).Error
	probe.Rollback()
	releaseErr := held.Commit().Error
	accountingErr := <-done
	if !waiting {
		t.Fatal("accounting never reached the held user row")
	}
	if probeErr != nil {
		t.Fatalf("accounting held subscription while waiting for user, inverting commerce order: %v", probeErr)
	}
	if releaseErr != nil || accountingErr != nil {
		t.Fatal(releaseErr, accountingErr)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
}
