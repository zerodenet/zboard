package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type nativeCommerceQuoteStub struct {
	calls int
	quote commerce.DiscountQuote
	err   error
}

func (s *nativeCommerceQuoteStub) EvaluateCommerceDiscount(_ context.Context, _ string, _ commerce.DiscountQuoteContext) (commerce.DiscountQuote, error) {
	s.calls++
	return s.quote, s.err
}

func TestCommerceDiscountAdapterPreservesHostContractAndUnavailableState(t *testing.T) {
	terms := `{"amount":2000,"large":9007199254740993,"text":"<&>"}`
	hash, _ := commerce.DiscountTermsHash(terms)
	sdkHash, _ := pluginv1.CommerceDiscountTermsHash(terms)
	if hash != sdkHash {
		t.Fatal("SDK and business terms identities differ", hash, sdkHash)
	}
	in := commerce.DiscountQuoteContext{Version: 1, BuyerID: 7, Operation: "new", PlanID: 1, PlanSKUID: 2, BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, InputFingerprint: strings.Repeat("a", 64)}
	stub := &nativeCommerceQuoteStub{quote: commerce.DiscountQuote{Provider: commerce.DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("b", 64), PackageDigest: strings.Repeat("c", 64), Generation: 2, ConfigRevision: 3}, Offer: commerce.DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: "CNY", DiscountMinor: 2000, ExpiresAt: time.Now().Add(time.Minute), Agreement: &commerce.DiscountAgreement{ID: "first", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}}}}
	adapter := CommerceDiscountProvider{Native: stub}
	out, err := adapter.Quote(context.Background(), "example.quote", in)
	if err != nil || out.Offer.DiscountMinor != 2000 || out.Provider.PluginID != "example.quote" {
		t.Fatal(out, err)
	}
	raw, _ := json.Marshal(out.Offer)
	var sdkOffer pluginv1.CommerceDiscountOffer
	if json.Unmarshal(raw, &sdkOffer) != nil || sdkOffer.Agreement.TermsJSON != terms || sdkOffer.Agreement.TermsHash != sdkHash {
		t.Fatal("wire representation changed exact terms", sdkOffer)
	}
	stub.quote.Offer.Status = "requires_confirmation"
	out, err = adapter.Quote(context.Background(), "example.quote", in)
	if err != nil || out.Offer.Status != "requires_confirmation" {
		t.Fatal("confirmation became charge approval", out, err)
	}
	stub.quote.Offer.Status, stub.quote.Offer.DiscountMinor, stub.quote.Offer.Agreement, stub.quote.Offer.Message = "unavailable", 0, nil, "cannot confirm prior recurring terms"
	out, err = adapter.Quote(context.Background(), "example.quote", in)
	if err != nil || out.Offer.Status != "unavailable" {
		t.Fatal("unknown offer became a zero-discount success", out, err)
	}
	stub.quote.Provider.PluginID = "other.quote"
	if _, err := adapter.Quote(context.Background(), "example.quote", in); !errors.Is(err, commerce.ErrDiscountQuoteInvalid) {
		t.Fatal("provider substitution accepted", err)
	}
	stub.err = plugins.ErrConflict
	if _, err := adapter.Quote(context.Background(), "example.quote", in); !errors.Is(err, commerce.ErrDiscountQuoteUnavailable) || !errors.Is(err, plugins.ErrConflict) {
		t.Fatal("lost stale-runtime explanation", err)
	}
	calls := stub.calls
	if _, err := adapter.Quote(context.Background(), "", in); !errors.Is(err, commerce.ErrDiscountQuoteUnavailable) || stub.calls != calls {
		t.Fatal("implicit provider selection", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Quote(ctx, "example.quote", in); !errors.Is(err, context.Canceled) || stub.calls != calls {
		t.Fatal("canceled request invoked provider", err)
	}
}
