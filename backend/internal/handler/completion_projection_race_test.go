package handler

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestCompletionResumesPrincipalProjectionRaceBeforeAccounting(t *testing.T) {
	assertCompletionProjectionRace(t, newOrderFixture(t))
}
func TestMySQLCompletionResumesPrincipalProjectionRaceBeforeAccounting(t *testing.T) {
	assertCompletionProjectionRace(t, newMySQLOrderFixture(t))
}
func assertCompletionProjectionRace(t *testing.T, f orderFixture) {
	h, credential := accountingFixtureFromOrder(t, f)
	plain, err := h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	const callback = "test:completion-projection-race"
	if err := h.db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
		if _, transactional := q.Statement.ConnPool.(gorm.TxCommitter); transactional {
			return
		}
		sql := q.Statement.SQL.String()
		if strings.HasPrefix(sql, "SELECT `id`") && strings.Contains(sql, "idx_credential_lookup_backfill") && injected.CompareAndSwap(false, true) {
			// Lose only rebuildable coverage after the completed preparation query.
			// Canonical identity, quota, deduplication and durable facts are untouched.
			if err := h.db.Exec("UPDATE protocol_credentials SET principal_lookup_version = 0 WHERE id = ?", credential.ID).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Callback().Query().Remove(callback) })
	event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "projection-race-core", EventID: "projection-race-completed", EventType: "flow.completed", Sequence: 1, FlowID: "projection-race-flow", PrincipalKey: plain, BytesDown: 30, OccurredAt: time.Now().UTC()}
	for range 2 {
		record, exhausted, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), event)
		if err != nil || exhausted || record.UsedBytes != 30 {
			t.Fatalf("safe projection recovery did not settle exactly once: used=%d exhausted=%v err=%v", record.UsedBytes, exhausted, err)
		}
	}
	if !injected.Load() {
		t.Fatal("projection race was not exercised")
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	var retained model.ProtocolCredential
	if err := h.db.First(&retained, credential.ID).Error; err != nil || retained.PrincipalLookupVersion != 1 || retained.Secret != credential.Secret || retained.PrincipalKey != credential.PrincipalKey {
		t.Fatal("projection recovery changed canonical identity or failed to rebuild", err)
	}
}
