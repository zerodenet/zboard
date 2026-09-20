package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func (h *handlers) CallPluginHost(ctx context.Context, pluginID, capability, operation string, payload json.RawMessage) (json.RawMessage, *pluginv1.HostCallError) {
	_ = pluginID
	var result any
	var callErr *pluginv1.HostCallError
	switch capability {
	case plugins.AccountAssertionCapability:
		if operation != "account.assert" {
			return nil, hostCallError("forbidden")
		}
		result, callErr = h.pluginAssertAccount(ctx, payload)
	case plugins.SubscriptionProjectionCapability:
		switch operation {
		case "subscriptions.list":
			result, callErr = h.pluginListSubscriptions(ctx, payload)
		case "subscriptions.get":
			result, callErr = h.pluginGetSubscription(ctx, payload)
		default:
			return nil, hostCallError("forbidden")
		}
	case plugins.MessageProjectionCapability:
		switch operation {
		case "messages.list":
			result, callErr = h.pluginListMessages(ctx, payload)
		case "messages.get":
			result, callErr = h.pluginGetMessage(ctx, payload)
		case "messages.mark-read":
			result, callErr = h.pluginMarkMessageRead(ctx, payload)
		default:
			return nil, hostCallError("forbidden")
		}
	default:
		return nil, hostCallError("forbidden")
	}
	if callErr != nil {
		return nil, callErr
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return raw, nil
}

func (h *handlers) pluginAssertAccount(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.AccountAssertionRequest
	if plugins.DecodeStrict(payload, &request) != nil || len(request.Account) > 128 || request.Password == "" || len(request.Password) > 4096 {
		return nil, hostCallError("invalid_request")
	}
	account, err := h.services.Identity.Accounts.Login(ctx, request.Account, request.Password)
	if errors.Is(err, identity.ErrCredentials) {
		return nil, hostCallError("invalid_credentials")
	}
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return pluginv1.Principal{
		ID: strconv.FormatUint(uint64(account.ID), 10), Display: account.Email, Admin: account.IsAdmin,
	}, nil
}

func (h *handlers) pluginListSubscriptions(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionListRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, _, callErr := h.pluginPrincipal(ctx, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	format, ok := pluginSubscriptionFormat(request.Format)
	if !ok {
		return nil, hostCallError("invalid_request")
	}
	page, err := h.services.SubscriptionQueries.Owned(ctx, userID, entitlements.SubscriptionQuery{Status: "active", Limit: 200})
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	now := time.Now().UTC()
	result := make([]pluginv1.ProjectedSubscription, 0, len(page.Items))
	for _, summary := range page.Items {
		projected, err := h.projectPluginSubscription(ctx, summary, format, now)
		if err != nil {
			return nil, hostCallError("temporary_unavailable")
		}
		projected.Content = ""
		result = append(result, projected)
	}
	return result, nil
}

func (h *handlers) pluginGetSubscription(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionContentRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, _, callErr := h.pluginPrincipal(ctx, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	subscriptionID, parseErr := strconv.ParseUint(request.SubscriptionID, 10, 64)
	format, ok := pluginSubscriptionFormat(request.Format)
	if parseErr != nil || subscriptionID == 0 || !ok {
		return nil, hostCallError("invalid_request")
	}
	page, err := h.services.SubscriptionQueries.Owned(ctx, userID, entitlements.SubscriptionQuery{ID: uint(subscriptionID), Status: "active", Limit: 1})
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	if len(page.Items) != 1 {
		return nil, hostCallError("not_found")
	}
	projected, err := h.projectPluginSubscription(ctx, page.Items[0], format, time.Now().UTC())
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	if request.KnownRevision != "" && request.KnownRevision == projected.Revision {
		projected.Content = ""
		projected.NotModified = true
	}
	return projected, nil
}

func (h *handlers) projectPluginSubscription(ctx context.Context, summary entitlements.SubscriptionSummary, format string, now time.Time) (pluginv1.ProjectedSubscription, error) {
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
	content, _, err := renderSubscriptionWithRenderer(format, nil, data)
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
		ID: strconv.FormatUint(uint64(subscription.ID), 10), DisplayName: display, Format: format,
		Revision: contentSHA, ContentSHA256: contentSHA, UpdatedAt: subscription.UpdatedAt.Unix(), Content: content,
	}, nil
}

func (h *handlers) pluginListMessages(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageListRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Limit < 1 || request.Limit > 100 {
		return nil, hostCallError("invalid_request")
	}
	userID, account, callErr := h.pluginPrincipal(ctx, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	offset := 0
	if request.Cursor != "" {
		parsed, err := strconv.ParseUint(request.Cursor, 10, 31)
		if err != nil {
			return nil, hostCallError("invalid_request")
		}
		offset = int(parsed)
	}
	page, err := h.services.Announcements.History(ctx, experience.AnnouncementAudienceQuery{
		UserID: userID, Audiences: pluginMessageAudiences(account.IsAdmin), Now: time.Now().UTC(), Offset: offset, Limit: request.Limit,
	})
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	items := make([]pluginv1.ProjectedMessage, 0, len(page.Items))
	for _, message := range page.Items {
		items = append(items, projectPluginMessage(message, false))
	}
	next := ""
	if offset+len(page.Items) < int(page.Total) {
		next = strconv.Itoa(offset + len(page.Items))
	}
	return pluginv1.MessagePage{Items: items, NextCursor: next}, nil
}

func (h *handlers) pluginGetMessage(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageGetRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	_, item, callErr := h.pluginMessageTarget(ctx, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	return projectPluginMessage(item, true), nil
}

func (h *handlers) pluginMarkMessageRead(ctx context.Context, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageMarkReadRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Revision == 0 {
		return nil, hostCallError("invalid_request")
	}
	userID, item, callErr := h.pluginMessageTarget(ctx, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	if item.Announcement.Revision != request.Revision {
		return nil, hostCallError("conflict")
	}
	receipt, err := h.services.Announcements.Acknowledge(ctx, userID, item.Announcement.ID, request.Revision, time.Now().UTC())
	if errors.Is(err, experience.ErrNotFound) {
		return nil, hostCallError("not_found")
	}
	if errors.Is(err, experience.ErrConflict) {
		return nil, hostCallError("conflict")
	}
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return map[string]any{"message_id": request.MessageID, "read_at": receipt.ReadAt.Unix()}, nil
}

func (h *handlers) pluginPrincipal(ctx context.Context, value string) (uint, identity.PublicAccount, *pluginv1.HostCallError) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, identity.PublicAccount{}, hostCallError("invalid_request")
	}
	account, err := h.services.Identity.Accounts.Me(ctx, identity.Principal{ID: uint(parsed)})
	if err != nil {
		return 0, identity.PublicAccount{}, hostCallError("unauthorized")
	}
	return uint(parsed), account, nil
}

func (h *handlers) pluginMessageTarget(ctx context.Context, principalID, rawMessageID string) (uint, experience.AnnouncementReadItem, *pluginv1.HostCallError) {
	userID, account, callErr := h.pluginPrincipal(ctx, principalID)
	if callErr != nil {
		return 0, experience.AnnouncementReadItem{}, callErr
	}
	messageID, err := strconv.ParseUint(rawMessageID, 10, 64)
	if err != nil || messageID == 0 {
		return 0, experience.AnnouncementReadItem{}, hostCallError("invalid_request")
	}
	page, err := h.services.Announcements.History(ctx, experience.AnnouncementAudienceQuery{
		UserID: userID, Audiences: pluginMessageAudiences(account.IsAdmin), Now: time.Now().UTC(), ID: uint(messageID), Limit: 1,
	})
	if err != nil {
		return 0, experience.AnnouncementReadItem{}, hostCallError("temporary_unavailable")
	}
	if len(page.Items) != 1 {
		return 0, experience.AnnouncementReadItem{}, hostCallError("not_found")
	}
	return userID, page.Items[0], nil
}

func pluginMessageAudiences(admin bool) []string {
	if admin {
		return []string{"all", "admin"}
	}
	return []string{"all", "user"}
}

func projectPluginMessage(item experience.AnnouncementReadItem, includeBody bool) pluginv1.ProjectedMessage {
	message := item.Announcement
	publishedAt := message.CreatedAt
	if message.StartsAt != nil {
		publishedAt = *message.StartsAt
	}
	result := pluginv1.ProjectedMessage{
		ID: strconv.FormatUint(uint64(message.ID), 10), Title: message.Title, Severity: message.Severity,
		Revision: message.Revision, PublishedAt: publishedAt.Unix(), UpdatedAt: message.UpdatedAt.Unix(),
	}
	if includeBody {
		result.Body = message.Content
	}
	if item.Read && item.ReadAt != nil {
		result.ReadAt = item.ReadAt.Unix()
	}
	return result
}

func pluginSubscriptionFormat(value string) (string, bool) {
	value = normalizeSubscriptionRenderer(value)
	_, ok := subscriptionRenderer(value)
	return value, ok
}

func hostCallError(code string) *pluginv1.HostCallError {
	return &pluginv1.HostCallError{Code: code}
}
