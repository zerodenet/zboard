package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

// PluginSubscriptionProjector is a delivery-adapter boundary. The application
// layer resolves and authorizes an owned subscription before rendering; the
// projector cannot select another principal or subscription.
type PluginSubscriptionProjector interface {
	SupportsPluginSubscriptionFormat(string) bool
	ProjectPluginSubscription(context.Context, entitlements.SubscriptionSummary, string, time.Time) (pluginv1.ProjectedSubscription, error)
}

// PluginHostServices exposes a small capability-scoped application API to a
// native plugin process. It composes existing domain capabilities and never
// exposes database handles, password hashes, login tokens, or internal HTTP.
type PluginHostServices struct {
	services  *Services
	projector PluginSubscriptionProjector
}

func NewPluginHostServices(services *Services, projector PluginSubscriptionProjector) *PluginHostServices {
	return &PluginHostServices{services: services, projector: projector}
}

func (s *PluginHostServices) PluginPrincipal(_ context.Context, pluginID string, userID uint) (string, error) {
	if s == nil || s.services == nil {
		return "", errors.New("plugin principal service is unavailable")
	}
	return s.principalToken(pluginID, userID)
}

func (s *PluginHostServices) CallPluginHost(ctx context.Context, pluginID, capability, operation string, payload json.RawMessage) (json.RawMessage, *pluginv1.HostCallError) {
	if s == nil || s.services == nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	var result any
	var callErr *pluginv1.HostCallError
	switch capability {
	case pluginv1.AccountAssertionCapability:
		if operation != "account.assert" {
			return nil, pluginHostError("forbidden")
		}
		result, callErr = s.assertAccount(ctx, pluginID, payload)
	case pluginv1.SubscriptionProjectionCapability:
		switch operation {
		case "subscriptions.list":
			result, callErr = s.listSubscriptions(ctx, pluginID, payload)
		case "subscriptions.get":
			result, callErr = s.getSubscription(ctx, pluginID, payload)
		default:
			return nil, pluginHostError("forbidden")
		}
	case pluginv1.MessageProjectionCapability:
		switch operation {
		case "messages.list":
			result, callErr = s.listMessages(ctx, pluginID, payload)
		case "messages.get":
			result, callErr = s.getMessage(ctx, pluginID, payload)
		case "messages.mark-read":
			result, callErr = s.markMessageRead(ctx, pluginID, payload)
		default:
			return nil, pluginHostError("forbidden")
		}
	case plugins.AccountSelfReadCapability, plugins.AccountAdminReadCapability,
		plugins.SubscriptionReadCapability, plugins.SubscriptionConfigReadCapability, plugins.SubscriptionAdminReadCapability,
		plugins.SubscriptionQuotaWriteCapability, plugins.SubscriptionTermWriteCapability, plugins.SubscriptionStatusWriteCapability,
		plugins.MessageReadCapability, plugins.MessageAckCapability:
		return s.callBusinessCapability(ctx, pluginID, capability, operation, payload)
	default:
		return nil, pluginHostError("forbidden")
	}
	if callErr != nil {
		return nil, callErr
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	return raw, nil
}

func (s *PluginHostServices) assertAccount(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.AccountAssertionRequest
	if plugins.DecodeStrict(payload, &request) != nil || len(request.Account) > 128 || request.Password == "" || len(request.Password) > 4096 {
		return nil, pluginHostError("invalid_request")
	}
	account, err := s.services.Identity.Accounts.Login(ctx, request.Account, request.Password)
	if errors.Is(err, identity.ErrCredentials) {
		return nil, pluginHostError("invalid_credentials")
	}
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	token, err := s.principalToken(pluginID, account.ID)
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	return pluginv1.Principal{ID: token, Display: account.Email, Admin: account.IsAdmin}, nil
}

func (s *PluginHostServices) listSubscriptions(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionListRequest
	if plugins.DecodeStrict(payload, &request) != nil || s.projector == nil || !s.projector.SupportsPluginSubscriptionFormat(request.Format) {
		return nil, pluginHostError("invalid_request")
	}
	userID, _, callErr := s.principal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	page, err := s.services.SubscriptionQueries.Owned(ctx, userID, entitlements.SubscriptionQuery{Status: "active", Limit: 200})
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	now := time.Now().UTC()
	result := make([]pluginv1.ProjectedSubscription, 0, len(page.Items))
	for _, summary := range page.Items {
		projected, err := s.projector.ProjectPluginSubscription(ctx, summary, request.Format, now)
		if err != nil {
			return nil, pluginHostError("temporary_unavailable")
		}
		projected.Content = ""
		result = append(result, projected)
	}
	return result, nil
}

func (s *PluginHostServices) getSubscription(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.SubscriptionContentRequest
	if plugins.DecodeStrict(payload, &request) != nil || s.projector == nil || !s.projector.SupportsPluginSubscriptionFormat(request.Format) {
		return nil, pluginHostError("invalid_request")
	}
	userID, _, callErr := s.principal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	subscriptionID, err := strconv.ParseUint(request.SubscriptionID, 10, 64)
	if err != nil || subscriptionID == 0 {
		return nil, pluginHostError("invalid_request")
	}
	page, err := s.services.SubscriptionQueries.Owned(ctx, userID, entitlements.SubscriptionQuery{ID: uint(subscriptionID), Status: "active", Limit: 1})
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	if len(page.Items) != 1 {
		return nil, pluginHostError("not_found")
	}
	projected, err := s.projector.ProjectPluginSubscription(ctx, page.Items[0], request.Format, time.Now().UTC())
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	if request.KnownRevision != "" && request.KnownRevision == projected.Revision {
		projected.Content = ""
		projected.NotModified = true
	}
	return projected, nil
}

func (s *PluginHostServices) listMessages(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageListRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Limit < 1 || request.Limit > 100 {
		return nil, pluginHostError("invalid_request")
	}
	userID, account, callErr := s.principal(ctx, pluginID, request.PrincipalID)
	if callErr != nil {
		return nil, callErr
	}
	offset := 0
	if request.Cursor != "" {
		parsed, err := strconv.ParseUint(request.Cursor, 10, 31)
		if err != nil {
			return nil, pluginHostError("invalid_request")
		}
		offset = int(parsed)
	}
	page, err := s.services.Announcements.History(ctx, experience.AnnouncementAudienceQuery{
		UserID: userID, Audiences: pluginMessageAudiences(account.IsAdmin), Now: time.Now().UTC(), Offset: offset, Limit: request.Limit,
	})
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
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

func (s *PluginHostServices) getMessage(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageGetRequest
	if plugins.DecodeStrict(payload, &request) != nil {
		return nil, pluginHostError("invalid_request")
	}
	_, item, callErr := s.messageTarget(ctx, pluginID, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	return projectPluginMessage(item, true), nil
}

func (s *PluginHostServices) markMessageRead(ctx context.Context, pluginID string, payload json.RawMessage) (any, *pluginv1.HostCallError) {
	var request pluginv1.MessageMarkReadRequest
	if plugins.DecodeStrict(payload, &request) != nil || request.Revision == 0 {
		return nil, pluginHostError("invalid_request")
	}
	userID, item, callErr := s.messageTarget(ctx, pluginID, request.PrincipalID, request.MessageID)
	if callErr != nil {
		return nil, callErr
	}
	if item.Announcement.Revision != request.Revision {
		return nil, pluginHostError("conflict")
	}
	receipt, err := s.services.Announcements.Acknowledge(ctx, userID, item.Announcement.ID, request.Revision, time.Now().UTC())
	if errors.Is(err, experience.ErrNotFound) {
		return nil, pluginHostError("not_found")
	}
	if errors.Is(err, experience.ErrConflict) {
		return nil, pluginHostError("conflict")
	}
	if err != nil {
		return nil, pluginHostError("temporary_unavailable")
	}
	return map[string]any{"message_id": request.MessageID, "read_at": receipt.ReadAt.Unix()}, nil
}

func (s *PluginHostServices) principal(ctx context.Context, pluginID, value string) (uint, identity.PublicAccount, *pluginv1.HostCallError) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != "p1" || pluginID == "" {
		return 0, identity.PublicAccount{}, pluginHostError("invalid_request")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	provided, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
	expected := s.principalSignature(pluginID, payload)
	parsed, parseErr := strconv.ParseUint(string(payload), 10, 64)
	if err != nil || signatureErr != nil || parseErr != nil || parsed == 0 || !hmac.Equal(provided, expected) {
		return 0, identity.PublicAccount{}, pluginHostError("unauthorized")
	}
	account, err := s.services.Identity.Accounts.Me(ctx, identity.Principal{ID: uint(parsed)})
	if err != nil {
		return 0, identity.PublicAccount{}, pluginHostError("unauthorized")
	}
	return uint(parsed), account, nil
}

func (s *PluginHostServices) principalToken(pluginID string, userID uint) (string, error) {
	if pluginID == "" || userID == 0 || s.services.Identity.secret == "" {
		return "", errors.New("plugin principal signing is unavailable")
	}
	payload := []byte(strconv.FormatUint(uint64(userID), 10))
	return "p1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.principalSignature(pluginID, payload)), nil
}

func (s *PluginHostServices) principalSignature(pluginID string, payload []byte) []byte {
	mac := hmac.New(sha256.New, []byte(s.services.Identity.secret))
	_, _ = mac.Write([]byte("zboard.plugin.principal.v1\x00"))
	_, _ = mac.Write([]byte(pluginID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (s *PluginHostServices) messageTarget(ctx context.Context, pluginID, principalID, rawMessageID string) (uint, experience.AnnouncementReadItem, *pluginv1.HostCallError) {
	userID, account, callErr := s.principal(ctx, pluginID, principalID)
	if callErr != nil {
		return 0, experience.AnnouncementReadItem{}, callErr
	}
	messageID, err := strconv.ParseUint(rawMessageID, 10, 64)
	if err != nil || messageID == 0 {
		return 0, experience.AnnouncementReadItem{}, pluginHostError("invalid_request")
	}
	page, err := s.services.Announcements.History(ctx, experience.AnnouncementAudienceQuery{
		UserID: userID, Audiences: pluginMessageAudiences(account.IsAdmin), Now: time.Now().UTC(), ID: uint(messageID), Limit: 1,
	})
	if err != nil {
		return 0, experience.AnnouncementReadItem{}, pluginHostError("temporary_unavailable")
	}
	if len(page.Items) != 1 {
		return 0, experience.AnnouncementReadItem{}, pluginHostError("not_found")
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

func pluginHostError(code string) *pluginv1.HostCallError {
	return &pluginv1.HostCallError{Code: code}
}
