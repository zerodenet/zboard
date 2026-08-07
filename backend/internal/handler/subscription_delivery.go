package handler

import "strings"

const (
	subscriptionDeliveryAuto   = "auto"
	subscriptionDeliveryNative = "native"
	subscriptionDeliveryZero   = "zero"
)

type subscriptionDeliverySelection struct {
	TemplateSlug  string
	Format        string
	UsesUserAgent bool
}

func resolveSubscriptionDelivery(requestedTemplate, userAgent string) subscriptionDeliverySelection {
	requestedTemplate = strings.TrimSpace(requestedTemplate)
	if requestedTemplate != "" && !strings.EqualFold(requestedTemplate, subscriptionDeliveryAuto) {
		if strings.EqualFold(requestedTemplate, subscriptionDeliveryNative) {
			return subscriptionDeliverySelection{Format: subscriptionDeliveryNative}
		}
		if isZeroSubscriptionAlias(requestedTemplate) {
			return subscriptionDeliverySelection{
				TemplateSlug: "znet-sink",
				Format:       subscriptionDeliveryZero,
			}
		}
		return subscriptionDeliverySelection{
			TemplateSlug: requestedTemplate,
			Format:       canonicalSubscriptionFormat(requestedTemplate),
		}
	}

	templateSlug := detectSubscriptionTemplate(userAgent)
	if templateSlug == "" {
		return subscriptionDeliverySelection{
			Format:        subscriptionDeliveryNative,
			UsesUserAgent: true,
		}
	}
	return subscriptionDeliverySelection{
		TemplateSlug:  templateSlug,
		Format:        canonicalSubscriptionFormat(templateSlug),
		UsesUserAgent: true,
	}
}

func canonicalSubscriptionFormat(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case isZeroSubscriptionAlias(normalized):
		return subscriptionDeliveryZero
	case normalized == "clash-yaml":
		return "clash"
	case normalized == "singbox":
		return "sing-box"
	default:
		return normalized
	}
}

func isZeroSubscriptionAlias(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zero", "znet-sink", "znet_sink", "znetsink", "zero-json", "zero-base64-json", "base64-json", "znet-sink-base64":
		return true
	default:
		return false
	}
}

func detectSubscriptionTemplate(userAgent string) string {
	normalized := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case containsAny(normalized, "znet-sink", "znet_sink", "znetsink"):
		// Keep the existing built-in template slug for stored installations;
		// the externally visible format is canonicalized to "zero".
		return "znet-sink"
	case containsAny(normalized, "sing-box", "singbox"):
		return "sing-box"
	case containsAny(normalized, "clash", "mihomo"):
		return "clash"
	default:
		return ""
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
