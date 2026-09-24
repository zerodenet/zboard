package handler

import (
	"context"
	"fmt"
)

// A plugin receives the saved template for its client format, including the
// operator's policy groups and rule sets. Missing or inactive templates fail
// instead of silently replacing that configuration with renderer defaults.
func (h *handlers) renderPluginSubscriptionTemplate(ctx context.Context, format string, data subscriptionTemplateData) (string, string, error) {
	source, err := h.services.SubscriptionTemplates.RenderSource(ctx, format)
	if err != nil {
		return "", "", err
	}
	item := subscriptionTemplateModel(source.Template)
	renderer := normalizeSubscriptionRenderer(item.Renderer)
	if renderer != format {
		return "", "", fmt.Errorf("subscription template %q uses renderer %q, expected %q", item.Slug, renderer, format)
	}
	data.SiteName = source.SiteName
	content, contentType, err := h.renderSubscriptionWithStoredRuleSets(ctx, renderer, item.Customization, data, false)
	if err != nil {
		return "", "", err
	}
	if err := h.validateZeroSubscriptionPreview(ctx, renderer, content); err != nil {
		return "", "", err
	}
	delivered, _, deliveryFormat := encodeSubscriptionTemplateDelivery(renderer, content, contentType)
	return delivered, deliveryFormat, nil
}
