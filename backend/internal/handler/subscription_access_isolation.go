package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const accountSubscriptionAccessPrefix = "/api/v1/account/subscriptions/"

type subscriptionAccessView = entitlements.AccessView

func parseAccountSubscriptionAccessID(path string, rotate bool) (uint, error) {
	if !strings.HasPrefix(path, accountSubscriptionAccessPrefix) {
		return 0, errors.New("invalid subscription access path")
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, accountSubscriptionAccessPrefix), "/"), "/")
	if (!rotate && len(parts) != 2) || (rotate && len(parts) != 3) {
		return 0, errors.New("invalid subscription access path")
	}
	if parts[1] != "access" || (rotate && parts[2] != "rotate") {
		return 0, errors.New("invalid subscription access path")
	}
	parsed, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || parsed == 0 {
		return 0, errors.New("invalid subscription id")
	}
	return uint(parsed), nil
}

func subscriptionAccessAvailable(sub model.Subscription, now time.Time) bool {
	return entitlements.AccessAvailable(entitlements.Subscription(sub), now)
}

// ReconcileSubscriptionAccessTokens provisions one independent token for every
// currently usable subscription that has no token row. Revoked rows are kept
// revoked and are never silently reactivated.
func (h *handlers) ReconcileSubscriptionAccessTokens() error {
	return h.services.ReconcileSubscriptionAccess(context.Background(), h.credentialCipher)
}

func (h *handlers) ScopedClientSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if !isSubscriptionClientUserAgent(r.UserAgent()) {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}

	rawToken := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/v1/client/subscription/"))
	if rawToken == "" || strings.Contains(rawToken, "/") {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	service := h.services.ClientSubscriptionAccess()
	grant, err := service.Resolve(r.Context(), rawToken)
	if errors.Is(err, entitlements.ErrAccessPermission) {
		h.redirectSubscriptionCamouflage(w, r)
		return
	}
	if errors.Is(err, entitlements.ErrAccessInactive) {
		Forbidden(w, err.Error())
		return
	}
	if err != nil {
		ServerError(w, errors.New("subscription access resolution failed"))
		return
	}
	filter, err := parseSubscriptionProjectionFilter(r.URL.Query(), h.isProtocolSupported)
	if err != nil {
		BadRequestError(w, err)
		return
	}
	now := grant.ObservedAt
	subscription := model.Subscription(grant.Subscription)

	allSubscriptions := []model.Subscription{subscription}
	sources, err := h.loadSubscriptionProjectionSources(r.Context(), allSubscriptions)
	if err != nil {
		ServerError(w, err)
		return
	}
	subscriptions := filterSubscriptionsForProjection(allSubscriptions, sources, filter)
	if len(subscriptions) > 0 {
		if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
			ServerError(w, err)
			return
		}
	}
	manifestNodes, err := h.buildProjectedSubscriptionManifestNodes(r.Context(), subscriptions, filter, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	if len(subscriptions) > 0 {
		if err := h.sortSubscriptionManifestNodes(subscriptions, manifestNodes); err != nil {
			ServerError(w, fmt.Errorf("resolve subscription delivery order: %w", err))
			return
		}
	}

	remaining := subscription.FlowTotal - subscription.FlowUsed
	cycleTotal, cycleUsed := subscriptionCycleQuota(subscription)
	if remaining < 0 {
		remaining = 0
	}
	_ = service.MarkUsed(r.Context(), rawToken, grant.AccessID, now)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf(
		"upload=0; download=%d; total=%d; expire=%d",
		cycleUsed, cycleTotal, subscription.EndAt.Unix(),
	))
	manifest := subscriptionManifest{
		Version:     "zboard.subscription/v1",
		GeneratedAt: now.Format(time.RFC3339),
		Subscription: subscriptionManifestSummary{
			ExpiresAt:     subscription.EndAt.Format(time.RFC3339),
			FlowTotal:     cycleTotal,
			FlowUsed:      cycleUsed,
			FlowRemaining: remaining,
		},
		ProtocolEndpoints: manifestNodes,
	}
	h.writeProjectedSubscription(w, r, manifest)
}
