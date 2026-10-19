package commerce

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrSubscriptionSettlementConflict = errors.New("subscription value settlement conflict")
var ErrSubscriptionQuoteChanged = errors.New("subscription value quote changed")
var valueQuotePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SubscriptionTerminationRequest struct {
	QuoteFingerprint string `json:"quote_fingerprint"`
	IdempotencyKey   string `json:"idempotency_key"`
}
type SubscriptionTerminationReceipt struct {
	ID               uint      `json:"id"`
	SubscriptionID   uint      `json:"subscription_id"`
	Currency         string    `json:"currency"`
	AmountMinor      int64     `json:"amount_minor,string"`
	WalletEntryID    *uint     `json:"wallet_entry_id,omitempty"`
	Status           string    `json:"status"`
	RevocationStatus string    `json:"revocation_status"`
	CreatedAt        time.Time `json:"created_at"`
}
type SubscriptionTerminationRepository interface {
	Terminate(context.Context, uint, uint, SubscriptionTerminationRequest) (SubscriptionTerminationReceipt, error)
}
type SubscriptionTermination struct {
	Repository SubscriptionTerminationRepository
}

func (s SubscriptionTermination) Terminate(ctx context.Context, actor, subscription uint, in SubscriptionTerminationRequest) (SubscriptionTerminationReceipt, error) {
	if actor == 0 {
		return SubscriptionTerminationReceipt{}, ErrOrderPermission
	}
	if subscription == 0 {
		return SubscriptionTerminationReceipt{}, ErrNotFound
	}
	if !valueQuotePattern.MatchString(in.QuoteFingerprint) || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 80 {
		return SubscriptionTerminationReceipt{}, &ValidationError{Message: "请先核对退订报价，并保留原请求标识。"}
	}
	if s.Repository == nil {
		return SubscriptionTerminationReceipt{}, ErrSubscriptionValueUnavailable
	}
	return s.Repository.Terminate(ctx, actor, subscription, in)
}
