package commerce

import (
	"strings"
	"testing"
)

func redemptionBindingFixture() DiscountRedemptionBinding {
	return DiscountRedemptionBinding{Version: 1, Key: strings.Repeat("a", 64), OrderID: 4, BuyerID: 5, InputFingerprint: strings.Repeat("b", 64), Provider: DiscountProviderIdentity{PluginID: "example.quote", SigningKeyFingerprint: strings.Repeat("c", 64), PackageDigest: strings.Repeat("d", 64), Generation: 1, ConfigRevision: 1}, AgreementID: "finite-first", AgreementVersion: "1", TermsHash: strings.Repeat("e", 64), BaseMinor: 12000, DiscountMinor: 2000, PayableMinor: 10000, Currency: "CNY"}
}

func TestDiscountRedemptionBindingSeparatesPriceAndOrderIdentity(t *testing.T) {
	b := redemptionBindingFixture()
	hash, err := DiscountRedemptionHash(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*DiscountRedemptionBinding){
		func(v *DiscountRedemptionBinding) { v.OrderID++ }, func(v *DiscountRedemptionBinding) { v.BuyerID++ }, func(v *DiscountRedemptionBinding) { v.DiscountMinor--; v.PayableMinor++ }, func(v *DiscountRedemptionBinding) { v.Provider.Generation++ }, func(v *DiscountRedemptionBinding) { v.TermsHash = strings.Repeat("f", 64) },
	} {
		altered := b
		change(&altered)
		if next, err := DiscountRedemptionHash(altered); err != nil || next == hash {
			t.Fatalf("binding change not distinguished: %s %v", next, err)
		}
	}
	request := DiscountRedemptionRequest{Version: 1, Action: "query", Binding: b, BindingHash: hash}
	good := DiscountRedemptionResult{Version: 1, Key: b.Key, BindingHash: hash, State: "reserved", Guarantee: DiscountRedemptionGuarantee, Token: "exclusive-hold"}
	if err := ValidateDiscountRedemptionResult(request, good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*DiscountRedemptionResult){
		func(v *DiscountRedemptionResult) { v.Guarantee = "expires_after_5m" }, func(v *DiscountRedemptionResult) { v.Key = strings.Repeat("f", 64) }, func(v *DiscountRedemptionResult) { v.BindingHash = strings.Repeat("f", 64) }, func(v *DiscountRedemptionResult) { v.Token = "" }, func(v *DiscountRedemptionResult) { v.State = "paid" },
	} {
		altered := good
		change(&altered)
		if ValidateDiscountRedemptionResult(request, altered) == nil {
			t.Fatalf("accepted changed evidence: %+v", altered)
		}
	}
	request.Action = "commit"
	if ValidateDiscountRedemptionRequest(request) == nil {
		t.Fatal("commit without known token")
	}
	request.Token = good.Token
	good.Token = "other-hold"
	if ValidateDiscountRedemptionResult(request, good) == nil {
		t.Fatal("token swap")
	}
}

func TestDiscountRedemptionStateMachineNeverReleasesPaidOrReservesUnknown(t *testing.T) {
	cases := []struct {
		desired, observed, action string
		valid                     bool
	}{
		{"reserve", "absent", "reserve", true}, {"reserve", "reserved", "", true}, {"reserve", "unknown", "", false}, {"reserve", "released", "", false},
		{"commit", "reserved", "commit", true}, {"commit", "committed", "", true}, {"commit", "absent", "", false}, {"commit", "released", "", false},
		{"release", "absent", "release", true}, {"release", "reserved", "release", true}, {"release", "rejected", "release", true}, {"release", "released", "", true}, {"release", "committed", "", false},
	}
	for _, c := range cases {
		action, err := NextDiscountRedemptionAction(c.desired, c.observed)
		if (err == nil) != c.valid || action != c.action {
			t.Fatalf("%s/%s: %s %v", c.desired, c.observed, action, err)
		}
	}
}
