package commerce

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func discountQuoteFixture(t *testing.T) (DiscountQuoteContext, DiscountOffer) {
	t.Helper()
	terms := `{"discount_minor":2000,"note":"<first> & exact 9007199254740993"}`
	hash, err := DiscountTermsHash(terms)
	if err != nil {
		t.Fatal(err)
	}
	in := DiscountQuoteContext{Version: 1, BuyerID: 7, Operation: "new", PlanID: 1, PlanSKUID: 2, BasePriceMinor: 12000, Currency: "CNY", BillingUnit: "month", BillingValue: 1, InputFingerprint: strings.Repeat("a", 64)}
	out := DiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: time.Now().Add(time.Minute), Agreement: &DiscountAgreement{ID: "first-20", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}}
	return in, out
}

func TestDiscountQuoteValidatesOnlyReductionAndBoundEvidence(t *testing.T) {
	in, out := discountQuoteFixture(t)
	if err := ValidateDiscountOffer(in, out, time.Now()); err != nil || in.BasePriceMinor-out.DiscountMinor != 10000 {
		t.Fatal("120 base minus 20 offer", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*DiscountQuoteContext, *DiscountOffer)
	}{
		{"negative", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.DiscountMinor = -1 }},
		{"exceeds-base", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.DiscountMinor = 12001 }},
		{"currency", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Currency = "USD" }},
		{"fingerprint", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.InputFingerprint = strings.Repeat("b", 64) }},
		{"scope", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Agreement.Scope = "all_accounts" }},
		{"expired", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.ExpiresAt = time.Now().Add(-time.Second) }},
		{"terms-hash", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Agreement.TermsHash = strings.Repeat("0", 64) }},
		{"terms-changed", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Agreement.TermsJSON = `{"discount_minor":1}` }},
		{"terms-null", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Agreement.TermsJSON = "null" }},
		{"unknown-status", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Status = "paid" }},
		{"first-only-renewal", func(i *DiscountQuoteContext, _ *DiscountOffer) { i.Operation = "renewal"; i.TargetSubscriptionID = 9 }},
		{"topup", func(i *DiscountQuoteContext, _ *DiscountOffer) { i.Operation = "topup" }},
		{"unavailable-discount", func(_ *DiscountQuoteContext, o *DiscountOffer) { o.Status = "unavailable"; o.Message = "offline" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, out := discountQuoteFixture(t)
			tc.change(&in, &out)
			if !errors.Is(ValidateDiscountOffer(in, out, time.Now()), ErrDiscountQuoteInvalid) {
				t.Fatal("invalid offer accepted")
			}
		})
	}
}

func TestDiscountQuoteRecurringTermsRequireExplicitConfirmationWhenChanged(t *testing.T) {
	in, out := discountQuoteFixture(t)
	in.Operation, in.TargetSubscriptionID, out.Agreement.Scope = "renewal", 9, "recurring"
	old := *out.Agreement
	in.Agreement = &old
	if err := ValidateDiscountOffer(in, out, time.Now()); err != nil {
		t.Fatal("unchanged recurring offer", err)
	}
	out.Agreement.Version = "2"
	if err := ValidateDiscountOffer(in, out, time.Now()); err == nil {
		t.Fatal("changed terms became silently available")
	}
	out.Status = "requires_confirmation"
	if err := ValidateDiscountOffer(in, out, time.Now()); err != nil {
		t.Fatal("explicit confirmation state rejected", err)
	}
	out.Status, out.DiscountMinor, out.Agreement, out.Message = "unavailable", 0, nil, "provider cannot confirm existing agreement"
	if err := ValidateDiscountOffer(in, out, time.Now()); err != nil || out.Status != "unavailable" {
		t.Fatal("unavailable must remain distinguishable from a zero discount", err)
	}
}

func TestDiscountQuoteTermsPreservePrecisionAndOnlyIgnoreWhitespace(t *testing.T) {
	a, err := DiscountTermsHash(` { "n":9007199254740993, "text":"<x>&" } `)
	b, err2 := DiscountTermsHash(`{"n":9007199254740993,"text":"<x>&"}`)
	c, err3 := DiscountTermsHash(`{"n":9007199254740992,"text":"<x>&"}`)
	if err != nil || err2 != nil || err3 != nil || a != b || a == c {
		t.Fatal(a, b, c, err, err2, err3)
	}
	if _, err := DiscountTermsHash(strings.Repeat(" ", MaxDiscountTermsBytes+1)); err == nil {
		t.Fatal("unbounded terms accepted")
	}
}
