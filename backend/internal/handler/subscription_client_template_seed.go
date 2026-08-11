package handler

import (
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const subscriptionClientTemplateSeedAction = "subscription_template.seed_client_defaults.v1"

// SeedSubscriptionClientTemplateDefaults adds the built-in client templates
// once. The audit record is the durable seed marker, so later administrator
// edits or deletions remain authoritative across process restarts.
func (h *handlers) SeedSubscriptionClientTemplateDefaults() error {
	var seeded int64
	if err := h.db.Model(&model.AuditLog{}).
		Where("action = ?", subscriptionClientTemplateSeedAction).
		Count(&seeded).Error; err != nil {
		return fmt.Errorf("inspect subscription client template seed: %w", err)
	}
	if seeded > 0 {
		return nil
	}
	if err := h.ReconcileSubscriptionClientTemplateDefaults(); err != nil {
		return err
	}
	marker := model.AuditLog{
		Actor:  "system",
		Action: subscriptionClientTemplateSeedAction,
		Target: "subscription_templates",
		Detail: "seeded built-in Shadowrocket, Quantumult X and v2rayN subscription templates",
	}
	if err := h.db.Create(&marker).Error; err != nil {
		return fmt.Errorf("persist subscription client template seed marker: %w", err)
	}
	return nil
}
