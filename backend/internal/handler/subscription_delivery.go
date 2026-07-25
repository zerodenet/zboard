package handler

import "strings"

const (
	subscriptionDeliveryAuto   = "auto"
	subscriptionDeliveryNative = "native"
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
		return subscriptionDeliverySelection{
			TemplateSlug: requestedTemplate,
			Format:       requestedTemplate,
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
		Format:        templateSlug,
		UsesUserAgent: true,
	}
}

func detectSubscriptionTemplate(userAgent string) string {
	normalized := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case containsAny(normalized, "znet-sink", "znet_sink", "znetsink"):
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
