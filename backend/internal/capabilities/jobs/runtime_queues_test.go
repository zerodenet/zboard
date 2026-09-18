package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runtimeQueueSourceStub struct {
	summary RuntimeQueueSummary
	page    RuntimeQueuePage
	err     error
	now     time.Time
	limit   int
	offset  int
}

func (s *runtimeQueueSourceStub) Summary(_ context.Context, now time.Time) (RuntimeQueueSummary, error) {
	s.now = now
	return s.summary, s.err
}

func (s *runtimeQueueSourceStub) Page(_ context.Context, now time.Time, limit, offset int) (RuntimeQueuePage, error) {
	s.now, s.limit, s.offset = now, limit, offset
	return s.page, s.err
}

func TestRuntimeQueuesOwnSelectionAndPagingBounds(t *testing.T) {
	admin := &runtimeQueueSourceStub{summary: RuntimeQueueSummary{ID: AdminRuntimeQueue}, page: RuntimeQueuePage{Total: 2}}
	publication := &runtimeQueueSourceStub{summary: RuntimeQueueSummary{ID: PublicationRuntimeQueue}, page: RuntimeQueuePage{Total: 3}}
	queues := RuntimeQueues{Admin: admin, Publication: publication}
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.FixedZone("test", 8*60*60))

	summaries, err := queues.Summaries(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].ID != PublicationRuntimeQueue || summaries[1].ID != AdminRuntimeQueue {
		t.Fatalf("unexpected queue order: %#v", summaries)
	}
	if !admin.now.Equal(now.UTC()) || admin.now.Location() != time.UTC || !publication.now.Equal(now.UTC()) {
		t.Fatalf("queue sources did not receive UTC observation time: admin=%v publication=%v", admin.now, publication.now)
	}

	page, err := queues.Page(context.Background(), AdminRuntimeQueue, now, 25, 50)
	if err != nil || page.Total != 2 {
		t.Fatalf("admin page = %#v, %v", page, err)
	}
	if admin.limit != 25 || admin.offset != 50 {
		t.Fatalf("paging arguments = %d/%d", admin.limit, admin.offset)
	}
	for _, input := range []struct {
		name          string
		limit, offset int
	}{{"unknown", 25, 0}, {AdminRuntimeQueue, 0, 0}, {AdminRuntimeQueue, 51, 0}, {AdminRuntimeQueue, 25, -1}, {AdminRuntimeQueue, 25, 1000001}} {
		if _, err := queues.Page(context.Background(), input.name, now, input.limit, input.offset); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Page(%q,%d,%d) error = %v, want ErrInvalid", input.name, input.limit, input.offset, err)
		}
	}
}
