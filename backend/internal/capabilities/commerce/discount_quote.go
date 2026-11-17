package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrDiscountQuoteUnavailable = errors.New("discount quote unavailable")
var ErrDiscountQuoteInvalid = errors.New("invalid discount quote")

const MaxDiscountQuoteBytes = 16 << 10
const MaxDiscountTermsBytes = 8 << 10

var discountHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// DiscountQuoteContext is constructed by core commerce, never accepted as an
// authoritative browser/plugin-page payload. Evaluation has no redemption side
// effects and conveys no payment or entitlement authority.
type DiscountQuoteContext struct {
	Version              int                `json:"version"`
	BuyerID              uint               `json:"buyer_id"`
	Operation            string             `json:"operation"`
	PlanID               uint               `json:"plan_id"`
	PlanSKUID            uint               `json:"plan_sku_id"`
	TargetSubscriptionID uint               `json:"target_subscription_id,omitempty"`
	PriceSourceOrderID   uint               `json:"price_source_order_id,omitempty"`
	BasePriceMinor       int64              `json:"base_price_minor"`
	Currency             string             `json:"currency"`
	BillingUnit          string             `json:"billing_unit"`
	BillingValue         int                `json:"billing_value"`
	InputFingerprint     string             `json:"input_fingerprint"`
	Selection            string             `json:"selection,omitempty"`
	Agreement            *DiscountAgreement `json:"agreement,omitempty"`
}

type DiscountAgreement struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Scope     string `json:"scope"`
	TermsHash string `json:"terms_hash"`
	TermsJSON string `json:"terms_json"`
}

type DiscountOffer struct {
	// Optional core-owned admission obligation, never opaque marketing terms.
	// An old strict snapshot reader rejects this field instead of losing the hold.
	Redemption       string             `json:"redemption,omitempty"`
	Status           string             `json:"status"` // available, unavailable, requires_confirmation
	InputFingerprint string             `json:"input_fingerprint"`
	Currency         string             `json:"currency"`
	DiscountMinor    int64              `json:"discount_minor"`
	Agreement        *DiscountAgreement `json:"agreement,omitempty"`
	ExpiresAt        time.Time          `json:"expires_at"`
	Message          string             `json:"message,omitempty"`
}

// Provider identity is supplied by the admitted native host, never the offer.
type DiscountProviderIdentity struct {
	PluginID              string `json:"plugin_id"`
	SigningKeyFingerprint string `json:"signing_key_fingerprint"`
	PackageDigest         string `json:"package_digest"`
	Generation            uint64 `json:"generation"`
	ConfigRevision        uint64 `json:"config_revision"`
}

type DiscountQuote struct {
	Provider DiscountProviderIdentity `json:"provider"`
	Offer    DiscountOffer            `json:"offer"`
}

func ValidateDiscountQuote(in DiscountQuoteContext, out DiscountQuote, now time.Time) error {
	provider := out.Provider
	if provider.PluginID == "" || len(provider.PluginID) > 160 || !discountHashPattern.MatchString(provider.SigningKeyFingerprint) ||
		!discountHashPattern.MatchString(provider.PackageDigest) || provider.Generation == 0 || provider.ConfigRevision == 0 {
		return ErrDiscountQuoteInvalid
	}
	return ValidateDiscountOffer(in, out.Offer, now)
}

// The caller explicitly selects exactly one provider. This interface neither
// stacks discounts nor reserves/consumes limited-use coupons.
type DiscountQuoter interface {
	Quote(context.Context, string, DiscountQuoteContext) (DiscountQuote, error)
}

// DiscountTermsHash hashes compact JSON bytes without interpreting plugin-owned
// rules. Object-key order is significant; providers must retain the exact terms
// representation for an agreement, not silently rewrite its rule identity.
func DiscountTermsHash(terms string) (string, error) {
	raw := []byte(terms)
	if len(raw) == 0 || len(raw) > MaxDiscountTermsBytes || !utf8.Valid(raw) || !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", ErrDiscountQuoteInvalid
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", ErrDiscountQuoteInvalid
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func validateDiscountAgreement(a *DiscountAgreement) error {
	if a == nil || strings.TrimSpace(a.ID) == "" || len(a.ID) > 128 || strings.TrimSpace(a.Version) == "" || len(a.Version) > 64 ||
		(a.Scope != "initial_only" && a.Scope != "recurring" && a.Scope != OperationDiscountScope) || !discountHashPattern.MatchString(a.TermsHash) {
		return ErrDiscountQuoteInvalid
	}
	hash, err := DiscountTermsHash(a.TermsJSON)
	if err != nil || hash != a.TermsHash {
		return ErrDiscountQuoteInvalid
	}
	return nil
}

func ValidateDiscountQuoteContext(in DiscountQuoteContext) error {
	if in.Version != 1 || in.BuyerID == 0 || in.PlanID == 0 || in.PlanSKUID == 0 || in.BasePriceMinor < 0 ||
		len(in.Currency) != 3 || in.Currency != strings.ToUpper(in.Currency) ||
		!discountHashPattern.MatchString(in.InputFingerprint) || len(in.Selection) > 256 || in.BillingValue <= 0 {
		return ErrDiscountQuoteInvalid
	}
	for _, c := range in.Currency {
		if c < 'A' || c > 'Z' {
			return ErrDiscountQuoteInvalid
		}
	}
	switch in.Operation {
	case "new":
		if in.TargetSubscriptionID != 0 {
			return ErrDiscountQuoteInvalid
		}
	case "renewal", "upgrade", "traffic_reset", "traffic_pack":
		if in.TargetSubscriptionID == 0 {
			return ErrDiscountQuoteInvalid
		}
	default:
		return ErrDiscountQuoteInvalid
	}
	switch in.BillingUnit {
	case "day", "week", "month", "year", "once":
	default:
		return ErrDiscountQuoteInvalid
	}
	if in.Agreement != nil {
		if err := validateDiscountAgreement(in.Agreement); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > MaxDiscountQuoteBytes {
		return ErrDiscountQuoteInvalid
	}
	return nil
}

// This validates evidence only. A requires_confirmation result is not permission
// to charge, and an unavailable result cannot be converted to a zero discount.
func ValidateDiscountOffer(in DiscountQuoteContext, out DiscountOffer, now time.Time) error {
	if err := ValidateDiscountQuoteContext(in); err != nil {
		return err
	}
	if out.Redemption != "" && out.Redemption != DiscountRedemptionGuarantee {
		return ErrDiscountRedemptionUnsupported
	}
	if out.InputFingerprint != in.InputFingerprint || out.Currency != in.Currency || out.DiscountMinor < 0 || out.DiscountMinor > in.BasePriceMinor || len(out.Message) > 512 {
		return ErrDiscountQuoteInvalid
	}
	switch out.Status {
	case "available", "requires_confirmation":
		if out.ExpiresAt.IsZero() || !out.ExpiresAt.After(now) || validateDiscountAgreement(out.Agreement) != nil {
			return ErrDiscountQuoteInvalid
		}
		if out.Agreement.Scope == OperationDiscountScope && (in.Operation != "upgrade" && in.Operation != "traffic_reset" && in.Operation != "traffic_pack") {
			return ErrDiscountQuoteInvalid
		}
		if out.Agreement.Scope == "initial_only" && in.Operation != "new" {
			return ErrDiscountQuoteInvalid
		}
		if in.Agreement != nil && (in.Agreement.ID != out.Agreement.ID || in.Agreement.Version != out.Agreement.Version || in.Agreement.Scope != out.Agreement.Scope || in.Agreement.TermsHash != out.Agreement.TermsHash) && out.Status != "requires_confirmation" {
			return ErrDiscountQuoteInvalid
		}
	case "unavailable":
		if out.DiscountMinor != 0 || out.Agreement != nil || strings.TrimSpace(out.Message) == "" {
			return ErrDiscountQuoteInvalid
		}
	default:
		return ErrDiscountQuoteInvalid
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > MaxDiscountQuoteBytes {
		return ErrDiscountQuoteInvalid
	}
	return nil
}
