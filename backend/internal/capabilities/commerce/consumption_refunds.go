package commerce

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrConsumptionRefundConflict = errors.New("consumption refund conflict")
var ErrConsumptionRefundReview = errors.New("consumption refund requires entitlement review")

// Approval is an administrator's recorded decision, not a payment-provider
// callback. The repository must authorize the actor and verify original funding.
type ConsumptionRefundRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	AmountMinor    int64  `json:"amount_minor"`
	TicketID       uint   `json:"ticket_id"`
	Reason         string `json:"reason"`
}

type ConsumptionRefundSummary struct {
	ID          uint      `json:"id"`
	OrderID     uint      `json:"order_id"`
	UserID      uint      `json:"user_id"`
	Currency    string    `json:"currency"`
	AmountMinor int64     `json:"amount_minor,string"`
	TicketID    uint      `json:"ticket_id"`
	ActorID     uint      `json:"actor_id"`
	Reason      string    `json:"reason"`
	EntryID     uint      `json:"entry_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type ConsumptionRefundRepository interface {
	Approve(context.Context, uint, uint, ConsumptionRefundRequest) (ConsumptionRefundSummary, error)
	Read(context.Context, uint, uint) (ConsumptionRefundSummary, error)
}

type ConsumptionRefunds struct{ Repository ConsumptionRefundRepository }

func (s ConsumptionRefunds) Approve(ctx context.Context, actor, order uint, in ConsumptionRefundRequest) (ConsumptionRefundSummary, error) {
	if actor == 0 {
		return ConsumptionRefundSummary{}, ErrOrderPermission
	}
	if order == 0 {
		return ConsumptionRefundSummary{}, ErrNotFound
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.AmountMinor <= 0 || in.AmountMinor > MaxWalletInputMinor || in.TicketID == 0 || len(in.Reason) < 3 || len(in.Reason) > 400 || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 80 {
		return ConsumptionRefundSummary{}, &ValidationError{Message: "请填写获批退款的正金额、原订单同账户工单、原因及请求标识。"}
	}
	if s.Repository == nil {
		return ConsumptionRefundSummary{}, ErrWalletUnavailable
	}
	return s.Repository.Approve(ctx, actor, order, in)
}

func (s ConsumptionRefunds) Read(ctx context.Context, actor, id uint) (ConsumptionRefundSummary, error) {
	if actor == 0 {
		return ConsumptionRefundSummary{}, ErrOrderPermission
	}
	if id == 0 {
		return ConsumptionRefundSummary{}, ErrNotFound
	}
	if s.Repository == nil {
		return ConsumptionRefundSummary{}, ErrWalletUnavailable
	}
	return s.Repository.Read(ctx, actor, id)
}
