package metering

import (
	"context"
	"errors"
	"testing"
	"time"
)

type summaryRecordsRepository struct {
	fullCalls, summaryCalls int
	actor                   uint
	query                   RecordsQuery
	err                     error
}

func (r *summaryRecordsRepository) Read(_ context.Context, actor uint, q RecordsQuery) (RecordsSnapshot, error) {
	r.fullCalls++
	r.actor, r.query = actor, q
	return RecordsSnapshot{Records: []TrafficRecord{{Meta: "retained full evidence"}}}, r.err
}

func (r *summaryRecordsRepository) ReadSummaryPage(_ context.Context, actor uint, q RecordsQuery) (RecordsSummaryPage, error) {
	r.summaryCalls++
	r.actor, r.query = actor, q
	return RecordsSummaryPage{Records: []TrafficRecordSummary{{ID: 7}}}, r.err
}

func TestRecordsSummaryRequiresExplicitMethodAndSharedValidation(t *testing.T) {
	r := &summaryRecordsRepository{}
	s := Records{Repository: r}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	q := RecordsQuery{UserID: 999, Paged: true, Limit: 2, From: from, To: from.Add(24 * time.Hour)}
	full, err := s.Read(context.Background(), 1, q)
	if err != nil || len(full.Records) != 1 || full.Records[0].Meta != "retained full evidence" || r.fullCalls != 1 || r.summaryCalls != 0 || r.query.UserID != 1 {
		t.Fatalf("Paged changed full contract: %+v %+v %v", full, r, err)
	}
	page, err := s.ReadSummaryPage(context.Background(), 1, q)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != 7 || r.fullCalls != 1 || r.summaryCalls != 1 || r.actor != 1 || r.query.UserID != 1 {
		t.Fatalf("explicit summary: %+v %+v %v", page, r, err)
	}
	invalid := []RecordsQuery{q, q, q, q, q}
	invalid[0].Limit = 201
	invalid[1].Offset = -1
	invalid[2].To = from
	invalid[3].To = from.Add(367 * 24 * time.Hour)
	invalid[4].Cursor = &RecordCursor{At: from, ID: 1, Direction: "sideways"}
	for _, bad := range invalid {
		for _, summary := range []bool{false, true} {
			var err error
			if summary {
				_, err = s.ReadSummaryPage(context.Background(), 1, bad)
			} else {
				_, err = s.Read(context.Background(), 1, bad)
			}
			var validation *PolicyValidation
			if !errors.As(err, &validation) {
				t.Fatalf("invalid query reached repository: %+v %v", bad, err)
			}
		}
	}
	q.Paged = false
	if _, err := s.ReadSummaryPage(context.Background(), 1, q); err == nil {
		t.Fatal("unbounded summary accepted")
	}
	if full, err := s.Read(context.Background(), 1, q); err != nil || full.Records[0].Meta != "retained full evidence" {
		t.Fatalf("unpaged full contract changed: %+v %v", full, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ReadSummaryPage(ctx, 1, q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.ReadSummaryPage(context.Background(), 0, q); !errors.Is(err, ErrTrendPermission) {
		t.Fatal(err)
	}
	if r.fullCalls != 2 || r.summaryCalls != 1 {
		t.Fatalf("invalid request reached repository: %+v", r)
	}
	r.err = errors.New("read failed")
	q.Paged = true
	if _, err := s.ReadSummaryPage(context.Background(), 1, q); !errors.Is(err, r.err) {
		t.Fatal("repository failure lost", err)
	}
}
