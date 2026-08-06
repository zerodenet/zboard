package handler

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestParseAccountSubscriptionAccessID(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		rotate bool
		want   uint
		valid  bool
	}{
		{name: "read", path: "/api/v1/account/subscriptions/42/access", want: 42, valid: true},
		{name: "revoke", path: "/api/v1/account/subscriptions/7/access/", want: 7, valid: true},
		{name: "rotate", path: "/api/v1/account/subscriptions/99/access/rotate", rotate: true, want: 99, valid: true},
		{name: "missing id", path: "/api/v1/account/subscriptions/access", valid: false},
		{name: "wrong action", path: "/api/v1/account/subscriptions/1/access/rotate", valid: false},
		{name: "extra segment", path: "/api/v1/account/subscriptions/1/access/extra", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseAccountSubscriptionAccessID(test.path, test.rotate)
			if test.valid && err != nil {
				t.Fatalf("parse access path: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatalf("expected invalid path, got %d", got)
			}
			if test.valid && got != test.want {
				t.Fatalf("subscription id = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSubscriptionAccessAvailableIsExactSubscriptionState(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	available := model.Subscription{Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 99}
	if !subscriptionAccessAvailable(available, now) {
		t.Fatal("expected active subscription with quota to be available")
	}
	for name, subscription := range map[string]model.Subscription{
		"expired":   {Status: subStatusActive, EndAt: now, FlowTotal: 100, FlowUsed: 1},
		"exhausted": {Status: subStatusActive, EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 100},
		"canceled":  {Status: subStatusCanceled, EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if subscriptionAccessAvailable(subscription, now) {
				t.Fatal("unusable subscription reported available")
			}
		})
	}
}

func TestProjectionFiltersCannotExpandSingleSubscriptionScope(t *testing.T) {
	subscription := model.Subscription{ID: 11}
	sources := map[uint]subscriptionProjectionSource{
		11: {PlanSlug: "limited", SKUCode: "limited-monthly", NodeGroupCode: "limited"},
	}
	filter := subscriptionProjectionFilter{Plans: map[string]struct{}{"unlimited": {}}}
	result := filterSubscriptionsForProjection([]model.Subscription{subscription}, sources, filter)
	if len(result) != 0 {
		t.Fatalf("filter expanded token scope: %#v", result)
	}
}

func TestRouterRemovesAccountAggregateAccessRoutes(t *testing.T) {
	source, err := os.ReadFile("../server/router.go")
	if err != nil {
		t.Fatalf("read router source: %v", err)
	}
	text := string(source)
	for _, expected := range []string{
		`/api/v1/account/subscriptions/:id/access`,
		`/api/v1/account/subscriptions/:id/access/rotate`,
		`h.ScopedClientSubscriptionHandler`,
		`h.ReconcileSubscriptionAccessTokens()`,
		`datastore.ReconcileSubscriptionAccessSchema(db)`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("router missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`newRoute(http.MethodGet, "/api/v1/subscription/access"`,
		`newRoute(http.MethodPost, "/api/v1/subscription/access/rotate"`,
		`newRoute(http.MethodDelete, "/api/v1/subscription/access"`,
		`h.FilteredClientSubscriptionHandler`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("router retains aggregate access path %q", forbidden)
		}
	}
}
