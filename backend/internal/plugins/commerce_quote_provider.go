package plugins

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const CommerceQuoteProviderCapability = "zboard.commerce.quote.provider.v1"
const CommerceQuoteTimeout = 3 * time.Second

func validateCommerceQuoteInstallation(v Installation) error {
	publicKey, keyErr := base64.StdEncoding.DecodeString(v.SigningKey)
	if !hasCapability(v, CommerceQuoteProviderCapability) || v.Manifest.Components.Server == nil ||
		!v.Enabled || v.State != "active" || v.Generation == 0 || v.ConfigRevision == 0 ||
		keyErr != nil || len(publicKey) != ed25519.PublicKeySize || !digestPattern.MatchString(v.VersionID) {
		return ErrUnavailable
	}
	return nil
}

func sameCommerceQuoteInstallation(before, after Installation) error {
	if err := validateCommerceQuoteInstallation(after); err != nil {
		return err
	}
	if before.ID != after.ID || before.Generation != after.Generation || before.ConfigRevision != after.ConfigRevision || before.VersionID != after.VersionID || before.SigningKey != after.SigningKey || before.Publisher != after.Publisher {
		return ErrConflict
	}
	return nil
}

// Caller holds mu. Selection is the exact plugin installation ID, never the
// first discovered provider or a collection whose discounts are implicitly stacked.
func (m *Manager) commerceQuoteRuntime(ctx context.Context, id string) (Installation, *process, error) {
	if err := m.guard(m.db.WithContext(ctx)); err != nil {
		return Installation{}, nil, err
	}
	v, err := m.load(id)
	if err != nil {
		return Installation{}, nil, ErrUnavailable
	}
	if err := validateCommerceQuoteInstallation(v); err != nil {
		return Installation{}, nil, err
	}
	p := m.processes[id]
	if p == nil || p.client == nil || p.client.Exited() {
		return Installation{}, nil, ErrUnavailable
	}
	return v, p, nil
}

// EvaluateCommerceDiscount is host-to-native read-only RPC. It does not create
// orders, consume coupons, reserve balances or issue payment-fact credentials.
// Core commerce must call outside its financial transaction and revalidate the
// host-owned pricing context before committing any later order.
func (m *Manager) EvaluateCommerceDiscount(ctx context.Context, id string, in commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	if m == nil || !idPattern.MatchString(id) {
		return commerce.DiscountQuote{}, ErrUnavailable
	}
	if err := commerce.ValidateDiscountQuoteContext(in); err != nil {
		return commerce.DiscountQuote{}, err
	}
	m.mu.Lock()
	before, process, err := m.commerceQuoteRuntime(ctx, id)
	if err != nil {
		m.mu.Unlock()
		return commerce.DiscountQuote{}, err
	}
	client, supported := pluginv1.CommerceQuoteClient(process.api)
	if !supported {
		m.mu.Unlock()
		return commerce.DiscountQuote{}, ErrUnavailable
	}
	// A per-provider limit covers all callers, not an unbounded account-key map.
	if err := m.admitPluginCall("commerce-quote:"+id, 60); err != nil {
		m.mu.Unlock()
		return commerce.DiscountQuote{}, err
	}
	m.mu.Unlock()
	return callCommerceQuoteProvider(ctx, before, client, in, func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		after, current, err := m.commerceQuoteRuntime(ctx, id)
		if err != nil {
			return err
		}
		if current != process {
			return ErrConflict
		}
		return sameCommerceQuoteInstallation(before, after)
	})
}

func callCommerceQuoteProvider(ctx context.Context, before Installation, client pluginv1.CommerceQuoteProviderClient, in commerce.DiscountQuoteContext, checkCurrent func() error) (commerce.DiscountQuote, error) {
	if err := validateCommerceQuoteInstallation(before); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if err := commerce.ValidateDiscountQuoteContext(in); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if client == nil || checkCurrent == nil {
		return commerce.DiscountQuote{}, ErrUnavailable
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteInvalid
	}
	callCtx, cancel := context.WithTimeout(ctx, CommerceQuoteTimeout)
	defer cancel()
	result, callErr := client.EvaluateCommerceQuote(callCtx, &pluginv1.CommerceQuoteRequest{ContextJson: raw, Generation: before.Generation, ConfigRevision: before.ConfigRevision})
	if callCtx.Err() != nil {
		return commerce.DiscountQuote{}, callCtx.Err()
	}
	// gRPC can observe its deadline before the local context timer goroutine
	// runs. Preserve that terminal result instead of reporting an unavailable
	// provider (or accepting a concurrently returned result).
	switch status.Code(callErr) {
	case codes.DeadlineExceeded:
		return commerce.DiscountQuote{}, context.DeadlineExceeded
	case codes.Canceled:
		return commerce.DiscountQuote{}, context.Canceled
	}
	// Disabled/replaced/reconfigured/revoked results cannot cross the host boundary.
	if err := checkCurrent(); err != nil {
		return commerce.DiscountQuote{}, err
	}
	if callErr != nil || result == nil {
		return commerce.DiscountQuote{}, ErrUnavailable
	}
	if len(result.OfferJson) == 0 || len(result.OfferJson) > commerce.MaxDiscountQuoteBytes {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteInvalid
	}
	var offer commerce.DiscountOffer
	if err := DecodeStrict(result.OfferJson, &offer); err != nil {
		return commerce.DiscountQuote{}, commerce.ErrDiscountQuoteInvalid
	}
	if err := commerce.ValidateDiscountOffer(in, offer, time.Now().UTC()); err != nil {
		return commerce.DiscountQuote{}, err
	}
	return commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: before.ID, SigningKeyFingerprint: keyFingerprint(before.SigningKey), PackageDigest: before.VersionID, Generation: before.Generation, ConfigRevision: before.ConfigRevision}, Offer: offer}, nil
}
