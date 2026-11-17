package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"gorm.io/gorm"
	"reflect"
	"testing"
)

type prefixCheckpointSpool struct {
	checkpointTestSpool
	failAt  uint64
	commits []uint64
}

func (s *prefixCheckpointSpool) ReadBatch(ctx context.Context, limit int) (zeroevent.Batch, error) {
	b, e := s.checkpointTestSpool.ReadBatch(ctx, limit)
	if e != nil {
		return b, e
	}
	for i := range b.Events {
		b.Positions = append(b.Positions, zeroevent.Checkpoint{Record: uint64(s.position + i + 1)})
	}
	return b, nil
}
func (s *prefixCheckpointSpool) Commit(ctx context.Context, next zeroevent.Checkpoint) error {
	if s.failAt == next.Record {
		return errors.New("injected checkpoint persistence failure")
	}
	s.commits = append(s.commits, next.Record)
	return s.checkpointTestSpool.Commit(ctx, next)
}
func TestConsumerCommitsSuccessfulPrefixBeforeLaterAccountingFailure(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	assertConsumerPrefix(t, h, c, 32)
}
func TestMySQLConsumerCommitsSuccessfulPrefixBeforeLaterAccountingFailure(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	assertConsumerPrefix(t, h, c, 128)
}
func assertConsumerPrefix(t *testing.T, h *handlers, c model.ProtocolCredential, chunk int) {
	t.Helper()
	count := 2*chunk + 7
	events := accountingBenchmarkEvents(c, 0, count)
	spool := &prefixCheckpointSpool{checkpointTestSpool: checkpointTestSpool{events: events}}
	failure := errors.New("injected later report failure")
	failID := events[2*chunk+3].ID
	callback := "test:later-report-failure"
	if e := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "metering_report_receipts" {
			return
		}
		v := tx.Statement.ReflectValue
		for v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				r := v.Index(i)
				if r.Kind() == reflect.Ptr {
					r = r.Elem()
				}
				if r.FieldByName("ReportID").String() == failID {
					tx.AddError(failure)
				}
			}
		}
	}); e != nil {
		t.Fatal(e)
	}
	got, e := h.consumeZeroEventBatch(t.Context(), spool, 2000)
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(e, failure) || got != 2*chunk || spool.position != 2*chunk {
		t.Fatalf("prefix count=%d position=%d err=%v", got, spool.position, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, int64(2*chunk*30), subStatusActive)
	if got, e = h.consumeZeroEventBatch(t.Context(), spool, 2000); e != nil || got != 7 || spool.position != count {
		t.Fatalf("resume count=%d position=%d err=%v", got, spool.position, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, int64(count*30), subStatusActive)
	if len(spool.limits) != 2 || spool.limits[0] != 2000 || spool.limits[1] != 2000 {
		t.Fatal("changed the read window instead of acknowledging progress", spool.limits)
	}
}
func TestConsumerPrefixCheckpointFailureReplaysOnlyUnacknowledgedSuffix(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	spool := &prefixCheckpointSpool{checkpointTestSpool: checkpointTestSpool{events: accountingBenchmarkEvents(c, 0, 65)}, failAt: 64}
	n, e := h.consumeZeroEventBatch(t.Context(), spool, 2000)
	if e == nil || n != 32 || spool.position != 32 {
		t.Fatal(n, spool.position, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 1920, subStatusActive)
	spool.failAt = 0
	n, e = h.consumeZeroEventBatch(t.Context(), spool, 2000)
	if e != nil || n != 33 || spool.position != 65 {
		t.Fatal(n, spool.position, e)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 1950, subStatusActive)
}
