package handler

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCompletionResumesPrincipalProjectionRaceBeforeAccounting(t *testing.T) {
	assertCompletionProjectionRace(t, newOrderFixture(t), false)
}
func TestMySQLCompletionResumesPrincipalProjectionRaceBeforeAccounting(t *testing.T) {
	assertCompletionProjectionRace(t, newMySQLOrderFixture(t), false)
}
func TestCompletionResumesInvalidationDuringPreparation(t *testing.T) {
	assertCompletionProjectionRace(t, newOrderFixture(t), true)
}
func TestMySQLCompletionResumesInvalidationDuringPreparation(t *testing.T) {
	assertCompletionProjectionRace(t, newMySQLOrderFixture(t), true)
}
func assertCompletionProjectionRace(t *testing.T, f orderFixture, before bool) {
	h, credential := accountingFixtureFromOrder(t, f)
	plain, err := h.credentialCipher.Decrypt(credential.Secret)
	if err != nil {
		t.Fatal(err)
	}
	var injected atomic.Bool
	const callback = "test:completion-projection-race"
	processor := h.db.Callback().Query().After("gorm:query")
	if before {
		processor = h.db.Callback().Query().Before("gorm:query")
	}
	if err := processor.Register(callback, func(q *gorm.DB) {
		if _, transactional := q.Statement.ConnPool.(gorm.TxCommitter); transactional {
			return
		}
		sql := q.Statement.SQL.String()
		match := strings.HasPrefix(sql, "SELECT `id`") && strings.Contains(sql, "idx_credential_lookup_backfill")
		if before {
			limit, ok := q.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
			match = ok && limit.Limit != nil && *limit.Limit == 1 && q.Statement.TableExpr != nil && strings.Contains(q.Statement.TableExpr.SQL, "idx_credential_lookup_backfill")
		}
		if match && injected.CompareAndSwap(false, true) {
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

func TestCompletionNeverAcknowledgesUnsettledRepeatedPreparationInvalidation(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newOrderFixture(t))
	var invalidations atomic.Int32
	const callback = "test:repeated-prepare-invalidation"
	if err := h.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if _, transactional := q.Statement.ConnPool.(gorm.TxCommitter); transactional {
			return
		}
		limit, ok := q.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		if !ok || limit.Limit == nil || *limit.Limit != 1 || q.Statement.TableExpr == nil || !strings.Contains(q.Statement.TableExpr.SQL, "idx_credential_lookup_backfill") {
			return
		}
		if err := h.db.Exec("UPDATE protocol_credentials SET principal_lookup_version = 0 WHERE id = ?", credential.ID).Error; err != nil {
			t.Error(err)
		}
		invalidations.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Callback().Query().Remove(callback) })
	event := metering.CompletedFlow{NodeID: credential.NodeID, CoreInstanceID: "repeated-prepare-core", EventID: "repeated-prepare-event", EventType: "flow.completed", Sequence: 1, FlowID: "repeated-prepare-flow", PrincipalKey: credential.PrincipalKey, BytesDown: 30, OccurredAt: time.Now().UTC()}
	record, exhausted, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), event)
	if !errors.Is(err, credentialstore.ErrPrincipalIndexIncomplete) || record.ID != 0 || exhausted || invalidations.Load() != 3 {
		t.Fatal("bounded pending preparation was falsely acknowledged", record.ID, exhausted, invalidations.Load(), err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("node_id = ? AND report_id = ?", event.NodeID, event.EventID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("unsettled traffic fact was committed", count, err)
	}
}
