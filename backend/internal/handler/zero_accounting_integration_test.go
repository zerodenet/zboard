package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestAccountingBatchReplayOutOfOrderAndExhaustion(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	checkAccountingBatchReplayOutOfOrderAndExhaustion(t, h, credential)
}

func TestMySQLAccountingBatchReplayOutOfOrderAndExhaustion(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	checkAccountingBatchReplayOutOfOrderAndExhaustion(t, h, credential)
}

func TestMySQLAccountingPreservesReportCollationIdempotency(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	events := accountingBenchmarkEvents(credential, 0, 2)
	events[0].ID, events[1].ID = "Case-Event", "case-event"
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
	events[0].ID, events[1].ID = "CASE-EVENT", "case-EVENT"
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 30, subStatusActive)
}

func checkAccountingBatchReplayOutOfOrderAndExhaustion(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	ctx := context.Background()
	for _, batch := range []int{0, 1, 1} {
		if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, batch, 32)); err != nil {
			t.Fatal(err)
		}
	}
	stale := accountingBenchmarkEvents(credential, 0, 32)
	for i := range stale {
		stale[i].ID += "-late"
	}
	if err := h.projectZeroNodeEvents(ctx, stale); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 1920, subStatusActive)
	var count int64
	if err := h.db.Model(&model.TrafficRecord{}).Count(&count).Error; err != nil || count != 64 {
		t.Fatalf("replayed/old events persisted: count=%d error=%v", count, err)
	}
	// Only 30 bytes remain, although the next batch reports 960 new bytes.
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Update("flow_total", 1950).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 2, 32)); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 1950, subStatusExpired)
	var pending model.NodeConfigPublish
	if err := h.db.First(&pending, credential.NodeID).Error; err != nil || pending.Generation != 2 {
		t.Fatalf("exhaustion publication: %+v %v", pending, err)
	}
	var current model.ProtocolCredential
	if err := h.db.First(&current, credential.ID).Error; err != nil || current.Status != "expired" {
		t.Fatalf("credential not expired: status=%s error=%v", current.Status, err)
	}
}

func TestAccountingBatchDoesNotShareBalancesOrKeepOldMultiplier(t *testing.T) {
	h, first := accountingBenchmarkFixture(t)
	var secondSubscription model.Subscription
	if err := h.db.First(&secondSubscription, first.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	secondSubscription.ID = 0
	if err := h.db.Create(&secondSubscription).Error; err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID, second.SubscriptionID = 0, secondSubscription.ID
	second.CredentialID, second.PrincipalKey = "second-credential", "second-principal"
	if err := h.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 2; batch++ {
		events := accountingBenchmarkEvents(first, batch, 4)
		other := accountingBenchmarkEvents(second, batch, 4)
		for i := range other {
			other[i].ID += "-second"
			other[i].FlowID = fmt.Sprintf("second-%d", i)
			other[i].Payload = []byte(fmt.Sprintf(`{"flow_id":%q,"traffic":{"bytes_up":%d,"bytes_down":%d}}`, other[i].FlowID, (batch+1)*10, (batch+1)*20))
		}
		if err := h.projectZeroNodeEvents(context.Background(), append(events, other...)); err != nil {
			t.Fatal(err)
		}
		want := int64(120)
		if batch == 1 {
			want = 360
		}
		assertAccountingTotal(t, h, first.SubscriptionID, want, subStatusActive)
		assertAccountingTotal(t, h, second.SubscriptionID, want, subStatusActive)
		if err := h.db.Model(&model.ProtocolEndpoint{}).Where("id = ?", first.ProtocolEndpointID).Update("multiplier_milli", 2000).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccountingBatchFinalWriteFailureRollsBackAndCanReplay(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Exec(`CREATE TRIGGER reject_credential_touch BEFORE UPDATE ON protocol_credentials
 WHEN NEW.last_used_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'last used write unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 205)
	if err := h.projectZeroNodeEvents(context.Background(), events); err == nil {
		t.Fatal("final write error ignored")
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	for _, table := range []string{"flow_usages", "traffic_records", "zero_event_node_cursors"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial commit in %s: %d %v", table, count, err)
		}
	}
	if err := h.db.Exec("DROP TRIGGER reject_credential_touch").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 6150, subStatusActive)
}

func TestAccountingBatchDeduplicatesReportIDsAcrossInsertChunks(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	events := accountingBenchmarkEvents(credential, 0, 205)
	events[204].ID = events[0].ID
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 6120, subStatusActive)
	for i := range events {
		events[i].ID = " " + events[i].ID + " "
	}
	if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 6120, subStatusActive)
}

func assertAccountingTotal(t *testing.T, h *handlers, subscriptionID uint, want int64, status string) {
	t.Helper()
	var subscription model.Subscription
	if err := h.db.First(&subscription, subscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.FlowUsed != want || subscription.Status != status {
		t.Fatalf("subscription used=%d status=%s, want %d %s", subscription.FlowUsed, subscription.Status, want, status)
	}
	var ledger int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("subscription_id = ?", subscriptionID).Select("COALESCE(SUM(used_bytes),0)").Scan(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != want {
		t.Fatalf("ledger=%d, want %d", ledger, want)
	}
}
