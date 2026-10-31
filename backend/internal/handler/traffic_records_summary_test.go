package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestTrafficRecordsSummaryKeepsFullEvidenceAndRawPageContract(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	checkTrafficRecordsSummary(t, h)
}

func TestMySQLTrafficRecordsSummaryKeepsFullEvidenceAndRawPageContract(t *testing.T) {
	h, _ := newMySQLBusinessHandlers(t)
	checkTrafficRecordsSummary(t, h)
}

// One real prepared fixture per engine exercises full and summary reads,
// permissions, pagination, HTTP serialization and failure propagation together.
func checkTrafficRecordsSummary(t *testing.T, h *handlers) {
	t.Helper()
	const actor, other, admin = uint(201), uint(202), uint(299)
	for _, id := range []uint{actor, other, admin} {
		if err := h.db.Create(&model.User{ID: id, Email: fmt.Sprintf("records-%d@example.test", id), Password: "unused", Status: userStatusActive, IsAdmin: id == admin}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := h.db.Create(&model.Node{ID: 7, Name: "raw-summary-node", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&model.ProtocolEndpoint{ID: 9, NodeID: 7, Name: "raw-summary-endpoint", RuntimeKey: "raw-summary-runtime", Protocol: "socks5", Address: "127.0.0.1", Port: 1080, OptionalConfig: "{}", Tags: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	meta := `{"accounting_samples":1000,"first_sequence":1,"rounding_numerator":500000,"diagnostic":"` + strings.Repeat("m", 32*1024) + `"}`
	var facts []model.TrafficRecord
	for i, at := range []time.Time{from, from.Add(time.Hour), from.Add(time.Hour), from.Add(24*time.Hour - time.Second), from.Add(-time.Second), from.Add(24 * time.Hour), from.Add(12 * time.Hour)} {
		user := actor
		if i == 6 {
			user = other
		}
		fact := model.TrafficRecord{EntitlementEpoch: 5, UserID: user, SubscriptionID: 11, NodeID: 7, ProtocolEndpointID: 9, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i), FlowID: fmt.Sprintf("flow-%d", i), EventType: "flow.updated", EventRevision: uint64(i + 1), RawBytes: int64((i + 1) * 10), UploadBytes: int64(i + 1), DownloadBytes: int64((i + 1) * 9), TrafficCalcMode: 2, ProtocolMultiplierMilli: 1500, UsedBytes: int64((i + 1) * 15), At: at, Meta: meta, CreatedAt: from.Add(48 * time.Hour), UpdatedAt: from.Add(48 * time.Hour)}
		if err := h.db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
		facts = append(facts, fact)
	}
	ctx := context.Background()
	q := metering.RecordsQuery{UserID: other, SubscriptionID: 11, NodeID: 7, ProtocolEndpointID: 9, Paged: true, From: from, To: from.Add(24 * time.Hour), Limit: 2}
	// ReadDB=nil exercises the original primary fallback, including authorization.
	s := metering.Records{Repository: meteringstore.Records{DB: h.db}}
	full, err := s.Read(ctx, actor, q)
	if err != nil || len(full.Records) != 2 || full.Total != 4 || full.Aggregates.UsedBytes != 150 {
		t.Fatalf("full page: %+v %v", full, err)
	}
	assertEvidence := func(rows []metering.TrafficRecord) {
		t.Helper()
		for _, row := range rows {
			if row.Meta != meta || row.EntitlementEpoch != 5 || !row.CreatedAt.Equal(from.Add(48*time.Hour)) || row.ReportID == "" || row.Nonce == "" || row.FlowID == "" || row.EventType != "flow.updated" || row.EventRevision == 0 || row.TrafficCalcMode != 2 {
				t.Fatalf("full record evidence changed: id=%d", row.ID)
			}
		}
	}
	assertEvidence(full.Records)
	unpaged, err := s.Read(ctx, actor, metering.RecordsQuery{})
	if err != nil || len(unpaged.Records) != 6 {
		t.Fatalf("unpaged full: records=%d %v", len(unpaged.Records), err)
	}
	assertEvidence(unpaged.Records)

	var selected []string
	var sourceReads int
	capture := false
	if err := h.db.Callback().Query().After("gorm:query").Register("test:raw_summary_columns", func(tx *gorm.DB) {
		if !capture || tx.Statement.Table != "traffic_records" {
			return
		}
		sourceReads++
		selected = append([]string(nil), tx.Statement.Selects...)
		if rows, ok := tx.Statement.Dest.(*[]model.TrafficRecord); ok {
			for _, row := range *rows {
				if row.Meta != "" || row.ReportID != "" || row.Nonce != "" || row.EntitlementEpoch != 0 || !row.CreatedAt.IsZero() {
					t.Error("summary loaded unrelated evidence")
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Callback().Query().Remove("test:raw_summary_columns") })
	capture = true
	first, err := s.ReadSummaryPage(ctx, actor, q)
	capture = false
	wantColumns := []string{"id", "user_id", "subscription_id", "node_id", "protocol_endpoint_id", "raw_bytes", "upload_bytes", "download_bytes", "protocol_multiplier_milli", "used_bytes", "record_at", "is_aggregate", "record_count"}
	if err != nil || sourceReads != 1 || !reflect.DeepEqual(selected, wantColumns) || first.Total != full.Total || first.Aggregates != full.Aggregates || !first.HasMore || len(first.Records) != 2 {
		t.Fatalf("summary page columns=%v reads=%d result=%+v error=%v", selected, sourceReads, first, err)
	}
	fullModels := make([]model.TrafficRecord, 0, len(full.Records))
	for _, row := range full.Records {
		fullModels = append(fullModels, model.TrafficRecord(row))
	}
	wantJSON, err := json.Marshal(trafficRecordSummaries(fullModels))
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(first.Records)
	if err != nil || string(gotJSON) != string(wantJSON) {
		t.Fatalf("existing raw summary JSON changed: %s %s %v", gotJSON, wantJSON, err)
	}
	last := first.Records[1]
	q.Cursor = &metering.RecordCursor{At: last.At, ID: last.ID, Direction: "older"}
	second, err := s.ReadSummaryPage(ctx, actor, q)
	if err != nil || second.Total != 4 || second.Aggregates != first.Aggregates || second.HasMore || len(second.Records) != 2 || second.Records[0].ID != facts[1].ID || second.Records[1].ID != facts[0].ID {
		t.Fatalf("older tie/edge page: %+v %v", second, err)
	}
	q.Cursor = &metering.RecordCursor{At: second.Records[0].At, ID: second.Records[0].ID, Direction: "newer"}
	back, err := s.ReadSummaryPage(ctx, actor, q)
	if err != nil || !reflect.DeepEqual(back.Records, first.Records) || back.Aggregates != first.Aggregates {
		t.Fatalf("newer page: %+v %v", back, err)
	}
	q.Cursor, q.Offset = nil, 2
	offset, err := s.ReadSummaryPage(ctx, actor, q)
	if err != nil || !reflect.DeepEqual(offset.Records, second.Records) || offset.Aggregates != first.Aggregates {
		t.Fatalf("offset page: %+v %v", offset, err)
	}
	q.Offset = 0
	for _, field := range []string{"subscription", "node", "endpoint"} {
		filtered := q
		switch field {
		case "subscription":
			filtered.SubscriptionID = 999
		case "node":
			filtered.NodeID = 999
		case "endpoint":
			filtered.ProtocolEndpointID = 999
		}
		empty, err := s.ReadSummaryPage(ctx, actor, filtered)
		if err != nil || empty.Total != 0 || len(empty.Records) != 0 || empty.Aggregates.UsedBytes != 0 {
			t.Fatalf("%s scope ignored: %+v %v", field, empty, err)
		}
	}

	// Explicit ReadDB and nil ReadDB use the same authority checks and results.
	read := h.db.Session(&gorm.Session{})
	s = metering.Records{Repository: meteringstore.Records{DB: h.db, ReadDB: read}}
	separate, err := s.ReadSummaryPage(ctx, actor, q)
	if err != nil || !reflect.DeepEqual(separate, first) {
		t.Fatalf("explicit read handle changed page: %+v %v", separate, err)
	}
	for _, administrative := range []bool{false, true} {
		user := actor
		path := "/api/v1/traffic/records?view=raw&paged=true&from=2026-09-01&to=2026-09-01&limit=2"
		if administrative {
			user = admin
			path = "/api/v1/admin/traffic/records?view=raw&paged=true&user_id=201&from=2026-09-01&to=2026-09-01&limit=2"
		}
		token, _, err := h.issueToken(authClaims{UserID: user, Email: fmt.Sprintf("records-%d@example.test", user), IsAdmin: administrative})
		if err != nil {
			t.Fatal(err)
		}
		get := func(target string) (json.RawMessage, pageMetadata) {
			t.Helper()
			r := httptest.NewRecorder()
			capture = true
			h.TrafficUsageRecordsHandler(r, announcementRequest(http.MethodGet, target, token, ""))
			capture = false
			if r.Code != http.StatusOK {
				t.Fatalf("raw HTTP: %d %s", r.Code, r.Body.String())
			}
			var envelope struct {
				Data struct {
					Items json.RawMessage
					Page  pageMetadata
				}
			}
			if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			return envelope.Data.Items, envelope.Data.Page
		}
		items, page := get(path)
		if string(items) != string(wantJSON) || page.Total != 4 || page.NextCursor == nil || !reflect.DeepEqual(selected, wantColumns) {
			t.Fatalf("HTTP summary/fingerprint: items=%s page=%+v columns=%v", items, page, selected)
		}
		nextItems, nextPage := get(path + "&cursor=" + url.QueryEscape(*page.NextCursor))
		secondJSON, err := json.Marshal(second.Records)
		if err != nil || string(nextItems) != string(secondJSON) || nextPage.PreviousCursor == nil {
			t.Fatalf("HTTP older page: %s %+v %v", nextItems, nextPage, err)
		}
		backItems, _ := get(path + "&cursor=" + url.QueryEscape(*nextPage.PreviousCursor))
		if string(backItems) != string(wantJSON) {
			t.Fatalf("HTTP newer page: %s", backItems)
		}
		offsetItems, _ := get(path + "&offset=2")
		if string(offsetItems) != string(secondJSON) {
			t.Fatalf("HTTP offset page: %s", offsetItems)
		}
		// Legacy unpaged raw route keeps Meta and the full record shape.
		r := httptest.NewRecorder()
		h.TrafficUsageRecordsHandler(r, announcementRequest(http.MethodGet, strings.Replace(path, "&paged=true", "", 1), token, ""))
		if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"meta":`) || !strings.Contains(r.Body.String(), `"flow_id":`) {
			t.Fatalf("unpaged raw fields lost: %d", r.Code)
		}
	}

	adminQuery := q
	adminQuery.Administrative, adminQuery.UserID = true, other
	foreign, err := s.ReadSummaryPage(ctx, admin, adminQuery)
	if err != nil || foreign.Total != 1 || len(foreign.Records) != 1 || foreign.Records[0].UserID != other {
		t.Fatalf("admin explicit scope: %+v %v", foreign, err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadSummaryPage(ctx, admin, adminQuery); !errors.Is(err, metering.ErrTrendPermission) {
		t.Fatalf("revoked admin summary: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ReadSummaryPage(canceled, actor, q); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled summary: %v", err)
	}
	injected := errors.New("raw page query failed")
	if err := h.db.Callback().Query().Before("gorm:query").Register("test:fail_raw_summary", func(tx *gorm.DB) {
		if tx.Statement.Table == "traffic_records" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadSummaryPage(ctx, actor, q); !errors.Is(err, injected) {
		t.Fatalf("real page failure hidden: %v", err)
	}
	h.db.Callback().Query().Remove("test:fail_raw_summary")
	// Summary reads must never rewrite any original accounting fact.
	var retained []model.TrafficRecord
	if err := h.db.Order("id").Find(&retained).Error; err != nil || len(retained) != len(facts) {
		t.Fatalf("retained facts: %d %v", len(retained), err)
	}
	for i, row := range retained {
		// SQL decoders may use a distinct time.Location representation for the
		// same instant. Verify all three instants, then compare every other bit.
		if !row.At.Equal(facts[i].At) || !row.CreatedAt.Equal(facts[i].CreatedAt) || !row.UpdatedAt.Equal(facts[i].UpdatedAt) {
			t.Fatalf("source fact time changed: id=%d", row.ID)
		}
		if !reflect.DeepEqual(row.At, facts[i].At) || !reflect.DeepEqual(row.CreatedAt, facts[i].CreatedAt) || !reflect.DeepEqual(row.UpdatedAt, facts[i].UpdatedAt) {
			t.Logf("database time representation normalized for fact %d; instants unchanged", row.ID)
		}
		row.At, row.CreatedAt, row.UpdatedAt = facts[i].At, facts[i].CreatedAt, facts[i].UpdatedAt
		if !reflect.DeepEqual(row, facts[i]) {
			t.Fatalf("source fact changed: id=%d", row.ID)
		}
	}
}
