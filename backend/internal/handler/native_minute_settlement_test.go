package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func TestSQLiteNativeMinuteSettlementPreservesPerReportChargesAndOldReplay(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	nativeMinuteCharges(t, h, c)
}
func TestMySQLNativeMinuteSettlementPreservesPerReportChargesAndOldReplay(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	nativeMinuteCharges(t, h, c)
}
func nativeMinuteCharges(t *testing.T, h *handlers, c model.ProtocolCredential) {
	t.Helper()
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().AddDate(-2, 0, 0).Truncate(time.Minute)
	var first metering.CompletedFlow
	for i := 0; i < 100; i++ {
		in := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "minute-core", FlowID: fmt.Sprint("flow-", i), EventID: fmt.Sprint("minute-", i), EventType: "flow.completed", Sequence: uint64(i + 1), BytesUp: 1, OccurredAt: at.Add(time.Duration(i%60) * time.Second)}
		if i == 0 {
			first = in
		}
		if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), in); err != nil {
			t.Fatal(i, err)
		}
	}
	var totals struct {
		Raw, Used, Reports int64
		Rows               int64
	}
	if err := h.db.Model(&model.TrafficRecord{}).Select("SUM(raw_bytes) AS raw,SUM(used_bytes) AS used,SUM(record_count) AS reports,COUNT(*) AS rows").Scan(&totals).Error; err != nil || totals.Raw != 100 || totals.Used != 200 || totals.Reports != 100 || totals.Rows > 3 {
		t.Fatal("charges were re-rounded or per-event history survived", totals, err)
	}
	var sub model.Subscription
	if err := h.db.First(&sub, c.SubscriptionID).Error; err != nil || sub.FlowUsed != 200 {
		t.Fatal(sub, err)
	}
	var receipts int64
	if err := h.db.Model(&model.MeteringReportReceipt{}).Count(&receipts).Error; err != nil || receipts != 100 {
		t.Fatal("indefinite report identities lost", receipts, err)
	}
	// The first original event is years old and its bucket now includes other
	// connections. Its independent receipt must still prevent another charge.
	if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), first); err != nil {
		t.Fatal("old replay rejected", err)
	}
	if err := h.db.First(&sub, c.SubscriptionID).Error; err != nil || sub.FlowUsed != 200 {
		t.Fatal("old replay charged again", sub, err)
	}
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	for {
		n, err := worker.Drain(t.Context(), 200)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var projected struct{ Used, Reports int64 }
	if err := h.db.Table("traffic_usage_hourly").Select("SUM(used_bytes) AS used,SUM(record_count) AS reports").Scan(&projected).Error; err != nil || projected.Used != 200 || projected.Reports != 100 {
		t.Fatal(projected, err)
	}
	t.Logf("100 original reports -> %d ledger rows; 100 rounding-preserving charges = 200 bytes; replay unchanged", totals.Rows)
}

func TestSQLiteNativeMinuteProjectionRebuildOnlyProjectsOutstandingDelta(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	at := time.Now().UTC().Truncate(time.Minute)
	complete := func(id string) {
		t.Helper()
		_, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "projection-minute", FlowID: id, EventID: id, EventType: "flow.completed", Sequence: 1, BytesUp: 10, OccurredAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	complete("first")
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	if n, err := worker.Drain(t.Context(), 200); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	complete("second")
	if err := h.db.Transaction(datastore.RebuildTrafficHourly); err != nil {
		t.Fatal(err)
	}
	sum := func(want int64) {
		t.Helper()
		var bytes int64
		if err := h.db.Table("traffic_usage_hourly").Select("COALESCE(SUM(raw_bytes),0)").Scan(&bytes).Error; err != nil || bytes != want {
			t.Fatal("double or missing projection", bytes, want, err)
		}
	}
	sum(10)
	if _, err := worker.Drain(t.Context(), 200); err != nil {
		t.Fatal(err)
	}
	sum(20)
	if err := h.db.Transaction(datastore.RebuildTrafficHourly); err != nil {
		t.Fatal(err)
	}
	sum(20)
}

func TestSQLiteNativeMinuteSettlementKeepsQuotaCap(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", c.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", c.SubscriptionID).Updates(map[string]any{"flow_total": 17, "flow_used": 0}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().AddDate(-1, 0, 0).Truncate(time.Minute)
	for i := 0; i < 20; i++ {
		_, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "capped-minute", FlowID: fmt.Sprint(i), EventID: fmt.Sprint("cap-", i), EventType: "flow.completed", Sequence: uint64(i + 1), BytesUp: 1, OccurredAt: at})
		if err != nil {
			t.Fatal(i, err)
		}
	}
	var sub model.Subscription
	if err := h.db.First(&sub, c.SubscriptionID).Error; err != nil || sub.FlowUsed != 17 {
		t.Fatal("cap changed", sub, err)
	}
	var totals struct{ Raw, Used, Reports int64 }
	if err := h.db.Model(&model.TrafficRecord{}).Select("SUM(raw_bytes) AS raw,SUM(used_bytes) AS used,SUM(record_count) AS reports").Scan(&totals).Error; err != nil || totals.Raw != 20 || totals.Used != 17 || totals.Reports != 20 {
		t.Fatal(totals, err)
	}
}

func TestMySQLNativeMinuteConcurrentSettlementAndProjection(t *testing.T) {
	h, c := accountingFixtureFromOrder(t, newMySQLBusinessOrderFixture(t))
	// An isolated test clock makes all writers target one physical settlement.
	at := time.Now().UTC().Truncate(time.Minute).Add(59 * time.Second)
	h.db.NowFunc = func() time.Time { return at }
	if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id=?", c.ProtocolEndpointID).Update("multiplier_milli", 1001).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	var wg sync.WaitGroup
	done := make(chan struct{})
	errs := make(chan error, 3)
	go func() {
		for {
			select {
			case <-done:
				errs <- nil
				return
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			default:
			}
			if _, err := worker.Drain(ctx, 200); err != nil {
				errs <- err
				return
			}
			select {
			case <-done:
				errs <- nil
				return
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	for writer := 0; writer < 2; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				id := fmt.Sprintf("concurrent-%d-%d", writer, i)
				input := metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "concurrent-minute", FlowID: id, EventID: id, EventType: "flow.completed", Sequence: uint64(i + 1), BytesUp: 1, OccurredAt: at.AddDate(-2, 0, 0)}
				for replay := 0; replay < 2; replay++ {
					if _, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(ctx, input); err != nil {
						errs <- err
						return
					}
				}
			}
			errs <- nil
		}(writer)
	}
	wg.Wait()
	close(done)
	for range 3 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	for {
		n, err := worker.Drain(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var ledger struct{ Rows, Reports, Raw, Used int64 }
	if err := h.db.Model(&model.TrafficRecord{}).Select("COUNT(*) AS rows,SUM(record_count) AS reports,SUM(raw_bytes) AS raw,SUM(used_bytes) AS used").Scan(&ledger).Error; err != nil || ledger.Rows != 1 || ledger.Reports != 24 || ledger.Raw != 24 || ledger.Used != 48 {
		t.Fatal(ledger, err)
	}
	var usage struct{ Reports, Used int64 }
	if err := h.db.Table("traffic_usage_hourly").Select("SUM(record_count) AS reports,SUM(used_bytes) AS used").Scan(&usage).Error; err != nil || usage.Reports != 24 || usage.Used != 48 {
		t.Fatal(usage, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 48, subStatusActive)
}

func TestSQLiteNativeMinuteNewDeltaFailureRollsBackExistingBucket(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	at := time.Now().UTC().Truncate(time.Minute)
	h.db.NowFunc = func() time.Time { return at.Add(59 * time.Second) }
	complete := func(id string) error {
		_, _, err := h.services.CompletionAccounting(h.credentialCipher).Complete(t.Context(), metering.CompletedFlow{NodeID: c.NodeID, PrincipalKey: c.PrincipalKey, CoreInstanceID: "rollback-minute", FlowID: id, EventID: id, EventType: "flow.completed", Sequence: 1, BytesUp: 10, OccurredAt: at})
		return err
	}
	if err := complete("first"); err != nil {
		t.Fatal(err)
	}
	var before model.TrafficRecord
	if err := h.db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("pending delta checkpoint failed")
	const callback = "test:minute_delta_rollback"
	if err := h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "metering_usage_projection_pending" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := complete("second")
	h.db.Callback().Create().Remove(callback)
	if !errors.Is(err, injected) {
		t.Fatal("failed delta committed", err)
	}
	var after model.TrafficRecord
	if err := h.db.First(&after, before.ID).Error; err != nil || after.RawBytes != 10 || after.RecordCount != 1 || after.Meta != before.Meta {
		t.Fatal(after, err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 10, subStatusActive)
	if err := complete("second"); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 20, subStatusActive)
}
