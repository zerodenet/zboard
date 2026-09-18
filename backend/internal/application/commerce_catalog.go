package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/commercestore"
	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

type orderListCapabilityInput struct {
	Status string `json:"status,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (s *Services) RegisterCommerceCapabilities(registry *catalog.Registry, cipher zeroadapter.Cipher, mieru bool) error {
	orders := catalog.Descriptor{
		Name: "commerce.orders.list", Version: "1.0", Owner: "commerce", Kind: "query", Sensitivity: "account_orders",
		Authorization: "Current credential account only; account identity is taken from the authenticated principal.",
		Idempotency:   "read_only", Execution: "synchronous", TimeoutMillis: 10000, Quota: "64 KiB input; maximum 200 orders per page", RateLimitPerMinute: 60,
		ErrorCodes: []string{"unauthenticated", "permission_denied", "invalid_argument", "rate_limited", "unavailable", "deadline_exceeded"}, Compatibility: "Additive response fields within v1; breaking changes require a new major version", Deprecation: "none",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"status":{"enum":["pending","paid","failed","canceled"]},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":200,"default":50}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["items","total","offset","limit"],"properties":{"items":{"type":"array","maxItems":200},"total":{"type":"integer"},"offset":{"type":"integer"},"limit":{"type":"integer"}}}`),
	}
	if err := registry.Register(orders, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		in, err := catalog.DecodeObject[orderListCapabilityInput](raw)
		if err != nil {
			return nil, err
		}
		out, err := s.OrderQueries.Owned(ctx, grant.Principal.AccountID, commerce.OrderQuery{Status: in.Status, Offset: in.Offset, Limit: in.Limit})
		if err != nil {
			var invalid *commerce.ValidationError
			if errors.As(err, &invalid) {
				return nil, catalog.ErrInput
			}
			if errors.Is(err, commerce.ErrOrderPermission) {
				return nil, catalog.ErrDenied
			}
			return nil, err
		}
		return out, nil
	}); err != nil {
		return err
	}
	payment := catalog.Descriptor{
		Name: "commerce.payments.record", Version: "1.0", Owner: "commerce", Kind: "command", Sensitivity: "payment_fact",
		Authorization: "Integration credential with the exact scope; the authenticated account must own the order.",
		Idempotency:   "provider_event_id is exactly-once within the authenticated integration identity", Execution: "synchronous_transactional", TimeoutMillis: 30000, Quota: "64 KiB input; one payment fact", RateLimitPerMinute: 120,
		ErrorCodes: []string{"unauthenticated", "permission_denied", "invalid_argument", "conflict", "rate_limited", "unavailable", "deadline_exceeded"}, Compatibility: "Additive response fields within v1; breaking changes require a new major version", Deprecation: "none",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["order_id","provider_event_id","provider_trade_no","status","amount_minor","currency","occurred_at"],"properties":{"order_id":{"type":"integer","minimum":1},"provider_event_id":{"type":"string","minLength":1,"maxLength":128},"provider_trade_no":{"type":"string","minLength":1,"maxLength":128},"status":{"enum":["paid","failed"]},"amount_minor":{"type":"integer","minimum":1},"currency":{"type":"string","minLength":3,"maxLength":3},"occurred_at":{"type":"string","format":"date-time"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["order_id","status","fulfilled"],"properties":{"order_id":{"type":"integer"},"status":{"enum":["paid","failed"]},"fulfilled":{"type":"boolean"}}}`),
	}
	facts := commerce.PaymentFacts{Repository: commercestore.PaymentFacts{DB: s.Identity.db, Issuer: zeroadapter.Issuer{Cipher: cipher, Mieru: mieru}}}
	return registry.Register(payment, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		if grant.Principal.Kind != "integration" {
			return nil, catalog.ErrDenied
		}
		in, err := catalog.DecodeObject[commerce.PaymentFact](raw)
		if err != nil {
			return nil, err
		}
		out, err := facts.Record(ctx, commerce.PaymentFactAuthority{AccountID: grant.Principal.AccountID, Provider: fmt.Sprintf("integration:%d", grant.Principal.CredentialID)}, in)
		if err != nil {
			var invalid *commerce.ValidationError
			switch {
			case errors.As(err, &invalid):
				return nil, catalog.ErrInput
			case errors.Is(err, commerce.ErrOrderPermission):
				return nil, catalog.ErrDenied
			case errors.Is(err, commerce.ErrPaymentFactConflict), errors.Is(err, commerce.ErrOrderTransition):
				return nil, catalog.ErrConflict
			default:
				return nil, err
			}
		}
		return out, nil
	})
}
