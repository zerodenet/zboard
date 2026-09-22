package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
	"github.com/zerodenet/zboard/backend/internal/capabilities/identity"
)

type subscriptionReadInput struct {
	ID     uint   `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	UserID uint   `json:"user_id,omitempty"`
}
type subscriptionConfigInput struct {
	ID            uint   `json:"id"`
	Format        string `json:"format"`
	KnownRevision string `json:"known_revision,omitempty"`
}

type accountAdminReadInput struct {
	UserID uint `json:"user_id"`
}
type quotaWriteInput struct {
	SubscriptionID uint   `json:"subscription_id"`
	DeltaMB        int64  `json:"delta_mb"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}
type subscriptionMutationInput struct {
	SubscriptionID uint   `json:"subscription_id"`
	Days           int    `json:"days,omitempty"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}
type messageReadInput struct {
	ID       uint   `json:"id,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}
type pluginMessage struct {
	ID       uint       `json:"id"`
	Title    string     `json:"title"`
	Content  string     `json:"content,omitempty"`
	Severity string     `json:"severity"`
	Revision uint64     `json:"revision"`
	Read     bool       `json:"read"`
	ReadAt   *time.Time `json:"read_at,omitempty"`
}
type pluginSubscriptionPage struct {
	Items  []entitlements.SubscriptionSummary `json:"items"`
	Total  int64                              `json:"total"`
	Offset int                                `json:"offset"`
	Limit  int                                `json:"limit"`
}
type pluginQuotaReceipt struct {
	TaskID         uint       `json:"task_id"`
	Status         int16      `json:"status"`
	IdempotencyKey string     `json:"idempotency_key"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"`
}

func pluginBusinessDescriptor(d catalog.Descriptor, kind, sensitivity, authorization, idempotency string, input, output string) catalog.Descriptor {
	d.Version, d.Kind, d.Sensitivity = "1.0", kind, sensitivity
	d.Authorization, d.Idempotency, d.Execution, d.TimeoutMillis = authorization, idempotency, "synchronous", 30000
	d.Quota, d.RateLimitPerMinute = "64 KiB input; bounded page or one target", 60
	d.ErrorCodes = []string{"unauthenticated", "permission_denied", "invalid_argument", "conflict", "rate_limited", "unavailable", "deadline_exceeded"}
	d.Compatibility, d.Deprecation = "Additive fields within v1; breaking changes require a new major version", "none"
	d.InputSchema, d.OutputSchema = json.RawMessage(input), json.RawMessage(output)
	return d
}

// RegisterPluginBusinessCapabilities binds plugin operations to the same domain
// services used by first-party handlers. The catalog authority supplies
// the actor; a JSON user_id is only accepted by separately granted admin calls.
func (s *Services) RegisterPluginBusinessCapabilities(registry *catalog.Registry, projector PluginSubscriptionProjector) error {
	accountOutput := `{"type":"object","required":["id","email","is_admin","status"],"properties":{"id":{"type":"integer"},"email":{"type":"string"},"is_admin":{"type":"boolean"},"status":{"type":"string"}}}`
	subscriptionOutput := `{"type":"object","required":["id","user_id","status","flow_total","flow_used","start_at","end_at"],"properties":{"id":{"type":"integer"},"user_id":{"type":"integer"},"status":{"type":"string"},"flow_total":{"type":"integer"},"flow_used":{"type":"integer"},"start_at":{"type":"string","format":"date-time"},"end_at":{"type":"string","format":"date-time"}}}`
	pageOutput := `{"type":"object","required":["items","total","offset","limit"],"properties":{"items":{"type":"array","maxItems":200},"total":{"type":"integer"},"offset":{"type":"integer"},"limit":{"type":"integer"}}}`
	messageOutput := `{"type":"object","required":["id","title","severity","revision","read"],"properties":{"id":{"type":"integer"},"title":{"type":"string"},"content":{"type":"string"},"severity":{"type":"string"},"revision":{"type":"integer"},"read":{"type":"boolean"}}}`
	messagePageOutput := `{"type":"object","required":["items","total","unread"],"properties":{"items":{"type":"array","maxItems":100},"total":{"type":"integer"},"unread":{"type":"integer"}}}`
	quotaOutput := `{"type":"object","required":["task_id","status","idempotency_key"],"properties":{"task_id":{"type":"integer"},"status":{"type":"integer"},"idempotency_key":{"type":"string"},"scheduled_at":{"type":"string","format":"date-time"}}}`
	self := pluginBusinessDescriptor(catalog.Descriptor{Name: "account.self.get", Owner: "identity"}, "query", "account_profile", "Verified current account session", "read_only", `{"type":"object","additionalProperties":false}`, accountOutput)
	if err := registry.Register(self, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		if _, err := catalog.DecodeObject[struct{}](raw); err != nil {
			return nil, err
		}
		return s.Identity.Accounts.Me(ctx, identity.Principal{ID: grant.Principal.AccountID})
	}); err != nil {
		return err
	}
	admin := pluginBusinessDescriptor(catalog.Descriptor{Name: "account.admin.get", Owner: "identity"}, "query", "account_profile", "Current administrator, admin page and distinct declared grant", "read_only", `{"type":"object","required":["user_id"],"additionalProperties":false,"properties":{"user_id":{"type":"integer","minimum":1}}}`, accountOutput)
	if err := registry.Register(admin, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		if !grant.Administrative {
			return nil, catalog.ErrDenied
		}
		in, err := catalog.DecodeObject[accountAdminReadInput](raw)
		if err != nil || in.UserID == 0 {
			return nil, catalog.ErrInput
		}
		return s.Identity.Directory.Detail(ctx, in.UserID)
	}); err != nil {
		return err
	}

	for _, entry := range []struct {
		descriptor    catalog.Descriptor
		admin, detail bool
	}{
		{catalog.Descriptor{Name: "subscriptions.owned.list", Owner: "entitlements"}, false, false},
		{catalog.Descriptor{Name: "subscriptions.owned.get", Owner: "entitlements"}, false, true},
		{catalog.Descriptor{Name: "subscriptions.admin.list", Owner: "entitlements"}, true, false},
		{catalog.Descriptor{Name: "subscriptions.admin.get", Owner: "entitlements"}, true, true},
	} {
		entry := entry
		input := `{"type":"object","additionalProperties":false,"properties":{"id":{"type":"integer","minimum":1},"status":{"enum":["active","expired","canceled"]},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":200}}}`
		if entry.admin {
			input = `{"type":"object","additionalProperties":false,"properties":{"id":{"type":"integer","minimum":1},"user_id":{"type":"integer","minimum":1},"status":{"enum":["active","expired","canceled"]},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":200}}}`
		}
		output := pageOutput
		if entry.detail {
			output = subscriptionOutput
		}
		d := pluginBusinessDescriptor(entry.descriptor, "query", "subscription", "Current owner; admin operations additionally require current administrator and admin page", "read_only", input, output)
		if err := registry.Register(d, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
			in, err := catalog.DecodeObject[subscriptionReadInput](raw)
			if err != nil {
				return nil, err
			}
			if entry.admin != grant.Administrative {
				return nil, catalog.ErrDenied
			}
			if entry.detail && in.ID == 0 {
				return nil, catalog.ErrInput
			}
			if !entry.admin && in.UserID != 0 {
				return nil, catalog.ErrDenied
			}
			q := entitlements.SubscriptionQuery{ID: in.ID, UserID: in.UserID, Status: in.Status, Offset: in.Offset, Limit: in.Limit}
			var page entitlements.SubscriptionPage
			if entry.admin {
				page, err = s.SubscriptionQueries.Administrative(ctx, grant.Principal.AccountID, q)
			} else {
				page, err = s.SubscriptionQueries.Owned(ctx, grant.Principal.AccountID, q)
			}
			if err != nil {
				var invalid *entitlements.QueryError
				if errors.As(err, &invalid) {
					return nil, catalog.ErrInput
				}
				if errors.Is(err, entitlements.ErrAccessPermission) {
					return nil, catalog.ErrDenied
				}
				return nil, err
			}
			if entry.detail {
				if len(page.Items) != 1 {
					return nil, catalog.ErrDenied
				}
				return page.Items[0], nil
			}
			return pluginSubscriptionPage{Items: page.Items, Total: page.Total, Offset: page.Offset, Limit: page.Limit}, nil
		}); err != nil {
			return err
		}
	}
	configInput := `{"type":"object","required":["id","format"],"additionalProperties":false,"properties":{"id":{"type":"integer","minimum":1},"format":{"type":"string","minLength":1,"maxLength":64},"known_revision":{"type":"string","maxLength":128}}}`
	configOutput := `{"type":"object","required":["id","display_name","format","revision","content_sha256","updated_at"],"properties":{"id":{"type":"string"},"display_name":{"type":"string"},"format":{"type":"string"},"revision":{"type":"string"},"content_sha256":{"type":"string"},"updated_at":{"type":"integer"},"content":{"type":"string"},"not_modified":{"type":"boolean"}}}`
	config := pluginBusinessDescriptor(catalog.Descriptor{Name: "subscriptions.owned.config", Owner: "entitlements"}, "query", "subscription_configuration", "Current active owner and separate configuration projection grant", "read_only", configInput, configOutput)
	if err := registry.Register(config, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		in, err := catalog.DecodeObject[subscriptionConfigInput](raw)
		if err != nil || in.ID == 0 || len(in.Format) > 64 || len(in.KnownRevision) > 128 {
			return nil, catalog.ErrInput
		}
		if projector == nil {
			return nil, catalog.ErrUnavailable
		}
		if !projector.SupportsPluginSubscriptionFormat(in.Format) {
			return nil, catalog.ErrInput
		}
		page, err := s.SubscriptionQueries.Owned(ctx, grant.Principal.AccountID, entitlements.SubscriptionQuery{ID: in.ID, Status: "active", Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Items) != 1 {
			return nil, catalog.ErrDenied
		}
		projected, err := projector.ProjectPluginSubscription(ctx, page.Items[0], in.Format, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if in.KnownRevision != "" && in.KnownRevision == projected.Revision {
			projected.Content = ""
			projected.NotModified = true
		}
		return projected, nil
	}); err != nil {
		return err
	}
	quota := pluginBusinessDescriptor(catalog.Descriptor{Name: "subscriptions.quota.adjust", Owner: "entitlements"}, "command", "subscription_quota", "Current administrator, admin page and distinct write grant; one subscription", "Required plugin-scoped key; durable task and per-target replay protection", `{"type":"object","required":["subscription_id","delta_mb","reason","idempotency_key"],"additionalProperties":false,"properties":{"subscription_id":{"type":"integer","minimum":1},"delta_mb":{"type":"integer"},"reason":{"type":"string","minLength":3,"maxLength":255},"idempotency_key":{"type":"string","minLength":1,"maxLength":128}}}`, quotaOutput)
	if err := registry.Register(quota, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		if !grant.Administrative {
			return nil, catalog.ErrDenied
		}
		in, err := catalog.DecodeObject[quotaWriteInput](raw)
		if err != nil || in.SubscriptionID == 0 || strings.TrimSpace(in.IdempotencyKey) == "" || len(in.IdempotencyKey) > 128 {
			return nil, catalog.ErrInput
		}
		key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", grant.Principal.PluginID, grant.Principal.AccountID, in.IdempotencyKey)))
		receipt, err := s.QuotaRequests().Create(ctx, grant.Principal.AccountID, entitlements.QuotaRequestInput{
			Scope: entitlements.QuotaScope{SubscriptionIDs: []uint{in.SubscriptionID}}, Content: entitlements.QuotaAdjustment{DeltaMB: in.DeltaMB, Reason: in.Reason},
			IdempotencyKey: "plugin:" + hex.EncodeToString(key[:]), ReplayExisting: true, AutoRun: true, Origin: fmt.Sprintf("plugin:%s", grant.Principal.PluginID),
		})
		if errors.Is(err, entitlements.ErrQuotaRequestInvalid) {
			return nil, catalog.ErrInput
		}
		if errors.Is(err, entitlements.ErrQuotaRequestConflict) {
			return nil, catalog.ErrConflict
		}
		if errors.Is(err, entitlements.ErrAccessPermission) {
			return nil, catalog.ErrDenied
		}
		if err != nil {
			return nil, err
		}
		return pluginQuotaReceipt{TaskID: receipt.ID, Status: receipt.Status, IdempotencyKey: in.IdempotencyKey, ScheduledAt: receipt.ScheduledAt}, nil
	}); err != nil {
		return err
	}
	for _, entry := range []struct {
		descriptor catalog.Descriptor
		kind       string
	}{
		{catalog.Descriptor{Name: "subscriptions.term.extend", Owner: "entitlements"}, entitlements.SubscriptionExtend},
		{catalog.Descriptor{Name: "subscriptions.status.cancel", Owner: "entitlements"}, entitlements.SubscriptionCancel},
	} {
		entry := entry
		input := `{"type":"object","required":["subscription_id","reason","idempotency_key"],"additionalProperties":false,"properties":{"subscription_id":{"type":"integer","minimum":1},"reason":{"type":"string","minLength":3,"maxLength":255},"idempotency_key":{"type":"string","minLength":1,"maxLength":128}}}`
		if entry.kind == entitlements.SubscriptionExtend {
			input = `{"type":"object","required":["subscription_id","days","reason","idempotency_key"],"additionalProperties":false,"properties":{"subscription_id":{"type":"integer","minimum":1},"days":{"type":"integer","minimum":1,"maximum":3650},"reason":{"type":"string","minLength":3,"maxLength":255},"idempotency_key":{"type":"string","minLength":1,"maxLength":128}}}`
		}
		output := `{"type":"object","required":["mutation_id","subscription_id","status","end_at"],"properties":{"mutation_id":{"type":"integer"},"subscription_id":{"type":"integer"},"status":{"type":"string"},"end_at":{"type":"string","format":"date-time"}}}`
		d := pluginBusinessDescriptor(entry.descriptor, "command", "subscription_state", "Current administrator and distinct write grant; one active subscription", "Required plugin-scoped key; same request returns prior receipt; conflicting replay is rejected", input, output)
		if err := registry.Register(d, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
			if !grant.Administrative {
				return nil, catalog.ErrDenied
			}
			in, err := catalog.DecodeObject[subscriptionMutationInput](raw)
			if err != nil || in.SubscriptionID == 0 || strings.TrimSpace(in.IdempotencyKey) == "" || len(in.IdempotencyKey) > 128 {
				return nil, catalog.ErrInput
			}
			key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", grant.Principal.PluginID, grant.Principal.AccountID, entry.kind, in.IdempotencyKey)))
			out, err := s.SubscriptionMutations().Apply(ctx, grant.Principal.AccountID, entitlements.SubscriptionMutationInput{
				SubscriptionID: in.SubscriptionID, Kind: entry.kind, Days: in.Days, Reason: in.Reason,
				IdempotencyKey: "plugin:" + hex.EncodeToString(key[:]), Origin: "plugin:" + grant.Principal.PluginID,
			})
			if errors.Is(err, entitlements.ErrSubscriptionMutationInvalid) {
				return nil, catalog.ErrInput
			}
			if errors.Is(err, entitlements.ErrSubscriptionMutationConflict) {
				return nil, catalog.ErrConflict
			}
			if errors.Is(err, entitlements.ErrAccessPermission) || errors.Is(err, entitlements.ErrAdministrativeRead) || errors.Is(err, entitlements.ErrAccessNotFound) {
				return nil, catalog.ErrDenied
			}
			return out, err
		}); err != nil {
			return err
		}
	}

	for _, entry := range []struct {
		descriptor  catalog.Descriptor
		detail, ack bool
	}{
		{catalog.Descriptor{Name: "messages.owned.list", Owner: "experience"}, false, false},
		{catalog.Descriptor{Name: "messages.owned.get", Owner: "experience"}, true, false},
		{catalog.Descriptor{Name: "messages.owned.ack", Owner: "experience"}, true, true},
	} {
		entry := entry
		kind, idem := "query", "read_only"
		if entry.ack {
			kind, idem = "command", "revision-conditional acknowledgement"
		}
		output := messagePageOutput
		if entry.detail {
			output = messageOutput
		}
		if entry.ack {
			output = `{"type":"object","required":["announcement_id","user_id","revision","read_at"]}`
		}
		d := pluginBusinessDescriptor(entry.descriptor, kind, "message", "Verified current account and audience visibility", idem, `{"type":"object","additionalProperties":false,"properties":{"id":{"type":"integer","minimum":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100},"revision":{"type":"integer","minimum":1}}}`, output)
		if err := registry.Register(d, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
			in, err := catalog.DecodeObject[messageReadInput](raw)
			if err != nil || in.Offset < 0 || in.Limit < 0 || in.Limit > 100 || (entry.detail && in.ID == 0) || (entry.ack && in.Revision == 0) {
				return nil, catalog.ErrInput
			}
			account, err := s.Identity.Accounts.Me(ctx, identity.Principal{ID: grant.Principal.AccountID})
			if err != nil {
				return nil, catalog.ErrDenied
			}
			audiences := []string{"all", "user"}
			if account.IsAdmin {
				audiences = []string{"all", "admin"}
			}
			limit := in.Limit
			if limit == 0 {
				limit = 50
			}
			if entry.detail {
				limit = 1
			}
			page, err := s.Announcements.History(ctx, experience.AnnouncementAudienceQuery{UserID: account.ID, Audiences: audiences, ID: in.ID, Offset: in.Offset, Limit: limit, Now: time.Now().UTC()})
			if err != nil {
				return nil, err
			}
			project := func(item experience.AnnouncementReadItem) pluginMessage {
				out := pluginMessage{ID: item.Announcement.ID, Title: item.Announcement.Title, Severity: item.Announcement.Severity, Revision: item.Announcement.Revision, Read: item.Read, ReadAt: item.ReadAt}
				if entry.detail {
					out.Content = item.Announcement.Content
				}
				return out
			}
			if entry.detail {
				if len(page.Items) != 1 {
					return nil, catalog.ErrDenied
				}
				item := page.Items[0]
				if entry.ack {
					if item.Announcement.Revision != in.Revision {
						return nil, catalog.ErrConflict
					}
					receipt, err := s.Announcements.Acknowledge(ctx, account.ID, item.Announcement.ID, in.Revision, time.Now().UTC())
					if errors.Is(err, experience.ErrConflict) {
						return nil, catalog.ErrConflict
					}
					if errors.Is(err, experience.ErrNotFound) || errors.Is(err, experience.ErrPermission) {
						return nil, catalog.ErrDenied
					}
					return receipt, err
				}
				return project(item), nil
			}
			items := make([]pluginMessage, 0, len(page.Items))
			for _, item := range page.Items {
				items = append(items, project(item))
			}
			return map[string]any{"items": items, "total": page.Total, "unread": page.Unread}, nil
		}); err != nil {
			return err
		}
	}
	return nil
}
