package platform

import "testing"

func TestSubscriptionPrefixRequiresPrivateSitePath(t *testing.T) {
	for _, value := range []string{"", "/private-feed", "/自定义/路径/"} {
		if _, err := SubscriptionPrefix(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"/", "//evil.test/feed", "https://evil.test/feed", "/api/v1/client/subscription", "/ADMIN/nodes", "/private?x=1", "/private#x", "/a/../b", "/a//b", "/a%2fb", "/a\\b", "relative"} {
		if _, err := SubscriptionPrefix(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if got, err := SubscriptionDeliveryURL("/private-feed/", "token"); err != nil || got != "/private-feed/token" {
		t.Fatal(got, err)
	}
	if got, err := SubscriptionDeliveryURL("", "token"); err != nil || got != "" {
		t.Fatal("legacy fallback", got, err)
	}
}
