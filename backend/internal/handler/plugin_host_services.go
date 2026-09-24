package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) CallPluginHost(ctx context.Context, pluginID, capability, operation string, payload json.RawMessage) (json.RawMessage, *pluginv1.HostCallError) {
	var result any
	var err *pluginv1.HostCallError
	switch capability {
	case plugins.AccountAssertionCapability:
		if operation != "account.assert" {
			return nil, hostCallError("forbidden")
		}
		result, err = h.pluginAssertAccount(ctx, pluginID, payload)
	case plugins.SubscriptionProjectionCapability:
		switch operation {
		case "subscriptions.capabilities":
			var request struct{}
			if len(payload) != 0 && plugins.DecodeStrict(payload, &request) != nil {
				return nil, hostCallError("invalid_request")
			}
			result = pluginv1.SubscriptionCapabilities{Usage: true}
		case "subscriptions.list":
			result, err = h.pluginListSubscriptions(ctx, pluginID, payload)
		case "subscriptions.get":
			result, err = h.pluginGetSubscription(ctx, pluginID, payload)
		case "subscriptions.usage":
			result, err = h.pluginGetSubscriptionUsage(ctx, pluginID, payload)
		default:
			return nil, hostCallError("forbidden")
		}
	case plugins.MessageProjectionCapability:
		switch operation {
		case "messages.list":
			result, err = h.pluginListMessages(ctx, pluginID, payload)
		case "messages.get":
			result, err = h.pluginGetMessage(ctx, pluginID, payload)
		case "messages.mark-read":
			result, err = h.pluginMarkMessageRead(ctx, pluginID, payload)
		default:
			return nil, hostCallError("forbidden")
		}
	default:
		return nil, hostCallError("forbidden")
	}
	if err != nil {
		return nil, err
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return raw, nil
}

func (h *handlers) pluginAssertAccount(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.AccountAssertionRequest
	if plugins.DecodeStrict(payload, &request) != nil || len(request.Account) > 128 || request.Password == "" || len(request.Password) > 4096 {
		return nil, hostCallError("invalid_request")
	}
	account := normalizeEmail(request.Account)
	if account == "" {
		return nil, hostCallError("invalid_credentials")
	}
	var user model.User
	if err := h.db.WithContext(ctx).Where("email = ? AND status = ?", account, userStatusActive).First(&user).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)) != nil {
		return nil, hostCallError("invalid_credentials")
	}
	display := strings.TrimSpace(user.AccountName)
	if display == "" {
		display = user.Email
	}
	principalID, err := h.pluginPrincipalToken(pluginID, user.ID)
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return pluginv1.Principal{ID: principalID, Display: display, Admin: user.IsAdmin}, nil
}

func (h *handlers) pluginListSubscriptions(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionListRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, callErr := h.pluginPrincipal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	format, ok := pluginSubscriptionFormat(request.Format)
	if !ok {
		return nil, hostCallError("invalid_request")
	}
	now := time.Now().UTC()
	if err := expireSubscriptions(h.db.WithContext(ctx), userID, now); err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	var subscriptions []model.Subscription
	if err := h.db.WithContext(ctx).Where(
		"user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total", userID, subStatusActive, now,
	).Order("id asc").Find(&subscriptions).Error; err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	result := make([]pluginv1.ProjectedSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		projected, err := h.projectPluginSubscription(ctx, subscription, format, now)
		if err != nil {
			return nil, hostCallError("temporary_unavailable")
		}
		projected.Content = ""
		result = append(result, projected)
	}
	return result, nil
}

func (h *handlers) pluginGetSubscription(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionContentRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, callErr := h.pluginPrincipal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	subscriptionID, parseErr := strconv.ParseUint(request.SubscriptionID, 10, 64)
	format, ok := pluginSubscriptionFormat(request.Format)
	if parseErr != nil || subscriptionID == 0 || !ok {
		return nil, hostCallError("invalid_request")
	}
	now := time.Now().UTC()
	var subscription model.Subscription
	if err := h.db.WithContext(ctx).Where(
		"id = ? AND user_id = ? AND status = ? AND end_at > ? AND flow_used < flow_total",
		subscriptionID, userID, subStatusActive, now,
	).First(&subscription).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, hostCallError("not_found")
		}
		return nil, hostCallError("temporary_unavailable")
	}
	projected, err := h.projectPluginSubscription(ctx, subscription, format, now)
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	if request.KnownRevision != "" && request.KnownRevision == projected.Revision {
		projected.Content = ""
		projected.NotModified = true
	}
	return projected, nil
}

func (h *handlers) pluginGetSubscriptionUsage(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionUsageRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, callErr := h.pluginPrincipal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	subscriptionID, parseErr := strconv.ParseUint(request.SubscriptionID, 10, 64)
	if parseErr != nil || subscriptionID == 0 {
		return nil, hostCallError("invalid_request")
	}
	var subscription model.Subscription
	if err := h.db.WithContext(ctx).Select("flow_used", "flow_total", "end_at").Where(
		"id = ? AND user_id = ?", subscriptionID, userID,
	).First(&subscription).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, hostCallError("not_found")
		}
		return nil, hostCallError("temporary_unavailable")
	}
	return pluginv1.SubscriptionUsage{
		UsedBytes:      uint64(max(int64(0), subscription.FlowUsed)),
		TotalBytes:     uint64(max(int64(0), subscription.FlowTotal)),
		ExpireAtUnixMs: uint64(max(int64(0), subscription.EndAt.UnixMilli())),
	}, nil
}

func (h *handlers) projectPluginSubscription(ctx context.Context, subscription model.Subscription, format string, now time.Time) (pluginv1.ProjectedSubscription, error) {
	if err := h.ensureCredentialsForSubscriptions([]model.Subscription{subscription}); err != nil {
		return pluginv1.ProjectedSubscription{}, err
	}
	nodes, err := h.buildProjectedSubscriptionManifestNodes([]model.Subscription{subscription}, subscriptionProjectionFilter{}, now)
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
	display := "Subscription " + strconv.FormatUint(uint64(subscription.ID), 10)
	var plan model.Plan
	if err := h.db.WithContext(ctx).Select("name").First(&plan, subscription.PlanID).Error; err == nil && strings.TrimSpace(plan.Name) != "" {
		display = plan.Name
	}
	return pluginv1.ProjectedSubscription{
		ID: strconv.FormatUint(uint64(subscription.ID), 10), DisplayName: display, Format: format,
		Revision: contentSHA, ContentSHA256: contentSHA, UpdatedAt: subscription.UpdatedAt.Unix(), Content: content,
	}, nil
}

func (h *handlers) pluginListMessages(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageListRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Limit < 1 || request.Limit > 100 {
		return nil, hostCallError("invalid_request")
	}
	userID, callErr := h.pluginPrincipal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	user, callErr := h.pluginUser(ctx, userID)
	if callErr != nil {
		return nil, callErr
	}
	query := h.pluginMessageQuery(ctx, user)
	if request.Cursor != "" {
		cursor, err := strconv.ParseUint(request.Cursor, 10, 64)
		if err != nil || cursor == 0 {
			return nil, hostCallError("invalid_request")
		}
		query = query.Where("id < ?", cursor)
	}
	var messages []model.Announcement
	if err := query.Order("id desc").Limit(request.Limit + 1).Find(&messages).Error; err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	next := ""
	if len(messages) > request.Limit {
		messages = messages[:request.Limit]
		next = strconv.FormatUint(uint64(messages[len(messages)-1].ID), 10)
	}
	reads, err := announcementReadMap(h.db.WithContext(ctx), userID, messages)
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	items := make([]pluginv1.ProjectedMessage, 0, len(messages))
	for _, message := range messages {
		items = append(items, projectPluginMessage(message, reads[message.ID], false))
	}
	return pluginv1.MessagePage{Items: items, NextCursor: next}, nil
}

func (h *handlers) pluginGetMessage(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageGetRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, hostCallError("invalid_request")
	}
	userID, messageID, user, callErr := h.pluginMessageTarget(ctx, pluginID, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	var message model.Announcement
	if err := h.pluginMessageQuery(ctx, user).Where("id = ?", messageID).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, hostCallError("not_found")
		}
		return nil, hostCallError("temporary_unavailable")
	}
	reads, err := announcementReadMap(h.db.WithContext(ctx), userID, []model.Announcement{message})
	if err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return projectPluginMessage(message, reads[message.ID], true), nil
}

func (h *handlers) pluginMarkMessageRead(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageMarkReadRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Revision == 0 {
		return nil, hostCallError("invalid_request")
	}
	userID, messageID, user, callErr := h.pluginMessageTarget(ctx, pluginID, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	var message model.Announcement
	if err := h.pluginMessageQuery(ctx, user).Where("id = ? AND revision = ?", messageID, request.Revision).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, hostCallError("not_found")
		}
		return nil, hostCallError("temporary_unavailable")
	}
	now := time.Now().UTC()
	receipt := model.AnnouncementRead{AnnouncementID: message.ID, UserID: userID, Revision: message.Revision, ReadAt: now}
	if err := h.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "announcement_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"revision": message.Revision, "read_at": now}),
	}).Create(&receipt).Error; err != nil {
		return nil, hostCallError("temporary_unavailable")
	}
	return map[string]any{"message_id": request.MessageID, "read_at": now.Unix()}, nil
}

// PluginPrincipal gives a page action the same opaque identity that account.assert
// gives the native plugin. A raw account ID is never a service authorization.
func (h *handlers) PluginPrincipal(ctx context.Context, pluginID string, userID uint) (string, error) {
	if _, callErr := h.pluginUser(ctx, userID); callErr != nil {
		return "", callErr
	}
	return h.pluginPrincipalToken(pluginID, userID)
}

func (h *handlers) pluginPrincipalToken(pluginID string, userID uint) (string, error) {
	if pluginID == "" || userID == 0 || h.jwtSecret == "" {
		return "", errors.New("plugin principal signing is unavailable")
	}
	payload := []byte(strconv.FormatUint(uint64(userID), 10))
	return "p1." + base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(h.pluginPrincipalSignature(pluginID, payload)), nil
}

func (h *handlers) pluginPrincipalSignature(pluginID string, payload []byte) []byte {
	mac := hmac.New(sha256.New, []byte(h.jwtSecret))
	_, _ = mac.Write([]byte("zboard.plugin.principal.v1\x00"))
	_, _ = mac.Write([]byte(pluginID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (h *handlers) pluginPrincipal(ctx context.Context, pluginID, value string) (uint, *pluginv1.HostCallError) {
	parts := strings.Split(value, ".")
	if pluginID == "" || h.jwtSecret == "" || len(value) > 128 || len(parts) != 3 || parts[0] != "p1" {
		return 0, hostCallError("unauthorized")
	}
	payload, payloadErr := base64.RawURLEncoding.DecodeString(parts[1])
	provided, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
	parsed, parseErr := strconv.ParseUint(string(payload), 10, 64)
	if payloadErr != nil || signatureErr != nil || parseErr != nil || parsed == 0 || uint64(uint(parsed)) != parsed ||
		!hmac.Equal(provided, h.pluginPrincipalSignature(pluginID, payload)) {
		return 0, hostCallError("unauthorized")
	}
	_, callErr := h.pluginUser(ctx, uint(parsed))
	return uint(parsed), callErr
}

func (h *handlers) pluginUser(ctx context.Context, userID uint) (model.User, *pluginv1.HostCallError) {
	var user model.User
	if err := h.db.WithContext(ctx).Where("id = ? AND status = ?", userID, userStatusActive).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return user, hostCallError("unauthorized")
		}
		return user, hostCallError("temporary_unavailable")
	}
	return user, nil
}

func (h *handlers) pluginMessageTarget(ctx context.Context, pluginID, principalID, rawMessageID string) (uint, uint, model.User, *pluginv1.HostCallError) {
	userID, callErr := h.pluginPrincipal(ctx, pluginID, principalID)
	if callErr != nil {
		return 0, 0, model.User{}, callErr
	}
	messageID, err := strconv.ParseUint(rawMessageID, 10, 64)
	if err != nil || messageID == 0 {
		return 0, 0, model.User{}, hostCallError("invalid_request")
	}
	user, callErr := h.pluginUser(ctx, userID)
	return userID, uint(messageID), user, callErr
}

func (h *handlers) pluginMessageQuery(ctx context.Context, user model.User) *gorm.DB {
	now := time.Now().UTC()
	audiences := []string{"all", "user"}
	if user.IsAdmin {
		audiences = []string{"all", "admin"}
	}
	return h.db.WithContext(ctx).Model(&model.Announcement{}).
		Where("status = ?", "published").Where("audience IN ?", audiences).
		Where("starts_at IS NULL OR starts_at <= ?", now)
}

func projectPluginMessage(message model.Announcement, receipt model.AnnouncementRead, includeBody bool) pluginv1.ProjectedMessage {
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
	if receipt.Revision >= message.Revision {
		result.ReadAt = receipt.ReadAt.Unix()
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
