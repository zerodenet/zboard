package commerce

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrPaymentFactConflict = errors.New("payment fact conflict")

type PaymentFactAuthority struct {
	AccountID uint
	Provider  string
}

type PaymentFact struct {
	OrderID         uint      `json:"order_id"`
	ProviderEventID string    `json:"provider_event_id"`
	ProviderTradeNo string    `json:"provider_trade_no"`
	Status          string    `json:"status"`
	AmountMinor     int64     `json:"amount_minor"`
	Currency        string    `json:"currency"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type PaymentFactResult struct {
	OrderID   uint   `json:"order_id"`
	Status    string `json:"status"`
	Fulfilled bool   `json:"fulfilled"`
}

type PaymentFactRepository interface {
	Record(context.Context, PaymentFactAuthority, PaymentFact) (PaymentFactResult, error)
}

type PaymentFacts struct{ Repository PaymentFactRepository }

func (s PaymentFacts) Record(ctx context.Context, authority PaymentFactAuthority, fact PaymentFact) (PaymentFactResult, error) {
	fact.ProviderEventID = strings.TrimSpace(fact.ProviderEventID)
	fact.ProviderTradeNo = strings.TrimSpace(fact.ProviderTradeNo)
	fact.Status = strings.ToLower(strings.TrimSpace(fact.Status))
	fact.Currency = strings.ToUpper(strings.TrimSpace(fact.Currency))
	now := time.Now().UTC()
	if authority.AccountID == 0 || strings.TrimSpace(authority.Provider) == "" || len(authority.Provider) > 32 || fact.OrderID == 0 || fact.ProviderEventID == "" || len(fact.ProviderEventID) > 128 || fact.ProviderTradeNo == "" || len(fact.ProviderTradeNo) > 128 || (fact.Status != "paid" && fact.Status != "failed") || fact.AmountMinor <= 0 || len(fact.Currency) != 3 || fact.OccurredAt.IsZero() || fact.OccurredAt.After(now.Add(10*time.Minute)) {
		return PaymentFactResult{}, &ValidationError{Message: "invalid payment fact"}
	}
	return s.Repository.Record(ctx, authority, fact)
}
