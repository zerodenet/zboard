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
	assertAccountingTotal(t, h, credential.SubscriptionID, 20, subStatusExpired)
	var usage model.ProtocolEndpointUsageDaily
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 20 || usage.RecordCount != 1 {
		t.Fatalf("completion usage projection: %+v %v", usage, err)
	}
	repeated, _, err := service.Complete(context.Background(), in)
	if err != nil || repeated.ID != record.ID {
		t.Fatalf("replay: %+v %v", repeated, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 20, subStatusExpired)
	usage = model.ProtocolEndpointUsageDaily{}
	if err := h.db.First(&usage, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || usage.UsedBytes != 20 || usage.RecordCount != 1 {
		t.Fatalf("replay changed usage projection: %+v %v", usage, err)
	}
}
