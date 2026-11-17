package entitlements

import "testing"

func TestLegacyMutationFingerprintKeepsPublishedIdentity(t *testing.T) {
	in := SubscriptionMutationInput{SubscriptionID: 55, Kind: SubscriptionCancel, Reason: "support cancellation", Origin: "plugin:example.operator", IdempotencyKey: "transport-key-one"}
	const expected = "d2a560c32ebd6a05388e2d1f9b1b064d58ade50f0346621bd80d5c02d9e28c2e"
	if got := SubscriptionMutationFingerprint(in); got != expected {
		t.Fatalf("published identity changed: %s", got)
	}
	in.IdempotencyKey = "transport-key-two"
	if got := SubscriptionMutationFingerprint(in); got != expected {
		t.Fatal("transport identity entered the legacy command hash")
	}
	in.Reason = "different reason"
	if got := SubscriptionMutationFingerprint(in); got == expected {
		t.Fatal("changed command reused historical fingerprint")
	}
}
