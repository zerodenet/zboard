package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAdminUserListProjectionCarriesBoundedBusinessCounts(t *testing.T) {
	item := adminUserListItem{
		userPublic: userPublic{
			ID: 4, Email: "member@example.com", Status: userStatusActive,
		},
		ActiveSubscriptionCount: 2,
		TotalSubscriptionCount:  3,
		PendingOrderCount:       1,
		TotalOrderCount:         7,
		CreatedAt:               time.Unix(10, 0).UTC(),
	}

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{
		`"active_subscription_count":2`,
		`"total_subscription_count":3`,
		`"pending_order_count":1`,
		`"total_order_count":7`,
		`"created_at":"1970-01-01T00:00:10Z"`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("user list projection is missing %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{"password", "email_verified_at", "last_login_at", "updated_at"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("user list projection contains detail field %q: %s", forbidden, text)
		}
	}
}

func TestOrderListProjectionOmitsPaymentCallbackAndDiagnostics(t *testing.T) {
	order := model.Order{
		ID: 9, UserID: 4, TradeNo: "trade-visible", Status: orderStatusPending,
		PlanName: "Starter", SKUName: "Monthly", RawCallback: `{"secret":"do-not-return"}`,
		FailureReason: "processor diagnostic", ProviderTradeNo: stringPointer("provider-private"),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}

	payload, err := json.Marshal(newAdminOrderListItem(order))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"raw_callback", "do-not-return", "failure_reason", "processor diagnostic", "provider_trade_no", "provider-private"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("order list projection contains forbidden detail %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"id":9`, `"trade_no":"trade-visible"`, `"plan_name":"Starter"`, `"sku_name":"Monthly"`} {
		if !strings.Contains(text, required) {
			t.Errorf("order list projection is missing %q: %s", required, text)
		}
	}
}

func TestOrderDetailProjectionNeverReturnsRawCallback(t *testing.T) {
	order := model.Order{
		ID: 11, UserID: 6, TradeNo: "trade-detail", Status: "failed",
		RawCallback: `{"credential":"must-stay-server-side"}`, FailureReason: "payment timed out",
		ProviderTradeNo: stringPointer("provider-reference"),
	}

	payload, err := json.Marshal(newAdminOrderDetail(order))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "raw_callback") || strings.Contains(text, "must-stay-server-side") {
		t.Fatalf("order detail contains payment callback body: %s", text)
	}
	for _, required := range []string{`"failure_reason":"payment timed out"`, `"provider_trade_no":"provider-reference"`} {
		if !strings.Contains(text, required) {
			t.Errorf("order detail is missing %q: %s", required, text)
		}
	}
}

func TestPaymentEventSummaryNeverReturnsCallbackPayload(t *testing.T) {
	event := model.PaymentEvent{
		ID: 21, OrderID: 11, Provider: "manual", ProviderEventID: "evt-21",
		EventType: "payment.confirmed", AmountMinor: 9900, SignatureValid: true,
		Payload: `{"authorization":"must-stay-server-side"}`, CreatedAt: time.Now().UTC(),
	}
	summary := adminPaymentEventSummary{
		ID: event.ID, Provider: event.Provider, ProviderEventID: event.ProviderEventID,
		EventType: event.EventType, AmountMinor: event.AmountMinor,
		SignatureValid: event.SignatureValid, ProcessedAt: event.ProcessedAt,
		CreatedAt: event.CreatedAt,
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"payload", "authorization", "must-stay-server-side", "order_id"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("payment event summary contains forbidden value %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"id":21`, `"provider":"manual"`, `"provider_event_id":"evt-21"`, `"event_type":"payment.confirmed"`, `"signature_valid":true`} {
		if !strings.Contains(text, required) {
			t.Errorf("payment event summary is missing %q: %s", required, text)
		}
	}
}

func TestPlanSummaryProjectionOmitsDescriptionPolicyAndSKUs(t *testing.T) {
	plan := model.Plan{
		ID: 17, Name: "Standard", Slug: "standard", Summary: "Visible summary",
		Description:  "Long detail that belongs in the detail response",
		TrafficBytes: 1024, SpeedLimitMbps: 50, DeviceLimit: 3,
		NodeGroupID: 8, NodeGroup: &model.NodeGroup{ID: 8, Name: "Primary", Code: "primary", IsEnabled: true},
		SKUs: []model.PlanSKU{{ID: 91, Code: "private-list-sku", IsActive: true}},
	}
	payload, err := json.Marshal(newPlanSummaryItem(plan, planSKUCountRow{PlanID: plan.ID, SKUCount: 4, ActiveSKUCount: 3}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"description", "Long detail", "traffic_bytes", "speed_limit_mbps", "device_limit", `"skus"`, "private-list-sku"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("plan summary contains detail field %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"id":17`, `"name":"Standard"`, `"sku_count":4`, `"active_sku_count":3`, `"node_group":{"id":8,"name":"Primary"`} {
		if !strings.Contains(text, required) {
			t.Errorf("plan summary is missing %q: %s", required, text)
		}
	}
}

func TestPlanDetailProjectionUsesCountsWithoutEmbeddingSKUData(t *testing.T) {
	plan := model.Plan{
		ID: 18, Name: "Advanced", Slug: "advanced", Description: "Operator detail",
		TrafficBytes: 2048, DeviceLimit: 5,
		SKUs: []model.PlanSKU{
			{ID: 101, PlanID: 18, Code: "advanced-month", Name: "Monthly", IsActive: true},
			{ID: 102, PlanID: 18, Code: "advanced-year", Name: "Yearly", IsActive: false},
		},
	}
	payload, err := json.Marshal(newPlanDetailItem(plan, planSKUCountRow{PlanID: plan.ID, SKUCount: 2, ActiveSKUCount: 1}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{`"description":"Operator detail"`, `"sku_count":2`, `"active_sku_count":1`} {
		if !strings.Contains(text, required) {
			t.Errorf("plan detail is missing %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{`"skus"`, `"advanced-month"`, `"advanced-year"`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("plan detail embeds unbounded SKU data %q: %s", forbidden, text)
		}
	}
}

func TestPlanCatalogProjectionContainsOnePrimarySKUWithoutCollection(t *testing.T) {
	plan := model.Plan{
		ID: 19, Name: "Catalog", Slug: "catalog", Summary: "Public summary",
		SKUs: []model.PlanSKU{
			{ID: 111, PlanID: 19, Code: "must-not-leak", Name: "Unbounded"},
		},
	}
	primary := model.PlanSKU{
		ID: 112, PlanID: 19, Code: "catalog-month", Name: "Monthly",
		SKUType: "new", PriceCents: 1200, Currency: "CNY", IsActive: true,
	}
	payload, err := json.Marshal(newPlanCatalogItem(
		plan,
		planSKUCountRow{PlanID: plan.ID, SKUCount: 3, ActiveSKUCount: 3},
		&primary,
	))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{`"id":19`, `"sku_count":3`, `"active_sku_count":3`, `"primary_sku":`, `"code":"catalog-month"`} {
		if !strings.Contains(text, required) {
			t.Errorf("plan catalog item is missing %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{`"skus"`, `"must-not-leak"`, `"description"`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("plan catalog item embeds unbounded or detail data %q: %s", forbidden, text)
		}
	}
}

func stringPointer(value string) *string {
	return &value
}
