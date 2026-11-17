package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const DiscountRedemptionCapability = "zboard.commerce.discount.redemption.v1"
const DiscountRedemptionGuarantee = "exclusive_until_core_finalization_v1"
const MaxDiscountRedemptionBytes = 32 << 10

var ErrDiscountRedemptionPending = errors.New("discount reservation is not yet confirmed")
var ErrDiscountRedemptionConflict = errors.New("discount reservation evidence conflicts")
var ErrDiscountRedemptionUnsupported = errors.New("provider lacks durable exclusive discount reservation capability")

// This immutable binding is constructed by core from an accepted order quote.
// The provider receives no authority to mutate payments, wallets or entitlements.
type DiscountRedemptionBinding struct {
	Version          int                      `json:"version"`
	Key              string                   `json:"key"`
	OrderID          uint                     `json:"order_id"`
	BuyerID          uint                     `json:"buyer_id"`
	InputFingerprint string                   `json:"input_fingerprint"`
	Provider         DiscountProviderIdentity `json:"provider"`
	AgreementID      string                   `json:"agreement_id"`
	AgreementVersion string                   `json:"agreement_version"`
	TermsHash        string                   `json:"terms_hash"`
	BaseMinor        int64                    `json:"base_minor"`
	DiscountMinor    int64                    `json:"discount_minor"`
	PayableMinor     int64                    `json:"payable_minor"`
	Currency         string                   `json:"currency"`
}

type DiscountRedemptionRequest struct {
	Version     int                       `json:"version"`
	Action      string                    `json:"action"` // query, reserve, commit, release
	Binding     DiscountRedemptionBinding `json:"binding"`
	BindingHash string                    `json:"binding_hash"`
	Token       string                    `json:"token,omitempty"`
}

type DiscountRedemptionResult struct {
	Version     int    `json:"version"`
	Key         string `json:"key"`
	BindingHash string `json:"binding_hash"`
	State       string `json:"state"` // absent, reserved, committed, released, rejected
	Guarantee   string `json:"guarantee"`
	Token       string `json:"token,omitempty"`
	Message     string `json:"message,omitempty"`
}

type DiscountRedemptionProvider interface {
	RedeemDiscount(context.Context, DiscountRedemptionRequest) (DiscountRedemptionResult, error)
}

func DiscountRedemptionHash(b DiscountRedemptionBinding) (string, error) {
	if b.Version != 1 || len(b.Key) != 64 || !discountHashPattern.MatchString(b.Key) || b.OrderID == 0 || b.BuyerID == 0 ||
		!discountHashPattern.MatchString(b.InputFingerprint) || !discountHashPattern.MatchString(b.TermsHash) ||
		b.Provider.PluginID == "" || len(b.Provider.PluginID) > 160 || !discountHashPattern.MatchString(b.Provider.SigningKeyFingerprint) ||
		!discountHashPattern.MatchString(b.Provider.PackageDigest) || b.Provider.Generation == 0 || b.Provider.ConfigRevision == 0 ||
		strings.TrimSpace(b.AgreementID) == "" || len(b.AgreementID) > 128 || strings.TrimSpace(b.AgreementVersion) == "" || len(b.AgreementVersion) > 64 ||
		b.BaseMinor < 0 || b.DiscountMinor < 0 || b.DiscountMinor > b.BaseMinor || b.PayableMinor != b.BaseMinor-b.DiscountMinor || len(b.Currency) != 3 {
		return "", ErrDiscountRedemptionConflict
	}
	for _, c := range b.Currency {
		if c < 'A' || c > 'Z' {
			return "", ErrDiscountRedemptionConflict
		}
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateDiscountRedemptionRequest(r DiscountRedemptionRequest) error {
	hash, err := DiscountRedemptionHash(r.Binding)
	if err != nil || r.Version != 1 || r.BindingHash != hash || len(r.Token) > 512 {
		return ErrDiscountRedemptionConflict
	}
	switch r.Action {
	case "query", "reserve", "release":
	case "commit":
		if r.Token == "" {
			return ErrDiscountRedemptionConflict
		}
	default:
		return ErrDiscountRedemptionConflict
	}
	return nil
}

func ValidateDiscountRedemptionResult(r DiscountRedemptionRequest, out DiscountRedemptionResult) error {
	if ValidateDiscountRedemptionRequest(r) != nil || out.Version != 1 || out.Key != r.Binding.Key || out.BindingHash != r.BindingHash ||
		len(out.Message) > 512 || len(out.Token) > 512 {
		return ErrDiscountRedemptionConflict
	}
	if out.Guarantee != DiscountRedemptionGuarantee {
		return ErrDiscountRedemptionUnsupported
	}
	switch out.State {
	case "reserved", "committed":
		if out.Token == "" || (r.Token != "" && r.Token != out.Token) {
			return ErrDiscountRedemptionConflict
		}
	case "absent", "released", "rejected":
		if out.Token != "" && r.Token != "" && out.Token != r.Token {
			return ErrDiscountRedemptionConflict
		}
	default:
		return ErrDiscountRedemptionConflict
	}
	switch r.Action {
	case "reserve":
		if out.State != "reserved" && out.State != "committed" && out.State != "released" && out.State != "rejected" {
			return ErrDiscountRedemptionConflict
		}
	case "commit":
		if out.State != "committed" && out.State != "reserved" {
			return ErrDiscountRedemptionConflict
		}
	case "release":
		if out.State != "released" && out.State != "committed" {
			return ErrDiscountRedemptionConflict
		}
	}
	return nil
}

// No clock can independently expire a confirmed reservation. Remote absent is
// only actionable after Query for this exact durable key. A provider retains a
// release tombstone so an older Reserve arriving afterwards cannot create a hold.
func NextDiscountRedemptionAction(desired, observed string) (string, error) {
	switch desired {
	case "reserve":
		switch observed {
		case "absent":
			return "reserve", nil
		case "reserved":
			return "", nil
		case "rejected", "released":
			return "", ErrDiscountRedemptionUnsupported
		}
	case "commit":
		switch observed {
		case "reserved":
			return "commit", nil
		case "committed":
			return "", nil
		}
	case "release":
		switch observed {
		case "absent", "reserved", "rejected":
			return "release", nil
		case "released":
			return "", nil
		}
	}
	return "", ErrDiscountRedemptionConflict
}
