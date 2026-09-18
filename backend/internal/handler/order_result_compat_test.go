package handler

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type orderResultCommand struct {
	OrderID  uint
	Status   string
	Force    bool
	Actor    authClaims
	Callback *string
}

type orderResult struct {
	Order     model.Order
	Fulfilled bool
}

func (h *handlers) applyOrderResult(ctx context.Context, command orderResultCommand) (orderResult, error) {
	out, err := h.services.OrderSettlement(h.credentialCipher, h.zeroMieruAccess).Apply(ctx, command.Actor.UserID, commerce.SettlementCommand{OrderID: command.OrderID, Status: command.Status, Force: command.Force, Callback: command.Callback})
	if errors.Is(err, commerce.ErrNotFound) {
		err = gorm.ErrRecordNotFound
	}
	if errors.Is(err, commerce.ErrOrderTransition) {
		err = errOrderTransitionRejected
	}
	if err != nil {
		return orderResult{}, err
	}
	return orderResult{Order: model.Order(out.Order), Fulfilled: out.Fulfilled}, nil
}
