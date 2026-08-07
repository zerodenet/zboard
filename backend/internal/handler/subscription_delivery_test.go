package handler

import "testing"

func TestResolveSubscriptionDelivery(t *testing.T) {
	tests := []struct {
		name              string
		requestedTemplate string
		userAgent         string
		wantTemplate      string
		wantFormat        string
		wantUsesUA        bool
	}{
		{
			name:         "znet sink client uses zero format",
			userAgent:    "ZNet-Sink/0.3.0",
			wantTemplate: "znet-sink",
			wantFormat:   "zero",
			wantUsesUA:   true,
		},
		{
			name:              "canonical zero uses legacy built in template",
			requestedTemplate: "zero",
			wantTemplate:      "znet-sink",
			wantFormat:        "zero",
		},
		{
			name:              "legacy plaintext name cannot request plaintext",
			requestedTemplate: "zero-json",
			wantTemplate:      "znet-sink",
			wantFormat:        "zero",
		},
		{
			name:              "legacy base64 name is canonicalized",
			requestedTemplate: "zero-base64-json",
			wantTemplate:      "znet-sink",
			wantFormat:        "zero",
		},
		{
			name:         "znet sink automatic clash import",
			userAgent:    "Clash.Meta",
			wantTemplate: "clash",
			wantFormat:   "clash",
			wantUsesUA:   true,
		},
		{
			name:         "mihomo client",
			userAgent:    "mihomo/1.19.0",
			wantTemplate: "clash",
			wantFormat:   "clash",
			wantUsesUA:   true,
		},
		{
			name:         "sing box client",
			userAgent:    "sing-box/1.12.0",
			wantTemplate: "sing-box",
			wantFormat:   "sing-box",
			wantUsesUA:   true,
		},
		{
			name:       "unknown client retains internal native fallback",
			userAgent:  "curl/8.14.1",
			wantFormat: "native",
			wantUsesUA: true,
		},
		{
			name:              "explicit template wins over user agent",
			requestedTemplate: "sing-box",
			userAgent:         "Clash.Meta",
			wantTemplate:      "sing-box",
			wantFormat:        "sing-box",
		},
		{
			name:              "explicit native wins over user agent",
			requestedTemplate: "native",
			userAgent:         "ZNet-Sink/0.3.0",
			wantFormat:        "native",
		},
		{
			name:              "explicit auto uses user agent",
			requestedTemplate: "auto",
			userAgent:         "Mihomo/1.19.0",
			wantTemplate:      "clash",
			wantFormat:        "clash",
			wantUsesUA:        true,
		},
		{
			name:              "custom template remains available",
			requestedTemplate: "operator-export",
			userAgent:         "Mihomo/1.19.0",
			wantTemplate:      "operator-export",
			wantFormat:        "operator-export",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolveSubscriptionDelivery(test.requestedTemplate, test.userAgent)
			if got.TemplateSlug != test.wantTemplate || got.Format != test.wantFormat || got.UsesUserAgent != test.wantUsesUA {
				t.Fatalf("resolveSubscriptionDelivery(%q, %q) = %#v, want template=%q format=%q usesUA=%t",
					test.requestedTemplate, test.userAgent, got, test.wantTemplate, test.wantFormat, test.wantUsesUA)
			}
		})
	}
}

func TestSubscriptionClientUserAgentRecognition(t *testing.T) {
	accepted := []string{
		"ZNet-Sink/0.3.0",
		"ZNet-Sink/0.0.16-rc.4",
		"ZNet-Sink/v0.1.0+build.7",
		"Clash.Meta",
		"Mihomo/1.19.0",
		"sing-box/1.12.0",
	}
	for _, userAgent := range accepted {
		if !isSubscriptionClientUserAgent(userAgent) {
			t.Fatalf("isSubscriptionClientUserAgent(%q) = false, want true", userAgent)
		}
	}

	rejected := []string{
		"",
		"curl/8.14.1",
		"Mozilla/5.0",
		"ZNet-Sink",
		"ZNet-Sink/",
		"znet-sink/0.3.0",
		"Mozilla/5.0 ZNet-Sink/0.3.0",
		"znetsink/0.3.0",
		"ZNet-Sink/0.3.0 extra",
	}
	for _, userAgent := range rejected {
		if isSubscriptionClientUserAgent(userAgent) {
			t.Fatalf("isSubscriptionClientUserAgent(%q) = true, want false", userAgent)
		}
	}
}

func TestCanonicalSubscriptionFormat(t *testing.T) {
	for _, alias := range []string{"zero", "znet-sink", "zero-json", "zero-base64-json", "base64-json", "znet-sink-base64"} {
		if got := canonicalSubscriptionFormat(alias); got != "zero" {
			t.Fatalf("canonicalSubscriptionFormat(%q) = %q, want zero", alias, got)
		}
	}
}

func TestZeroSubscriptionAliasesAreRecognized(t *testing.T) {
	for _, alias := range []string{
		"zero",
		"znet-sink",
		"znet_sink",
		"znetsink",
		"zero-json",
		"zero-base64-json",
		"base64-json",
		"znet-sink-base64",
	} {
		if !isZeroSubscriptionAlias(alias) {
			t.Fatalf("isZeroSubscriptionAlias(%q) = false, want true", alias)
		}
	}
}
