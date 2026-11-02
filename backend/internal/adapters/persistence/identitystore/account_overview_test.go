package identitystore

import (
	"context"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestAccountCleanupOverviewMatchesWorker(t *testing.T) {
	db, _ := administrationFixture(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&model.SystemConfig{ConfigKey: identity.CleanupDaysKey, Value: "14", ValueType: "int"}).Error; err != nil {
		t.Fatal(err)
	}
	create := func(name string, age int) model.User {
		return deletionUser(t, db, name+"@example.test", now.AddDate(0, 0, -age))
	}
	fresh, expired, active, ended, shared, removed, due, funded, pending, admin := create("fresh", 10), create("expired", 90), create("active", 90), create("ended", 90), create("shared", 90), create("removed", 90), create("due", 30), create("funded", 90), create("pending", 90), create("admin", 90)
	if err := db.Model(&admin).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	early := now.AddDate(0, 0, -2)
	group := model.NodeGroup{Name: "cleanup-overview", Code: "cleanup-overview"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "Cleanup overview", Slug: "cleanup-overview", NodeGroupID: group.ID}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "cleanup-overview", Name: "Cleanup overview", BillingUnit: "month", BillingValue: 1, Currency: "USD"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	subs := []model.Subscription{{UserID: expired.ID, EndAt: now.AddDate(0, 0, -3), Status: "expired"}, {UserID: active.ID, EndAt: now.AddDate(0, 0, 5), Status: "active"}, {UserID: ended.ID, EndAt: now.AddDate(0, 0, 10), EndedAt: &early, Status: "canceled"}, {UserID: admin.ID, EndAt: now.AddDate(0, 0, 4), Status: "active"}}
	for i := range subs {
		subs[i].PlanID, subs[i].PlanSKUID, subs[i].NodeGroupID = plan.ID, sku.ID, group.ID
		subs[i].StartAt = now.AddDate(0, 0, -60)
		subs[i].Config = "{}"
		if err := db.Create(&subs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	removedAt := now.AddDate(0, 0, -1)
	for _, m := range []model.SubscriptionMember{{UserID: shared.ID, SubscriptionID: subs[3].ID, Status: "active"}, {UserID: removed.ID, SubscriptionID: subs[3].ID, Status: "removed", RemovedAt: &removedAt}} {
		m.JoinedAt = now.AddDate(0, 0, -30)
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.WalletAccount{UserID: funded.ID, Currency: "USD", BalanceMinor: 9007199254740993, ReservedMinor: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Order{UserID: pending.ID, PlanID: plan.ID, PlanSKUID: sku.ID, TradeNo: "cleanup-pending", Status: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	perpetual := create("perpetual", 90)
	if err := db.Create(&model.Subscription{UserID: perpetual.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, StartAt: now.AddDate(0, 0, -60), EndAt: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), Status: "active", Config: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	repo := AccountDirectory{DB: db}
	queries := 0
	const callback = "test:overview-query-budget"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(callback, func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	page, err := repo.List(context.Background(), identity.AccountDirectoryQuery{Paged: true, Limit: 50}, now)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Callback().Query().Remove(callback)
	_ = db.Callback().Row().Remove(callback)
	if queries != 7 {
		t.Fatalf("page projection grew per owner: %d queries", queries)
	}
	states := map[uint]struct {
		state string
		days  int
	}{fresh.ID: {"scheduled", 4}, expired.ID: {"scheduled", 11}, active.ID: {"subscribed", 19}, ended.ID: {"scheduled", 12}, shared.ID: {"subscribed", 18}, removed.ID: {"scheduled", 13}, due.ID: {"eligible", -16}, funded.ID: {"protected", 0}, pending.ID: {"protected", 0}, admin.ID: {"protected", 0}}
	for _, item := range page.Items {
		if item.ID == perpetual.ID {
			if item.Cleanup.State != "subscribed" || item.Cleanup.EligibleAt != nil {
				t.Fatalf("invented perpetual deadline: %+v", item.Cleanup)
			}
			continue
		}
		expect := states[item.ID]
		if !item.Cleanup.Enabled || item.Cleanup.InactiveDays != 14 || item.Cleanup.State != expect.state {
			t.Fatalf("owner=%d cleanup=%+v expected=%+v", item.ID, item.Cleanup, expect)
		}
		if expect.state == "protected" {
			if item.Cleanup.EligibleAt != nil || item.Cleanup.Reason == "" {
				t.Fatalf("protected estimate: %+v", item.Cleanup)
			}
		} else if item.Cleanup.EligibleAt == nil || !item.Cleanup.EligibleAt.Equal(now.AddDate(0, 0, expect.days)) {
			t.Fatalf("owner=%d wrong date: %+v", item.ID, item.Cleanup)
		}
		if item.ID == funded.ID && (len(item.WalletBalances) != 1 || item.WalletBalances[0].BalanceMinor != "9007199254740993") {
			t.Fatalf("balance precision: %+v", item.WalletBalances)
		}
	}
	var candidates []uint
	if err := cleanupCandidates(db, now, 14).Pluck("users.id", &candidates).Error; err != nil || len(candidates) != 1 || candidates[0] != due.ID {
		t.Fatalf("worker/display disagreement: %v %v", candidates, err)
	}
	boundary, err := repo.Detail(context.Background(), fresh.ID, now.AddDate(0, 0, 4))
	if err != nil || boundary.Cleanup.State != "eligible" {
		t.Fatalf("eligibility boundary: %+v %v", boundary.Cleanup, err)
	}
	var removedCandidates []uint
	if err := cleanupCandidates(db, now.AddDate(0, 0, 16), 14).Where("users.id IN ?", []uint{shared.ID, removed.ID}).Pluck("users.id", &removedCandidates).Error; err != nil || len(removedCandidates) != 1 || removedCandidates[0] != removed.ID {
		t.Fatalf("removed shared access did not converge: %v %v", removedCandidates, err)
	}
	if err := db.Model(&model.SystemConfig{}).Where("config_key=?", identity.CleanupDaysKey).Update("value", "0").Error; err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Detail(context.Background(), funded.ID, now)
	if err != nil || detail.Cleanup.Enabled || detail.Cleanup.State != "disabled" || len(detail.WalletBalances) != 1 {
		t.Fatalf("disabled policy lost wallet: %+v %v", detail, err)
	}
}
