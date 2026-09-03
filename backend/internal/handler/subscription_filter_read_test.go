package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAccountSubscriptionSearchFiltersBeforeCountAndSupportsHistoricalSelection(t *testing.T) {
	f := newCatalogFixture(t)
	plan := f.plan(t, 1)
	if err := f.h.db.Model(&plan).Update("name", "Historical report plan").Error; err != nil {
		t.Fatal(err)
	}
	for id := uint(1); id <= 106; id++ {
		owner := uint(1)
		if id == 106 {
			owner = 2
		}
		sub := model.Subscription{ID: id, UserID: owner, PlanID: plan.ID, NodeGroupID: f.group.ID, Status: "expired"}
		if err := f.h.db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
	}
	var page struct {
		Items []adminSubscriptionListItem
		Total int64
	}
	path := "/api/v1/subscriptions?paged=true&limit=25&q=" + url.QueryEscape("Historical report")
	if status := f.get(t, path+"&offset=100", f.h.SubscriptionsHandler, &page); status != 200 {
		t.Fatal(status)
	}
	if page.Total != 105 || len(page.Items) != 5 || page.Items[4].ID != 1 {
		t.Fatalf("historical page=%+v", page)
	}
	for _, id := range []uint{1, 106} {
		status := f.get(t, fmt.Sprintf("/api/v1/subscriptions?paged=true&q=%d&user_id=2&limit=1", id), f.h.SubscriptionsHandler, &page)
		if status != 200 {
			t.Fatal(status)
		}
		want := int64(1)
		if id == 106 {
			want = 0
		}
		if page.Total != want || int64(len(page.Items)) != want {
			t.Fatalf("numeric owner scope id=%d page=%+v", id, page)
		}
	}
	if status := f.get(t, "/api/v1/subscriptions?paged=true&q="+strings.Repeat("x", 129), f.h.SubscriptionsHandler, nil); status != http.StatusBadRequest {
		t.Fatalf("long query status=%d", status)
	}
}

func TestTrafficTrendSubscriptionDirectoryRequiresExplicitLegacyOptIn(t *testing.T) {
	f := newTrafficReadFixture(t)
	sub := model.Subscription{ID: 1, UserID: 1}
	if err := f.h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	for name, handler := range map[string]http.HandlerFunc{"utc": f.h.TrafficTrendsHandler, "calendar": f.h.TrafficTrendsSystemCalendarHandler} {
		t.Run(name, func(t *testing.T) {
			path := "/api/v1/traffic/trends?from=2026-09-01&to=2026-09-02"
			f.capture()
			var trend trafficTrendResponse
			f.get(t, path, false, handler, &trend)
			if len(trend.Subscriptions) != 0 {
				t.Fatal("default trend returned an unbounded directory")
			}
			for _, query := range f.log.queries {
				if strings.Contains(query, "FROM `subscriptions`") {
					t.Fatalf("default chart read subscription directory: %s", query)
				}
			}
			f.get(t, path+"&include_subscriptions=true", false, handler, &trend)
			if len(trend.Subscriptions) != 1 {
				t.Fatal("explicit legacy read lost its contract")
			}
		})
	}
}
