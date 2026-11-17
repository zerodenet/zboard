package handler

import (
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestCompletionRetriesRolledBackDeadlockWithoutDoubleCharge(t *testing.T) {
	h, c := accountingBenchmarkFixture(t)
	failed := false
	const name = "test:completion_deadlock"
	if err := h.db.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "subscriptions" && !failed {
			failed = true
			tx.AddError(&mysqlDriver.MySQLError{Number: 1213, Message: "injected deadlock after quota update"})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.db.Callback().Update().Remove(name)
	in := metering.CompletedFlow{NodeID: c.NodeID, CoreInstanceID: "retry-core", EventID: "retry-completion", EventType: "flow.completed", FlowID: "retry-flow", PrincipalKey: c.PrincipalKey, Sequence: 1, BytesUp: 1, BytesDown: 2, OccurredAt: time.Now().UTC()}
	service := h.services.CompletionAccounting(h.credentialCipher)
	if _, _, err := service.Complete(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Fatal("quota failure was not exercised")
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 3, subStatusActive)
	if _, _, err := service.Complete(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, c.SubscriptionID, 3, subStatusActive)
	var result model.MeteringSettlementResult
	if err := h.db.Where("subscription_id=?", c.SubscriptionID).First(&result).Error; err != nil || result.UsedBytes != 3 || result.RecordCount != 1 {
		t.Fatal(result, err)
	}
}
