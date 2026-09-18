package commerce

import (
	"context"
	"errors"
	"strings"
)

var ErrOrderTransition = errors.New("order transition rejected")

type SettlementCommand struct {
	OrderID  uint
	Status   string
	Force    bool
	Callback *string
}
type SettlementResult struct {
	Order     Order
	Fulfilled bool
}
type OrderSettlementRepository interface {
	Apply(context.Context, uint, SettlementCommand) (SettlementResult, error)
}
type OrderSettlement struct{ Repository OrderSettlementRepository }

func (s OrderSettlement) Apply(ctx context.Context, actor uint, in SettlementCommand) (SettlementResult, error) {
	if actor == 0 {
		return SettlementResult{}, ErrOrderPermission
	}
	if in.OrderID == 0 {
		return SettlementResult{}, ErrNotFound
	}
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status == "" || in.Status == "success" {
		in.Status = "paid"
	}
	switch in.Status {
	case "paid", "failed", "canceled":
	default:
		return SettlementResult{}, &ValidationError{Message: "invalid callback status"}
	}
	return s.Repository.Apply(ctx, actor, in)
}
