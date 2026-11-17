package application

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestSQLiteProjectionContinuationDrainsBeyondOneBatch(t *testing.T) {
	assertProjectionContinuation(t, datastore.DriverSQLite)
}
func TestMySQLProjectionContinuationDrainsBeyondOneBatch(t *testing.T) {
	assertProjectionContinuation(t, datastore.DriverMySQL)
}

func assertProjectionContinuation(t *testing.T, driver string) {
	db := schemaFixture(t, driver)
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	app := New(db, "continuation-fixture")
	t.Cleanup(app.Close)
	if err := app.SystemConfigDefaults.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	user := model.User{AccountName: "continuation", Email: "continuation@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "continuation", Code: "continuation", IsEnabled: true}
	node := model.Node{Name: "continuation", Config: "{}"}
	for _, row := range []any{&user, &group, &node} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "continuation", Slug: "continuation", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "continuation", Name: "continuation", BillingUnit: "month", BillingValue: 1, PriceCents: 100, Currency: "CNY"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "active", StartAt: now, EndAt: now.Add(24 * time.Hour), FlowTotal: 10000, FlowUsed: 2005, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, RuntimeKey: uuid.NewString(), Name: "continuation", Protocol: "vless", Port: 443, IsActive: true, MultiplierMilli: 1500, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]model.TrafficRecord, 401)
	for i := range rows {
		rows[i] = model.TrafficRecord{UserID: user.ID, SubscriptionID: sub.ID, NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: fmt.Sprintf("continuation-%d", i), Nonce: fmt.Sprintf("continuation-nonce-%d", i), RawBytes: 3, DownloadBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: now, Meta: "settled synthetic projection source"}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
		return meteringstore.RecordUsageProjections(tx, rows)
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []byte {
		t.Helper()
		var facts []model.TrafficRecord
		if err := db.Order("id").Find(&facts).Error; err != nil {
			t.Fatal(err)
		}
		type fact struct {
			Record model.TrafficRecord
			Nonce  string
			Epoch  uint64
		}
		frozen := make([]fact, len(facts))
		for i, row := range facts {
			frozen[i] = fact{row, row.Nonce, row.EntitlementEpoch}
		}
		data, err := json.Marshal(frozen)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := snapshot()
	var originalSub model.Subscription
	if err := db.First(&originalSub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	job := app.UsageProjectionJob()
	// A one-hour idle interval makes an ordinary periodic second tick incapable
	// of passing this test. Only the shared durable continuation can drain 401.
	job.Interval = time.Hour
	if err := app.RegisterPeriodicJob(job); err != nil {
		t.Fatal(err)
	}
	app.StartWork()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var pending int64
		if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("full batches waited for the idle interval", pending)
		case <-tick.C:
		}
	}
	drain, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	if err := app.Jobs.Drain(drain); err != nil {
		t.Fatal(err)
	}
	var yielded, succeeded int64
	if err := db.Table("job_runs").Where("handler = ? AND state = ?", job.ID, jobs.Yielded).Count(&yielded).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("job_runs").Where("handler = ? AND state = ?", job.ID, jobs.Succeeded).Count(&succeeded).Error; err != nil {
		t.Fatal(err)
	}
	var schedule struct {
		Runs           uint64
		CompactHistory bool
	}
	if err := db.Table("job_schedules").Where("id = ?", job.ID).Take(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	if yielded != 0 || succeeded != 1 || schedule.Runs != 3 || !schedule.CompactHistory {
		t.Fatal("bounded continuation did not preserve cumulative executions in compact history", yielded, succeeded, schedule)
	}
	var totals struct{ Raw, Used, Records int64 }
	if err := db.Table("traffic_usage_hourly").Select("COALESCE(SUM(raw_bytes),0) AS raw,COALESCE(SUM(used_bytes),0) AS used,COALESCE(SUM(record_count),0) AS records").Scan(&totals).Error; err != nil || totals.Raw != 1203 || totals.Used != 2005 || totals.Records != 401 {
		t.Fatal("projection duplicated or lost settled deltas", totals, err)
	}
	var currentSub model.Subscription
	if err := db.First(&currentSub, sub.ID).Error; err != nil || !reflect.DeepEqual(originalSub, currentSub) {
		t.Fatal("projection continuation changed entitlement or quota", err)
	}
	if string(snapshot()) != string(before) {
		t.Fatal("projection continuation changed immutable accounting evidence")
	}
}
