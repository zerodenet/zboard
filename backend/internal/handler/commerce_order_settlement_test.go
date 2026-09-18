package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestCommerceSettlementRechecksAdministratorForNewAndRepeatedResults(t *testing.T) {
	f := newOrderFixture(t)
	first := f.create(t, 0)
	second := f.create(t, 0)
	service := f.h.services.OrderSettlement(f.h.credentialCipher, f.h.zeroMieruAccess)
	command := commerce.SettlementCommand{OrderID: first.ID, Status: "paid"}
	out, err := service.Apply(context.Background(), 1, command)
	if err != nil || !out.Fulfilled || out.Order.Status != "paid" {
		t.Fatalf("settlement: %+v %v", out, err)
	}
	if err := f.h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{first.ID, second.ID} {
		command.OrderID = id
		out, err := service.Apply(context.Background(), 1, command)
		if !errors.Is(err, commerce.ErrOrderPermission) || out.Order.ID != 0 || out.Fulfilled {
			t.Fatalf("revoked settlement: %+v %v", out, err)
		}
	}
	var stored model.Order
	if err := f.h.db.First(&stored, second.ID).Error; err != nil || stored.Status != "pending" || stored.SubscriptionID != 0 {
		t.Fatalf("denied mutation: %+v %v", stored, err)
	}
}
