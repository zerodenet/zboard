package datastore_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteFixedQuotaMigrationRepairsAlreadyUpgradedInstances(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "fixed.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	checkFixedQuotaMigration(t, db)
}
func TestMySQLFixedQuotaMigrationRepairsAlreadyUpgradedInstances(t *testing.T) {
	db, _ := hourlyMySQLFixture(t)
	checkFixedQuotaMigration(t, db)
}
func checkFixedQuotaMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	user := model.User{Email: "fixed@example.test", Password: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	group := model.NodeGroup{Name: "Fixed", Code: "fixed", IsEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{Name: "Fixed", Slug: "fixed", NodeGroupID: group.ID}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "fixed", Name: "One year validity", BillingUnit: "year", BillingValue: 1, Currency: "CNY"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		lifecycle string
		reset     int16
		want      bool
	}{{"fixed", 0, true}, {"fixed", 5, true}, {"fixed", 2, false}, {"renewable", 0, false}, {"renewable", 2, false}}
	ids := make([]uint, len(cases))
	for i, tc := range cases {
		sub := model.Subscription{Lifecycle: tc.lifecycle, ResetPolicy: tc.reset, UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "active", StartAt: time.Now().UTC(), EndAt: time.Now().UTC().Add(365 * 24 * time.Hour), FlowTotal: 100, FlowUsed: 100, Config: "{}"}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		ids[i] = sub.ID
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = '0026_fixed_quota_lifecycle.up.sql'").Error; err != nil {
		t.Fatal(err)
	}
	for retry := 0; retry < 2; retry++ {
		if err := datastore.RunMigrations(db); err != nil {
			t.Fatal(err)
		}
		for i, tc := range cases {
			var sub model.Subscription
			if err := db.First(&sub, ids[i]).Error; err != nil {
				t.Fatal(err)
			}
			if sub.EndsOnQuotaExhaustion != tc.want || sub.Lifecycle != tc.lifecycle || sub.FlowUsed != 100 || sub.EndedAt != nil {
				t.Fatalf("case %d retry %d: %+v", i, retry, sub)
			}
		}
	}
}
