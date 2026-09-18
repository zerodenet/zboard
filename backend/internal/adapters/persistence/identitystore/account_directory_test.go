package identitystore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestAccountDirectoryUsesBoundedAggregateQueries(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Now().UTC()
	user := model.User{Email: "member@example.test", AccountName: "member", Status: "active", Password: "hash"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "account-directory", Code: "account-directory"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "Account directory", Slug: "account-directory", NodeGroupID: group.ID}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "account-directory", Name: "Account directory", BillingUnit: "month", BillingValue: 1, Currency: "USD"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Subscription{
		{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "active", StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), FlowTotal: 100, FlowUsed: 1, Config: "{}"},
		{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "expired", StartAt: now.Add(-2 * time.Hour), EndAt: now.Add(-time.Hour), FlowTotal: 100, FlowUsed: 10, Config: "{}"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Order{{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: "pending", Status: "pending"}, {UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: "paid", Status: "paid"}}).Error; err != nil {
		t.Fatal(err)
	}

	queries := 0
	const callback = "test:account-directory-query-count"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(callback, func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback); _ = db.Callback().Row().Remove(callback) })
	repository := AccountDirectory{DB: db}
	detail, err := repository.Detail(context.Background(), user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if queries != 3 || detail.ActiveSubscriptionCount != 1 || detail.TotalSubscriptionCount != 2 || detail.PendingOrderCount != 1 || detail.TotalOrderCount != 2 {
		t.Fatalf("detail queries=%d result=%+v", queries, detail)
	}
	queries = 0
	page, err := repository.List(context.Background(), identity.AccountDirectoryQuery{Paged: true, Limit: 50}, now)
	if err != nil {
		t.Fatal(err)
	}
	if queries != 4 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ActiveSubscriptionCount != 1 || page.Items[0].PendingOrderCount != 1 {
		t.Fatalf("list queries=%d result=%+v", queries, page)
	}
}
