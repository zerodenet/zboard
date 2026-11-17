package handler

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm/clause"
	"testing"
	"time"
)

func TestMySQLSettledReplayDoesNotWaitForMutableQuota(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	events := accountingBenchmarkEvents(c, 0, 1)
	if e := h.projectZeroNodeEvents(t.Context(), events); e != nil {
		t.Fatal(e)
	}
	for _, stage := range []string{"hot", "compact", "sealed"} {
		if stage == "compact" {
			old := time.Now().UTC().Add(-48 * time.Hour)
			if e := h.db.Model(&model.FlowUsage{}).Where("node_id=?", c.NodeID).Update("updated_at", old).Error; e != nil {
				t.Fatal(e)
			}
			if _, e := (meteringstore.FlowCursorCompaction{DB: h.db}).Drain(t.Context(), 100); e != nil {
				t.Fatal(e)
			}
		}
		if stage == "sealed" {
			if _, e := (meteringstore.ColdIdentities{DB: h.db}).Drain(t.Context(), 32); e != nil {
				t.Fatal(e)
			}
			var count int64
			if e := h.db.Model(&model.FlowCursorCheckpoint{}).Where("node_id=?", c.NodeID).Count(&count).Error; e != nil || count != 0 {
				t.Fatal("floor not sealed", count, e)
			}
		}
		lock := h.db.Begin()
		if lock.Error != nil {
			t.Fatal(lock.Error)
		}
		defer lock.Rollback()
		var sub model.Subscription
		if e := lock.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, c.SubscriptionID).Error; e != nil {
			t.Fatal(e)
		}
		// A proof must not lock a sealed page for update either. A concurrent
		// accounting writer may hold it while preserving the same lower floor.
		if stage == "sealed" {
			var blocks []model.MeteringColdBlock
			if e := lock.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=?", c.NodeID).Find(&blocks).Error; e != nil || len(blocks) == 0 {
				t.Fatal("sealed pages missing", e)
			}
		}
		for _, newID := range []bool{false, true} {
			replay := append([]zeroevent.Envelope(nil), events...)
			if newID {
				replay[0].ID = "older-unreceipted-sample"
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			e := h.projectZeroNodeEvents(ctx, replay)
			cancel()
			if e != nil {
				t.Fatalf("settled replay waited for mutable authority: stage=%s new_id=%v %v", stage, newID, e)
			}
		}
		if e := lock.Rollback().Error; e != nil {
			t.Fatal(e)
		}
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 30, subStatusActive)
	// A larger cumulative value is not proven settled merely by its sequence.
	tail := events[0]
	tail.ID = "unseen-historical-tail"
	tail.Sequence = 2
	tail.Payload = []byte(`{"flow_id":"flow-0","traffic":{"bytes_up":20,"bytes_down":40}}`)
	if e := h.projectZeroNodeEvents(t.Context(), []zeroevent.Envelope{tail}); e != nil {
		t.Fatal(e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 60, subStatusActive)
	var cursor model.FlowUsage
	if e := h.db.Where("node_id=? AND flow_id=?", c.NodeID, metering.FlowUsageKey(tail.CoreInstanceID, tail.FlowID)).First(&cursor).Error; e != nil || cursor.RawBytes != 60 {
		t.Fatal(cursor, e)
	}
}
