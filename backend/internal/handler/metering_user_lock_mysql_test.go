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
	// A mutable live user row is not the financial owner: immutable account
	// subjects keep reporting valid during profile edits and after hard deletion.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		held.Rollback()
		<-done
		t.Fatal("metering waited on a live user profile", ctx.Err())
	}
	if err := held.Commit().Error; err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
}
