package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Capture executed statements, not just a dry-run approximation of the handler.
type trafficQueryLog struct {
	logger.Interface
	queries      []string
	contexts     []context.Context
	authContexts []context.Context
	afterAuth    func()
}

func (l *trafficQueryLog) Trace(ctx context.Context, _ time.Time, sql func() (string, int64), _ error) {
	query, _ := sql()
	// Authentication is a separate read; query-count assertions below measure
	// the reconciliation projection only.
	if strings.Contains(query, "FROM `users`") {
		l.authContexts = append(l.authContexts, ctx)
		if l.afterAuth != nil {
			l.afterAuth()
		}
		return
	}
	l.queries = append(l.queries, query)
	l.contexts = append(l.contexts, ctx)
}

type trafficReadFixture struct {
	h     *handlers
	token string
	admin string
	log   *trafficQueryLog
}

func newTrafficReadFixture(t *testing.T) trafficReadFixture {
	t.Helper()
	h, token := newAnnouncementTestHandlers(t)
	user := model.User{ID: 99, Email: "traffic-admin@example.test", Password: "unused", Status: userStatusActive, IsAdmin: true}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	admin, _, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	return trafficReadFixture{h: h, token: token, admin: admin}
}

func (f *trafficReadFixture) capture() {
	f.log = &trafficQueryLog{Interface: logger.Discard}
	f.h.db = f.h.db.Session(&gorm.Session{Logger: f.log})
}

func (f trafficReadFixture) get(t *testing.T, path string, admin bool, handler http.HandlerFunc, data any) {
	t.Helper()
	token := f.token
	if admin {
		token = f.admin
	}
	r := httptest.NewRecorder()
	handler(r, announcementRequest(http.MethodGet, path, token, ""))
	if r.Code != http.StatusOK {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	var envelope struct{ Data json.RawMessage }
	if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, data); err != nil {
		t.Fatal(err)
	}
}

func (f trafficReadFixture) seedReconciliation(t *testing.T) {
	t.Helper()
	// Subscription 3 has a wrongly attributed raw row. Reconciliation must
	// still count it by subscription, rather than hide it via traffic.user_id.
	for _, sub := range []model.Subscription{
		{ID: 1, UserID: 1, FlowUsed: 100},
		{ID: 2, UserID: 1, FlowUsed: 80},
		{ID: 3, UserID: 1, FlowUsed: 30},
		{ID: 4, UserID: 1, FlowUsed: 20},
		{ID: 5, UserID: 2, FlowUsed: 9999},
	} {
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, row := range []struct {
		sub, user uint
		used      int64
	}{{1, 1, 100}, {2, 1, 50}, {3, 2, 40}, {5, 2, 10000}, {999, 1, 777}} {
		record := model.TrafficRecord{SubscriptionID: row.sub, UserID: row.user, UsedBytes: row.used, NodeID: 1, ReportID: fmt.Sprint(i), Nonce: fmt.Sprint(i), At: time.Now().UTC()}
		if err := f.h.db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
}

type reconciliationTestPage struct {
	Items      []trafficReconciliationItem
	Total      int64
	Aggregates trafficReconciliationAggregates
}

func TestTrafficReconciliationScopesTotalsAndReusesIssuePageAggregation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	for offset, wantID := range []uint{4, 3, 2} {
		f.capture()
		var page reconciliationTestPage
		f.get(t, fmt.Sprintf("/api/v1/admin/traffic/reconciliation?paged=true&user_id=1&issues_only=true&limit=1&offset=%d", offset), true, f.h.TrafficReconciliationHandler, &page)
		if page.Total != 3 || len(page.Items) != 1 || page.Items[0].SubscriptionID != wantID {
			t.Fatalf("page = %+v", page)
		}
		if page.Aggregates != (trafficReconciliationAggregates{SubscriptionCount: 4, MatchedCount: 1, MissingRecordsCount: 2, OverRecordedCount: 1, FlowUsed: 230, RecordedBytes: 190, MissingBytes: 50, OverRecordedBytes: 10}) {
			t.Fatalf("aggregates = %+v", page.Aggregates)
		}
		if wantID == 3 && (page.Items[0].RecordedBytes != 40 || page.Items[0].Difference != -10) {
			t.Fatalf("wrong-owner raw record was hidden: %+v", page.Items[0])
		}
		if len(f.log.queries) != 2 {
			t.Fatalf("want summary + issue page, got %d queries", len(f.log.queries))
		}
		for _, query := range f.log.queries {
			if !strings.Contains(query, "subscription_id IN (SELECT subscriptions.id") || !strings.Contains(query, "subscriptions.user_id = 1") {
				t.Fatalf("unscoped aggregate: %s", query)
			}
		}
		if offset == 0 {
			assertTrafficScopeIndexPlan(t, f, f.log.queries[0])
		}
	}
}

func TestTrafficReconciliationAllPageCountsAndBoundedPageTotals(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	f.capture()
	var page reconciliationTestPage
	f.get(t, "/api/v1/admin/traffic/reconciliation?paged=true&user_id=1&limit=2&offset=2", true, f.h.TrafficReconciliationHandler, &page)
	if page.Total != 4 || len(page.Items) != 2 || page.Items[0].SubscriptionID != 2 || page.Items[1].RecordedBytes != 100 {
		t.Fatalf("page = %+v", page)
	}
	if len(f.log.queries) != 3 || !strings.Contains(f.log.queries[2], "subscription_id IN (2,1)") {
		t.Fatalf("want summary, bounded page and its totals: %v", f.log.queries)
	}
	var exact reconciliationTestPage
	f.get(t, "/api/v1/admin/traffic/reconciliation?paged=true&subscription_id=3", true, f.h.TrafficReconciliationHandler, &exact)
	if exact.Total != 1 || exact.Aggregates.RecordedBytes != 40 || len(exact.Items) != 1 {
		t.Fatalf("exact page = %+v", exact)
	}
}

func TestTrafficReconciliationSelfScopeCannotReadAnotherSubscription(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	var items []trafficReconciliationItem
	f.get(t, "/api/v1/traffic/reconciliation?user_id=2&subscription_id=5&issues_only=true", false, f.h.TrafficReconciliationHandler, &items)
	if len(items) != 0 {
		t.Fatalf("foreign subscription leaked: %+v", items)
	}
	f.get(t, "/api/v1/traffic/reconciliation?subscription_id=3", false, f.h.TrafficReconciliationHandler, &items)
	if len(items) != 1 || items[0].RecordedBytes != 40 {
		t.Fatalf("own subscription mismatch = %+v", items)
	}
}

func TestTrafficReconciliationPropagatesCancellationToSQL(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.seedReconciliation(t)
	f.capture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.log.afterAuth = cancel
	r := httptest.NewRecorder()
	f.h.TrafficReconciliationHandler(r, announcementRequest(http.MethodGet, "/api/v1/admin/traffic/reconciliation?paged=true", f.admin, "").WithContext(ctx))
	if r.Code != http.StatusInternalServerError || len(f.log.queries) != 1 || f.log.contexts[0] != ctx {
		t.Fatalf("cancellation status=%d queries=%d", r.Code, len(f.log.queries))
	}
}

// SQLite execution-plan evidence supplements the handler semantics tests.
// It does not establish the production MySQL optimizer or measured latency.
func assertTrafficScopeIndexPlan(t *testing.T, f trafficReadFixture, query string) {
	t.Helper()
	var plan []struct{ Detail string }
	if err := f.h.db.Session(&gorm.Session{Logger: logger.Discard}).Raw("EXPLAIN QUERY PLAN " + query).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	usesIndex := false
	for _, step := range plan {
		t.Log(step.Detail)
		if strings.Contains(step.Detail, "SEARCH traffic_records USING INDEX") {
			usesIndex = true
		}
	}
	if !usesIndex {
		t.Fatal("traffic projection lost its scoped index search")
	}
}
