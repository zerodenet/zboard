package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type counterWriterRecorder struct {
	calls  int
	writer NodeCounterWriter
}

func (r *counterWriterRecorder) RecordNodeActivity(_ context.Context, _ uint, _ string, update NodeActivityUpdate) error {
	r.calls++
	r.writer = update.CounterWriter
	return nil
}

func TestObservationActivityWriterIsExplicitAndValidated(t *testing.T) {
	repo := &counterWriterRecorder{}
	service := NodeActivity{Repository: repo}
	update := NodeActivityUpdate{At: time.Unix(1700000000, 0), ConnectorSeen: true}
	if err := service.Record(context.Background(), 1, "opaque", update); err != nil || repo.writer != "" {
		t.Fatal("connector liveness manufactured a counter writer", err)
	}
	for _, writer := range []NodeCounterWriter{CounterWriterHeartbeat, CounterWriterLegacyConnector} {
		update.CounterWriter = writer
		if err := service.Record(context.Background(), 1, "opaque", update); err != nil || repo.writer != writer {
			t.Fatal("explicit counter writer lost", writer, err)
		}
	}
	calls := repo.calls
	update.CounterWriter = "buffered_stats"
	if err := service.Record(context.Background(), 1, "opaque", update); !errors.Is(err, ErrNodeActivityInvalid) || repo.calls != calls {
		t.Fatal("activity could claim a buffered Stats source", err, repo.calls)
	}
}
