package commerce

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrWalletAdjustmentConflict = errors.New("wallet adjustment conflict")

type WalletDebitRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	UserID         uint   `json:"user_id"`
	Currency       string `json:"currency"`
	AmountMinor    int64  `json:"amount_minor"`
	TicketID       uint   `json:"ticket_id"`
	Reason         string `json:"reason"`
}
type WalletPayoutRequest struct {
	IdempotencyKey string    `json:"idempotency_key"`
	Outcome        string    `json:"outcome"`
	Reference      string    `json:"reference"`
	Note           string    `json:"note"`
	OccurredAt     time.Time `json:"occurred_at"`
}
type WalletReversalRequest struct {
	Reason string `json:"reason"`
}
type WalletPayoutSummary struct {
	ID         uint      `json:"id"`
	ActorID    uint      `json:"actor_id"`
	Outcome    string    `json:"outcome"`
	Reference  string    `json:"reference"`
	Note       string    `json:"note"`
	OccurredAt time.Time `json:"occurred_at"`
	CreatedAt  time.Time `json:"created_at"`
}
type WalletAdjustmentSummary struct {
	ID              uint                  `json:"id"`
	UserID          uint                  `json:"user_id"`
	Currency        string                `json:"currency"`
	AmountMinor     int64                 `json:"amount_minor,string"`
	Reason          string                `json:"reason"`
	TicketID        uint                  `json:"ticket_id"`
	ActorID         uint                  `json:"actor_id"`
	EntryID         uint                  `json:"entry_id"`
	ReversalEntryID *uint                 `json:"reversal_entry_id,omitempty"`
	ReversalActorID *uint                 `json:"reversal_actor_id,omitempty"`
	ReversalReason  string                `json:"reversal_reason,omitempty"`
	ExternalStatus  string                `json:"external_status"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	Results         []WalletPayoutSummary `json:"results,omitempty"`
}
type WalletAdjustmentPage struct {
	Items   []WalletAdjustmentSummary `json:"items"`
	HasMore bool                      `json:"has_more"`
}
type WalletAdjustmentsRepository interface {
	Debit(context.Context, uint, WalletDebitRequest) (WalletAdjustmentSummary, error)
	Read(context.Context, uint, uint) (WalletAdjustmentSummary, error)
	List(context.Context, uint, uint, string, uint, int) (WalletAdjustmentPage, error)
	RecordPayout(context.Context, uint, uint, WalletPayoutRequest) (WalletAdjustmentSummary, error)
	Reverse(context.Context, uint, uint, WalletReversalRequest) (WalletAdjustmentSummary, error)
	AdministrativeWallet(context.Context, uint, uint, string, uint, int) (WalletPage, error)
}
type WalletAdjustments struct{ Repository WalletAdjustmentsRepository }

func (s WalletAdjustments) Debit(ctx context.Context, actor uint, in WalletDebitRequest) (WalletAdjustmentSummary, error) {
	if actor == 0 {
		return WalletAdjustmentSummary{}, ErrOrderPermission
	}
	var err error
	in.Currency, err = NormalizeWalletCurrency(in.Currency)
	if err != nil {
		return WalletAdjustmentSummary{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.UserID == 0 || in.TicketID == 0 || in.AmountMinor <= 0 || in.AmountMinor > MaxWalletInputMinor || len(in.Reason) < 3 || len(in.Reason) > 400 || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 80 {
		return WalletAdjustmentSummary{}, &ValidationError{Message: "请填写可核对的用户、正金额、同用户工单、原因及请求标识。"}
	}
	return s.Repository.Debit(ctx, actor, in)
}
func (s WalletAdjustments) Read(ctx context.Context, actor, id uint) (WalletAdjustmentSummary, error) {
	if actor == 0 {
		return WalletAdjustmentSummary{}, ErrOrderPermission
	}
	if id == 0 {
		return WalletAdjustmentSummary{}, ErrNotFound
	}
	return s.Repository.Read(ctx, actor, id)
}
func (s WalletAdjustments) List(ctx context.Context, actor, user uint, currency string, after uint, limit int) (WalletAdjustmentPage, error) {
	if actor == 0 {
		return WalletAdjustmentPage{}, ErrOrderPermission
	}
	if user == 0 || limit < 1 || limit > 100 {
		return WalletAdjustmentPage{}, &ValidationError{Message: "用户或分页参数无效。"}
	}
	currency, err := NormalizeWalletCurrency(currency)
	if err != nil {
		return WalletAdjustmentPage{}, err
	}
	return s.Repository.List(ctx, actor, user, currency, after, limit)
}
func (s WalletAdjustments) AdministrativeWallet(ctx context.Context, actor, user uint, currency string, after uint, limit int) (WalletPage, error) {
	if actor == 0 {
		return WalletPage{}, ErrOrderPermission
	}
	if user == 0 || limit < 1 || limit > 100 {
		return WalletPage{}, &ValidationError{Message: "用户或分页参数无效。"}
	}
	currency, err := NormalizeWalletCurrency(currency)
	if err != nil {
		return WalletPage{}, err
	}
	return s.Repository.AdministrativeWallet(ctx, actor, user, currency, after, limit)
}
func (s WalletAdjustments) RecordPayout(ctx context.Context, actor, id uint, in WalletPayoutRequest) (WalletAdjustmentSummary, error) {
	if actor == 0 {
		return WalletAdjustmentSummary{}, ErrOrderPermission
	}
	if id == 0 {
		return WalletAdjustmentSummary{}, ErrNotFound
	}
	in.Outcome = strings.ToLower(strings.TrimSpace(in.Outcome))
	in.Reference = strings.TrimSpace(in.Reference)
	in.Note = strings.TrimSpace(in.Note)
	in.OccurredAt = in.OccurredAt.UTC()
	if (in.Outcome != "unknown" && in.Outcome != "succeeded" && in.Outcome != "failed") || len(in.Reference) > 128 || len(in.Note) > 500 || in.OccurredAt.IsZero() || in.OccurredAt.After(time.Now().UTC().Add(10*time.Minute)) || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 80 {
		return WalletAdjustmentSummary{}, &ValidationError{Message: "请输入人工核对的外部结果、观察时间与请求标识；本操作不会执行出款。"}
	}
	if in.Outcome != "unknown" && in.Reference == "" && len(in.Note) < 3 {
		return WalletAdjustmentSummary{}, &ValidationError{Message: "确认出款成功或失败时，请提供流水引用或核对依据。"}
	}
	return s.Repository.RecordPayout(ctx, actor, id, in)
}
func (s WalletAdjustments) Reverse(ctx context.Context, actor, id uint, in WalletReversalRequest) (WalletAdjustmentSummary, error) {
	if actor == 0 {
		return WalletAdjustmentSummary{}, ErrOrderPermission
	}
	if id == 0 {
		return WalletAdjustmentSummary{}, ErrNotFound
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 3 || len(in.Reason) > 400 {
		return WalletAdjustmentSummary{}, &ValidationError{Message: "请记录已确认出款失败后的余额恢复依据。"}
	}
	return s.Repository.Reverse(ctx, actor, id, in)
}
