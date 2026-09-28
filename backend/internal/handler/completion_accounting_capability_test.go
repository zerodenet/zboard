package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestCompletionCapabilityRollsBackExhaustionAndReplays(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Updates(map[string]interface{}{"flow_total": 20, "flow_used": 0}).Error; err != nil {
		t.Fatal(err)
	}
	in := metering.CompletedFlow{NodeID: credential.NodeID, EventID: "completion-capability", FlowID: "flow-final", PrincipalKey: credential.PrincipalKey, CoreInstanceID: "runtime-final", EventType: "flow.completed", Sequence: 1, Revision: 1, BytesUp: 1000, OccurredAt: time.Now().UTC()}
	failure := errors.New("publication failed")
	broken := metering.CompletionAccounting{Repository: meteringstore.CompletionAccounting{DB: h.db, Cipher: h.credentialCipher, EnqueuePublications: func(*gorm.DB, uint, uint) error { return failure }}}
	record, exhausted, err := broken.Complete(context.Background(), in)
	if !errors.Is(err, failure) || record.ID != 0 || exhausted {
		t.Fatalf("failed outcome escaped: %+v %t %v", record, exhausted, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 0, subStatusActive)
	var count int64
	if err = h.db.Model(&model.TrafficRecord{}).Where("report_id = ?", in.EventID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed transaction retained record: %d %v", count, err)
	}
	if err = h.db.Model(&model.ProtocolEndpointUsageDaily{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed transaction retained usage projection: %d %v", count, err)
	}
	service := h.services.CompletionAccounting(h.credentialCipher)
	record, exhausted, err = service.Complete(context.Background(), in)
	if err != nil || !exhausted || record.UsedBytes != 20 {
		t.Fatalf("completion: %+v %t %v", record, exhausted, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 20, subStatusActive)
	var current model.ProtocolCredential
	if err := h.db.First(&current, credential.ID).Error; err != nil || current.Status != "expired" {
		t.Fatal("exhaustion did not suspend credentials", current.Status, err)
	}
	var usage model.ProtocolEndpointUsageDaily
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 20 || usage.RecordCount != 1 {
		t.Fatalf("completion usage projection: %+v %v", usage, err)
	}
	repeated, _, err := service.Complete(context.Background(), in)
	if err != nil || repeated.ID != record.ID {
		t.Fatalf("replay: %+v %v", repeated, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 20, subStatusActive)
	usage = model.ProtocolEndpointUsageDaily{}
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 20 || usage.RecordCount != 1 {
		t.Fatalf("replay changed usage projection: %+v %v", usage, err)
	}
}

func TestSingleUseAccountingFreezesRenewalDeadlineBeforeWorkerRuns(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	if err := h.db.Model(&model.Subscription{}).Where("id = ?", credential.SubscriptionID).Updates(map[string]any{"flow_total": 20, "flow_used": 0, "lifecycle": "renewable", "ends_on_quota_exhaustion": true}).Error; err != nil {
		t.Fatal(err)
	}
	order := model.Order{UserID: 1, SubscriptionID: credential.SubscriptionID, TradeNo: "terminal-accounting-order", Status: "paid"}
	if err := h.db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	in := metering.CompletedFlow{NodeID: credential.NodeID, EventID: "single-use-final", FlowID: "single-use-final", PrincipalKey: credential.PrincipalKey, CoreInstanceID: "single-use-runtime", EventType: "flow.completed", Sequence: 1, Revision: 1, BytesUp: 1000, OccurredAt: time.Now().UTC()}
	if _, exhausted, err := h.services.CompletionAccounting(h.credentialCipher).Complete(context.Background(), in); err != nil || !exhausted {
		t.Fatal(exhausted, err)
	}
	var sub model.Subscription
	if err := h.db.First(&sub, credential.SubscriptionID).Error; err != nil || sub.EndedAt == nil || sub.Status != "expired" {
		t.Fatal(sub, err)
	}
	ended := *sub.EndedAt
	if due, err := h.services.CredentialExpiry().HasDue(context.Background(), ended.Add(time.Hour)); err != nil || !due {
		t.Fatal("terminal snapshot not scheduled", due, err)
	}
	if _, err := h.services.CredentialExpiry().ExpireDue(context.Background(), ended.Add(time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	sub = model.Subscription{}
	if err := h.db.First(&sub, credential.SubscriptionID).Error; err != nil || !sub.EndedAt.Equal(ended) {
		t.Fatal("worker moved deadline", sub, err)
	}
	if err := h.db.First(&order, order.ID).Error; err != nil || order.SubscriptionEndedAt == nil || !order.SubscriptionEndedAt.Equal(ended) || order.SubscriptionFinalFlowUsed != 20 {
		t.Fatal("terminal snapshot missing", order, err)
	}
	if due, err := h.services.CredentialExpiry().HasDue(context.Background(), ended.Add(time.Hour)); err != nil || due {
		t.Fatal("worker did not converge", due, err)
	}
}
