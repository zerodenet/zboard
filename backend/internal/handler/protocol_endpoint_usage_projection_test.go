package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func drainUsageProjections(t testing.TB, db *gorm.DB) {
	t.Helper()
	worker := meteringstore.UsageProjectionWorker{DB: db}
	for {
		count, err := worker.Drain(context.Background(), meteringstore.UsageProjectionBatchLimit)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
	}
}

func TestAccountingDailyProjectionFailureDoesNotBlockSettlementOrReplay(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	ctx := context.Background()
	if err := h.db.Exec(`CREATE TRIGGER reject_daily_chart BEFORE INSERT ON protocol_endpoint_usage_daily BEGIN SELECT RAISE(ABORT, 'chart unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 4)
	for i := 0; i < 2; i++ {
		if err := h.projectZeroNodeEvents(ctx, events); err != nil {
			t.Fatalf("daily failure blocked authoritative settlement/replay: %v", err)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	if count, err := worker.Drain(ctx, 100); err == nil || count != 0 {
		t.Fatalf("failed chart checkpoint acknowledged: %d %v", count, err)
	}
	for _, check := range []any{&model.TrafficRecord{}, &model.MeteringUsageProjectionPending{}} {
		var count int64
		var ledgerRows int64
		if err := h.db.Model(&model.TrafficRecord{}).Count(&ledgerRows).Error; err != nil {
			t.Fatal(err)
		}
		if err := h.db.Model(check).Count(&count).Error; err != nil || count != ledgerRows || count < 1 || count > 4 {
			t.Fatalf("settlement or intent lost/duplicated in %T: %d %v", check, count, err)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	if err := h.db.Exec("DROP TRIGGER reject_daily_chart").Error; err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	var usage model.ProtocolEndpointUsageDaily
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 120 || usage.RecordCount != 4 {
		t.Fatal(usage, err)
	}
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	usage = model.ProtocolEndpointUsageDaily{}
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 120 || usage.RecordCount != 4 {
		t.Fatalf("replay double-projected settled value: %+v %v", usage, err)
	}
}

func TestAccountingUsageProjectionIntentFailureRollsBackLedgerAndQuota(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Exec(`CREATE TRIGGER reject_projection_intent BEFORE INSERT ON metering_usage_projection_pending BEGIN SELECT RAISE(ABORT, 'durable intent unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 4)
	if err := h.projectZeroNodeEvents(context.Background(), events); err == nil {
		t.Fatal("settlement acknowledged without durable projection recovery intent")
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	for _, table := range []string{"traffic_records", "flow_usages", "zero_event_node_cursors", "metering_usage_projection_pending"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial settlement in %s: %d %v", table, count, err)
		}
	}
	if err := h.db.Exec("DROP TRIGGER reject_projection_intent").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	drainUsageProjections(t, h.db)
}

func TestProtocolUsageProjectionPendingIsUnknownOnWire(t *testing.T) {
	for _, pending := range []bool{false, true} {
		payload, err := json.Marshal(protocolUsageModel(networkcap.ProtocolUsageRecord{UsedBytesToday: 11, UsedBytesTotal: 21, ActiveFlows: 3, TrafficProjectionPending: pending}))
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatal(err)
		}
		if wire["active_flows"] != float64(3) {
			t.Fatalf("projection lag changed live observation: %s", payload)
		}
		if pending {
			if wire["used_bytes_today"] != nil || wire["used_bytes_total"] != nil || wire["traffic_projection_pending"] != true {
				t.Fatalf("partial chart masquerades as complete: %s", payload)
			}
		} else if wire["used_bytes_today"] != float64(11) || wire["used_bytes_total"] != float64(21) {
			t.Fatalf("settled chart values changed: %s", payload)
		}
	}
}
