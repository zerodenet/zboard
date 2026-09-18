package handler

import (
	"context"
	"fmt"
)

const subscriptionClientTemplateSeedAction = "subscription_template.seed_client_defaults.v1"

// SeedSubscriptionClientTemplateDefaults adds the built-in client templates
// once. The audit record is the durable seed marker, so later administrator
// edits or deletions remain authoritative across process restarts.
func (h *handlers) SeedSubscriptionClientTemplateDefaults() error {
	definitions, err := subscriptionClientTemplateDefinitions()
	if err != nil {
		return err
	}
	if err := h.services.SubscriptionTemplates.SeedClients(context.Background(), definitions, subscriptionClientTemplateSeedAction, "seeded built-in Shadowrocket, Quantumult X and v2rayN subscription templates"); err != nil {
		return fmt.Errorf("persist subscription client template seed marker: %w", err)
	}
	return nil
}
