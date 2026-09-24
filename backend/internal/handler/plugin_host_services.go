package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

// SupportsPluginSubscriptionFormat is the delivery-adapter boundary used by
// application.PluginHostServices. Account ownership is resolved before this
// adapter is called; this adapter only renders an already-authorized summary
// with the same projection pipeline as ZBoard's public subscription delivery.
func (h *handlers) SupportsPluginSubscriptionFormat(value string) bool {
	_, ok := pluginSubscriptionFormat(value)
	return ok
}

func (h *handlers) ProjectPluginSubscription(ctx context.Context, summary entitlements.SubscriptionSummary, format string, now time.Time) (pluginv1.ProjectedSubscription, error) {
	format, ok := pluginSubscriptionFormat(format)
	if !ok {
		return pluginv1.ProjectedSubscription{}, entitlements.ErrAccessNotFound
	}
	subscription := model.Subscription{
		ID: summary.ID, UserID: summary.UserID, PlanID: summary.PlanID, PlanSKUID: summary.PlanSKUID,
		NodeGroupID: summary.NodeGroupID, SubscriptionType: summary.SubscriptionType,
		StartAt: summary.StartAt, EndAt: summary.EndAt, Status: summary.Status,
		FlowTotal: summary.FlowTotal, FlowUsed: summary.FlowUsed, SpeedLimitMbps: summary.SpeedLimitMbps,
		DeviceLimit: summary.DeviceLimit, FamilyLimit: summary.FamilyLimit, RenewalPriceMinor: summary.RenewalPriceMinor,
		ResetPolicy: summary.ResetPolicy, NextResetAt: summary.NextResetAt, TrafficCalcMode: summary.TrafficCalcMode,
		CreatedAt: summary.CreatedAt, UpdatedAt: summary.UpdatedAt,
	}
	if err := h.ensureCredentialsForSubscriptions([]model.Subscription{subscription}); err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	nodes, err := h.buildProjectedSubscriptionManifestNodes(ctx, []model.Subscription{subscription}, subscriptionProjectionFilter{}, now)
	if err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	if err := h.sortSubscriptionManifestNodes([]model.Subscription{subscription}, nodes); err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	remaining := subscription.FlowTotal - subscription.FlowUsed
	if remaining < 0 {
		remaining = 0
	}
	manifest := subscriptionManifest{
		Version: "zboard.subscription/v1", GeneratedAt: now.Format(time.RFC3339),
		Subscription: subscriptionManifestSummary{
			ExpiresAt: subscription.EndAt.Format(time.RFC3339), FlowTotal: subscription.FlowTotal,
			FlowUsed: subscription.FlowUsed, FlowRemaining: remaining,
		},
		ProtocolEndpoints: nodes,
	}
	data, err := h.subscriptionTemplateData(ctx, manifest)
	if err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	content, deliveryFormat, err := h.renderPluginSubscriptionTemplate(ctx, format, data)
	if err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	digest := sha256.Sum256([]byte(content))
	contentSHA := hex.EncodeToString(digest[:])
	display := strings.TrimSpace(summary.PlanName)
	if display == "" {
		display = "Subscription " + strconv.FormatUint(uint64(subscription.ID), 10)
	}
	return pluginv1.ProjectedSubscription{
		ID: strconv.FormatUint(uint64(subscription.ID), 10), DisplayName: display, Format: deliveryFormat,
		Revision: contentSHA, ContentSHA256: contentSHA, UpdatedAt: subscription.UpdatedAt.Unix(), Content: content,
	}, nil
}

func pluginSubscriptionFormat(value string) (string, bool) {
	value = normalizeSubscriptionRenderer(value)
	_, ok := subscriptionRenderer(value)
	return value, ok
}
