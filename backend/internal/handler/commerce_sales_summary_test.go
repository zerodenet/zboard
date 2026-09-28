package handler

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"net/http"
	"testing"
	"time"
)

func TestAdminPlanSalesSummaryUsesActualAvailableSKUsAndKeepsCurrenciesSeparate(t *testing.T) {
	f := newCatalogFixture(t)
	f.plan(t, 1)
	if err := f.h.db.Model(&model.Plan{}).Where("id=1").Update("is_renewable", false).Error; err != nil {
		t.Fatal(err)
	}
	f.plan(t, 2)
	f.sku(t, 1, 1500, "purchase")
	f.sku(t, 1, 4300, "purchase")
	f.sku(t, 1, 500, "reset")
	f.sku(t, 1, 10, "renew") // Product renewal is disabled.
	usd := f.sku(t, 1, 200, "purchase")
	if err := f.h.db.Model(&usd).Update("currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	inactive := f.sku(t, 1, 1, "reset")
	if err := f.h.db.Model(&inactive).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	archived := f.sku(t, 1, 2, "addon")
	if err := f.h.db.Model(&archived).Update("archived_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	f.sku(t, 2, 100, "purchase")
	if err := f.h.db.Model(&model.Plan{}).Where("id=2").Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id=1").Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := f.h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	var page struct {
		Items []commerce.PlanSummary
		Total int64
	}
	status := f.get(t, "/api/v1/plans?paged=true&include_inactive=true", f.h.PlanListCommerceHandler, &page)
	if status != http.StatusOK || page.Total != 2 {
		t.Fatalf("status %d page %+v", status, page)
	}
	for _, item := range page.Items {
		if item.ID == 2 {
			if len(item.SalesOptions) != 0 {
				t.Fatalf("draft is purchasable %+v", item)
			}
			continue
		}
		if len(item.SalesOptions) != 3 || item.SKUCount != 6 || item.ActiveSKUCount != 5 {
			t.Fatalf("summary %+v", item)
		}
		for _, sale := range item.SalesOptions {
			switch {
			case sale.Operation == "purchase" && sale.Currency == "CNY":
				if sale.MinPriceCents != 1500 || sale.MaxPriceCents != 4300 {
					t.Fatalf("price range %+v", sale)
				}
			case sale.Operation == "purchase" && sale.Currency == "USD":
				if sale.MinPriceCents != 200 {
					t.Fatalf("USD price %+v", sale)
				}
			case sale.Operation == "reset":
				if sale.MinPriceCents != 500 {
					t.Fatalf("inactive leaked %+v", sale)
				}
			default:
				t.Fatalf("unavailable sale %+v", sale)
			}
		}
	}
}
