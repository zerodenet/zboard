package commerce

import (
	"context"
	"strings"
	"time"
)

type PaymentReceiptSummary struct {
	Provider         string    `json:"provider,omitempty"`
	ProviderTradeNo  string    `json:"provider_trade_no,omitempty"`
	ID               uint      `json:"id"`
	AmountMinor      int64     `json:"amount_minor,string"`
	Currency         string    `json:"currency"`
	Disposition      string    `json:"disposition"`
	WalletEntryID    *uint     `json:"wallet_entry_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	TicketID         *uint     `json:"ticket_id,omitempty"`
	ResolutionReason string    `json:"resolution_reason,omitempty"`
}
type PaymentReceiptPage struct {
	Items   []PaymentReceiptSummary `json:"items"`
	HasMore bool                    `json:"has_more"`
}
type ResolveReceiptRequest struct {
	ReceiptID uint   `json:"receipt_id"`
	TicketID  uint   `json:"ticket_id"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
}
type PaymentReceiptsRepository interface {
	Read(context.Context, uint, uint, bool, uint, int) (PaymentReceiptPage, error)
	Resolve(context.Context, uint, uint, ResolveReceiptRequest) (PaymentReceiptSummary, error)
}
type PaymentReceipts struct{ Repository PaymentReceiptsRepository }

func (s PaymentReceipts) Read(ctx context.Context, actor, order uint, admin bool, after uint, limit int) (PaymentReceiptPage, error) {
	if actor == 0 {
		return PaymentReceiptPage{}, ErrOrderPermission
	}
	if order == 0 {
		return PaymentReceiptPage{}, ErrNotFound
	}
	if limit < 1 || limit > 100 {
		return PaymentReceiptPage{}, &ValidationError{Message: "收据分页参数无效。"}
	}
	return s.Repository.Read(ctx, actor, order, admin, after, limit)
}
func (s PaymentReceipts) Resolve(ctx context.Context, actor, order uint, in ResolveReceiptRequest) (PaymentReceiptSummary, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if actor == 0 {
		return PaymentReceiptSummary{}, ErrOrderPermission
	}
	if order == 0 || in.ReceiptID == 0 || in.TicketID == 0 || len(in.Reason) < 3 || len(in.Reason) > 500 || (in.Decision != "credit_wallet" && in.Decision != "already_applied") {
		return PaymentReceiptSummary{}, &ValidationError{Message: "请选择核对结论，填写同一用户的工单与核对依据。"}
	}
	return s.Repository.Resolve(ctx, actor, order, in)
}
