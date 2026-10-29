package plugins

import (
	"context"
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const CommerceDiscountRedemptionCapability = "zboard.commerce.discount.redemption.v1"

// RedeemDiscount is host-only native RPC, never a page OperationCapability. The
// caller owns the persisted key and queries before every unknown mutation. All
// provider calls occur outside financial transactions. There is no payment grant.
func (m *Manager) RedeemDiscount(ctx context.Context, in commerce.DiscountRedemptionRequest) (commerce.DiscountRedemptionResult, error) {
	var empty commerce.DiscountRedemptionResult
	if m == nil || commerce.ValidateDiscountRedemptionRequest(in) != nil {
		return empty, commerce.ErrDiscountRedemptionConflict
	}
	id := in.Binding.Provider.PluginID
	if !idPattern.MatchString(id) {
		return empty, ErrUnavailable
	}
	m.mu.Lock()
	before, process, err := m.commerceQuoteRuntime(ctx, id)
	if err != nil {
		m.mu.Unlock()
		return empty, err
	}
	if !hasCapability(before, CommerceDiscountRedemptionCapability) {
		m.mu.Unlock()
		return empty, commerce.ErrDiscountRedemptionUnsupported
	}
	expected := commerce.DiscountProviderIdentity{PluginID: before.ID, SigningKeyFingerprint: keyFingerprint(before.SigningKey), PackageDigest: before.VersionID, Generation: before.Generation, ConfigRevision: before.ConfigRevision}
	// New reservations require the exact quoted runtime. Recovery follows the
	// original key across restarts/reconfiguration under the same admitted signer;
	// forcing the old generation would strand every hold after a process restart.
	if (in.Action == "reserve" && in.Binding.Provider != expected) || in.Binding.Provider.SigningKeyFingerprint != expected.SigningKeyFingerprint {
		m.mu.Unlock()
		return empty, ErrConflict
	}
	client, ok := process.api.(pluginv1.CommerceRedemptionProviderClient)
	if !ok {
		m.mu.Unlock()
		return empty, commerce.ErrDiscountRedemptionUnsupported
	}
	if err := m.admitPluginCall("commerce-redemption:"+id, 60); err != nil {
		m.mu.Unlock()
		return empty, err
	}
	m.mu.Unlock()
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > commerce.MaxDiscountRedemptionBytes {
		return empty, commerce.ErrDiscountRedemptionConflict
	}
	callCtx, cancel := context.WithTimeout(ctx, CommerceQuoteTimeout)
	defer cancel()
	out, callErr := client.ExecuteCommerceRedemption(callCtx, &pluginv1.CommerceQuoteRequest{ContextJson: raw, Generation: before.Generation, ConfigRevision: before.ConfigRevision})
	if callCtx.Err() != nil {
		return empty, callCtx.Err()
	}
	if status.Code(callErr) == codes.DeadlineExceeded {
		return empty, context.DeadlineExceeded
	}
	if status.Code(callErr) == codes.Canceled {
		return empty, context.Canceled
	}
	m.mu.Lock()
	after, current, fenceErr := m.commerceQuoteRuntime(ctx, id)
	if fenceErr == nil && (current != process || !hasCapability(after, CommerceDiscountRedemptionCapability)) {
		fenceErr = ErrConflict
	}
	if fenceErr == nil {
		fenceErr = sameCommerceQuoteInstallation(before, after)
	}
	m.mu.Unlock()
	if fenceErr != nil {
		return empty, fenceErr
	}
	if status.Code(callErr) == codes.Unimplemented {
		return empty, commerce.ErrDiscountRedemptionUnsupported
	}
	if callErr != nil || out == nil {
		return empty, ErrUnavailable
	}
	if len(out.OfferJson) == 0 || len(out.OfferJson) > commerce.MaxDiscountRedemptionBytes {
		return empty, commerce.ErrDiscountRedemptionConflict
	}
	var result commerce.DiscountRedemptionResult
	if DecodeStrict(out.OfferJson, &result) != nil {
		return empty, commerce.ErrDiscountRedemptionConflict
	}
	if err := commerce.ValidateDiscountRedemptionResult(in, result); err != nil {
		return empty, err
	}
	return result, nil
}
