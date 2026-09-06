package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestSQLiteStartupCreatesDistinctAccessForSameUsersSubscriptions(t *testing.T) {
	f := newOrderFixture(t)
	first := f.paid(t, f.create(t, 0).ID)
	second := f.paid(t, f.create(t, 0).ID)
	for i := 0; i < 2; i++ {
		if err := f.h.ReconcileSubscriptionAccessTokens(); err != nil {
			t.Fatal(err)
		}
	}
	var tokens []model.SubscriptionToken
	if err := f.h.db.Order("subscription_id").Find(&tokens).Error; err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[0].SubscriptionID == nil || tokens[1].SubscriptionID == nil || *tokens[0].SubscriptionID != first.SubscriptionID || *tokens[1].SubscriptionID != second.SubscriptionID || tokens[0].TokenHash == tokens[1].TokenHash {
		t.Fatalf("subscription access is not isolated: token count=%d", len(tokens))
	}
}
