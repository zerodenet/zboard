package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

type checkpointTestSpool struct {
	zeroevent.EventSpool
	events    []zeroevent.Envelope
	position  int
	limits    []int
	commitErr error
	complete  context.CancelFunc
}

func (s *checkpointTestSpool) ReadBatch(ctx context.Context, limit int) (zeroevent.Batch, error) {
	if err := ctx.Err(); err != nil {
		return zeroevent.Batch{}, err
	}
	s.limits = append(s.limits, limit)
	end := min(s.position+limit, len(s.events))
	return zeroevent.Batch{Events: s.events[s.position:end], Next: zeroevent.Checkpoint{Record: uint64(end)}}, nil
}

func (s *checkpointTestSpool) Commit(_ context.Context, next zeroevent.Checkpoint) error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.position = int(next.Record)
	if s.position == len(s.events) && s.complete != nil {
		s.complete()
	}
	return nil
}

func (s *checkpointTestSpool) Status() zeroevent.Status { return zeroevent.Status{} }

func TestSQLiteConsumerShortTransactionsContinueAfterFullBurst(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	count := zeroEventConsumerBurstBatches*zeroEventSQLiteBatchLimit + 1
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, count), complete: cancel}
	runtime := &zeroEventRuntime{spool: spool, config: zeroevent.ConsumerConfig{MaxBatch: 2000, CommitInterval: time.Hour}, done: make(chan struct{})}
	go h.runZeroEventConsumer(ctx, runtime)
	<-runtime.done
	if spool.position != count || len(spool.limits) != zeroEventConsumerBurstBatches+1 {
		t.Fatalf("consumer stalled after full burst: position=%d reads=%v", spool.position, spool.limits)
	}
	for _, limit := range spool.limits {
		if limit != zeroEventSQLiteBatchLimit {
			t.Fatalf("SQLite transaction limit=%d", limit)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, int64(count*30), subStatusActive)
}

func TestConsumerCheckpointFailureReplaysCommittedAccountingExactlyOnce(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	spool := &checkpointTestSpool{events: accountingBenchmarkEvents(credential, 0, 33), commitErr: errors.New("checkpoint write failed")}
	if count, err := h.consumeZeroEventBatch(context.Background(), spool, 32); err == nil || count != 0 || spool.position != 0 {
		t.Fatalf("failed checkpoint advanced: count=%d position=%d error=%v", count, spool.position, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 960, subStatusActive)
	spool.commitErr = nil
	for _, want := range []int{32, 1, 0} {
		if count, err := h.consumeZeroEventBatch(context.Background(), spool, 32); err != nil || count != want {
			t.Fatalf("retry count=%d want=%d error=%v", count, want, err)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 990, subStatusActive)
}

func TestConsumerBatchSizingPreservesConfiguredSmallerAndMySQLLimits(t *testing.T) {
	for _, item := range []struct {
		configured int
		sqlite     bool
		want       int
	}{{2000, true, 32}, {8, true, 8}, {2000, false, 2000}} {
		if got := zeroEventConsumerBatchLimit(item.configured, item.sqlite); got != item.want {
			t.Fatalf("batch size=%d want=%d", got, item.want)
		}
	}
	if got := zeroEventConsumerNextInterval(time.Hour, zeroevent.Status{}, true); got != zeroEventConsumerMinimumInterval {
		t.Fatalf("full backlog interval=%s", got)
	}
	if got := zeroEventConsumerNextInterval(time.Hour, zeroevent.Status{}, false); got != time.Hour {
		t.Fatalf("idle interval=%s", got)
	}
}
