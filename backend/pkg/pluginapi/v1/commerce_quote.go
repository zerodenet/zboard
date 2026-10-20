package pluginv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

const CommerceQuoteProviderCapability = "zboard.commerce.quote.provider.v1"
const MaxCommerceQuoteBytes = 16 << 10
const MaxCommerceQuoteTermsBytes = 8 << 10

// CommerceDiscountContext comes only from the native host RPC. A page's fields
// are not authoritative. Evaluating this context must not consume/reserve a
// coupon; this version defines no redemption lifecycle.
type CommerceDiscountContext struct {
	Version              int                        `json:"version"`
	BuyerID              uint                       `json:"buyer_id"`
	Operation            string                     `json:"operation"`
	PlanID               uint                       `json:"plan_id"`
	PlanSKUID            uint                       `json:"plan_sku_id"`
	TargetSubscriptionID uint                       `json:"target_subscription_id,omitempty"`
	PriceSourceOrderID   uint                       `json:"price_source_order_id,omitempty"`
	BasePriceMinor       int64                      `json:"base_price_minor"`
	Currency             string                     `json:"currency"`
	BillingUnit          string                     `json:"billing_unit"`
	BillingValue         int                        `json:"billing_value"`
	InputFingerprint     string                     `json:"input_fingerprint"`
	Selection            string                     `json:"selection,omitempty"`
	Agreement            *CommerceDiscountAgreement `json:"agreement,omitempty"`
}

type CommerceDiscountAgreement struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Scope     string `json:"scope"` // initial_only or recurring
	TermsHash string `json:"terms_hash"`
	TermsJSON string `json:"terms_json"`
}

type CommerceDiscountOffer struct {
	Status           string                     `json:"status"` // available, unavailable, requires_confirmation
	InputFingerprint string                     `json:"input_fingerprint"`
	Currency         string                     `json:"currency"`
	DiscountMinor    int64                      `json:"discount_minor"`
	Agreement        *CommerceDiscountAgreement `json:"agreement,omitempty"`
	ExpiresAt        time.Time                  `json:"expires_at"`
	Message          string                     `json:"message,omitempty"`
}

// CommerceDiscountTermsHash hashes compact UTF-8 JSON bytes, preserving number
// spelling and object-key order. TermsJSON is a string in the outer envelope so
// transport HTML escaping cannot alter the represented agreement bytes.
func CommerceDiscountTermsHash(terms string) (string, error) {
	raw := []byte(terms)
	if len(raw) == 0 || len(raw) > MaxCommerceQuoteTermsBytes || !utf8.Valid(raw) || !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", errors.New("invalid commerce discount terms")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func (r *CommerceQuoteRequest) DiscountContext() (CommerceDiscountContext, error) {
	var out CommerceDiscountContext
	if r == nil || r.Generation == 0 || r.ConfigRevision == 0 || len(r.ContextJson) == 0 || len(r.ContextJson) > MaxCommerceQuoteBytes || !json.Valid(r.ContextJson) {
		return out, errors.New("invalid host commerce quote context")
	}
	d := json.NewDecoder(bytes.NewReader(r.ContextJson))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, err
	}
	// Full business validation and scope/amount checks remain host-owned.
	if out.Version != 1 || out.BuyerID == 0 {
		return out, errors.New("invalid host commerce quote context")
	}
	return out, nil
}

func CommerceDiscountResult(offer CommerceDiscountOffer) (*CommerceQuoteResult, error) {
	raw, err := json.Marshal(offer)
	if err != nil || len(raw) > MaxCommerceQuoteBytes {
		return nil, errors.New("invalid commerce discount offer encoding")
	}
	return &CommerceQuoteResult{OfferJson: raw}, nil
}

// CommerceQuoteClient retrieves the independent optional RPC from the normal
// native SDK client. Existing PluginControlClient consumers remain compatible.
// The assertion describes the client transport, not server availability or a
// grant; the host must separately check current signed-manifest admission.
func CommerceQuoteClient(client PluginControlClient) (CommerceQuoteProviderClient, bool) {
	quote, ok := client.(CommerceQuoteProviderClient)
	return quote, ok
}
