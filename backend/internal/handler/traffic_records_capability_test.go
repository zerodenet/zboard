package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestTrafficRecordsCapabilityScopeCursorAndRevocation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedUsage(t)
	service := f.h.services.Records()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	q := metering.RecordsQuery{UserID: 2, Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 2}
	first, err := service.Read(context.Background(), 1, q)
	if err != nil || first.Total != 6 || first.Aggregates.UsedBytes != 210 || len(first.Records) != 2 || !first.HasMore {
		t.Fatalf("first: %+v %v", first, err)
	}
	for _, r := range first.Records {
		if r.UserID != 1 {
			t.Fatal("foreign record")
		}
	}
	last := first.Records[1]
	q.Cursor = &metering.RecordCursor{At: last.At, ID: last.ID, Direction: "older"}
	second, err := service.Read(context.Background(), 1, q)
	if err != nil || len(second.Records) != 2 || second.Total != 6 {
		t.Fatalf("second: %+v %v", second, err)
	}
	for _, r := range second.Records {
		for _, old := range first.Records {
			if r.ID == old.ID {
				t.Fatal("repeated cursor record")
			}
		}
	}
	start := second.Records[0]
	q.Cursor = &metering.RecordCursor{At: start.At, ID: start.ID, Direction: "newer"}
	back, err := service.Read(context.Background(), 1, q)
	if err != nil || len(back.Records) != 2 || back.Records[0].ID != first.Records[0].ID || back.Records[1].ID != first.Records[1].ID {
		t.Fatalf("back: %+v %v", back, err)
	}
	q.Cursor = nil
	q.Offset = 2
	offset, err := service.Read(context.Background(), 1, q)
	if err != nil || len(offset.Records) != 2 || offset.Records[0].ID != second.Records[0].ID {
		t.Fatalf("offset: %+v %v", offset, err)
	}
	q.ProtocolEndpointID = 7
	empty, err := service.Read(context.Background(), 1, q)
	if err != nil || empty.Total != 0 || len(empty.Records) != 0 {
		t.Fatalf("foreign endpoint: %+v %v", empty, err)
	}
	q = metering.RecordsQuery{Administrative: true}
	if _, err = service.Read(context.Background(), 99, q); err != nil {
		t.Fatal(err)
	}
	if err = f.h.db.Model(&model.User{}).Where("id = 99").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = service.Read(context.Background(), 99, q); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin: %v", err)
	}
}
