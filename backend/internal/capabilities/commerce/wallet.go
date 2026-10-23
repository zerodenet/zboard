package commerce

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrWalletUnavailable = errors.New("wallet facts unavailable")
var ErrWalletRange = errors.New("wallet amount exceeds supported integer range")

// HTTP numeric amounts must also be exact in the browser's integer range.
const MaxWalletInputMinor int64 = 9007199254740991

type TopUpRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
}
type WalletEntry struct {
	ID            uint      `json:"id"`
	Kind          string    `json:"kind"`
	BalanceDelta  int64     `json:"balance_delta,string"`
	ReservedDelta int64     `json:"reserved_delta,string"`
	BalanceAfter  int64     `json:"balance_after,string"`
	ReservedAfter int64     `json:"reserved_after,string"`
	SourceOrderID *uint     `json:"source_order_id,omitempty"`
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"created_at"`
}
type WalletPage struct {
	Currency       string        `json:"currency"`
	BalanceMinor   int64         `json:"balance_minor,string"`
	ReservedMinor  int64         `json:"reserved_minor,string"`
	AvailableMinor int64         `json:"available_minor,string"`
	Entries        []WalletEntry `json:"entries"`
	HasMore        bool          `json:"has_more"`
}
type WalletRepository interface {
	Read(context.Context, uint, string, uint, int) (WalletPage, error)
	CreateTopUp(context.Context, uint, TopUpRequest) (Order, error)
}
type Wallet struct{ Repository WalletRepository }

func NormalizeWalletCurrency(in string) (string, error) {
	in = strings.ToUpper(strings.TrimSpace(in))
	if len(in) != 3 {
		return "", &ValidationError{Message: "请输入三位币种代码。"}
	}
	for _, v := range in {
		if v < 'A' || v > 'Z' {
			return "", &ValidationError{Message: "请输入三位币种代码。"}
		}
	}
	return in, nil
}
func (s Wallet) Read(ctx context.Context, actor uint, currency string, after uint, limit int) (WalletPage, error) {
	if actor == 0 {
		return WalletPage{}, ErrOrderPermission
	}
	currency, err := NormalizeWalletCurrency(currency)
	if err != nil {
		return WalletPage{}, err
	}
	if limit < 1 || limit > 100 {
		return WalletPage{}, &ValidationError{Message: "余额流水每页应为 1–100 条。"}
	}
	return s.Repository.Read(ctx, actor, currency, after, limit)
}
func (s Wallet) CreateTopUp(ctx context.Context, actor uint, in TopUpRequest) (Order, error) {
	if actor == 0 {
		return Order{}, ErrOrderPermission
	}
	var err error
	in.Currency, err = NormalizeWalletCurrency(in.Currency)
	if err != nil {
		return Order{}, err
	}
	if in.AmountMinor <= 0 || in.AmountMinor > MaxWalletInputMinor || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 80 {
		return Order{}, &ValidationError{Message: "请输入可靠整数范围内的充值金额和本次请求标识。"}
	}
	return s.Repository.CreateTopUp(ctx, actor, in)
}
