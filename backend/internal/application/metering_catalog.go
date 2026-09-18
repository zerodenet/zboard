package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"time"
)

type usageCapabilityInput struct {
	UserID             uint      `json:"user_id,omitempty"`
	SubscriptionID     uint      `json:"subscription_id,omitempty"`
	NodeID             uint      `json:"node_id,omitempty"`
	ProtocolEndpointID uint      `json:"protocol_endpoint_id,omitempty"`
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
	Bucket             string    `json:"bucket"`
	Limit              *int      `json:"limit,omitempty"`
	Cursor             *struct {
		At        time.Time `json:"at"`
		ID        uint      `json:"id"`
		Direction string    `json:"direction"`
	} `json:"cursor,omitempty"`
	IncludeTotals bool `json:"include_totals,omitempty"`
}
type usageCapabilityOutput struct {
	Items      []metering.UsageRow       `json:"items"`
	Statistics *metering.UsageStatistics `json:"statistics"`
	HasMore    bool                      `json:"has_more"`
}

// RegisterMeteringCapabilities connects the transport-neutral catalog to the
// same application service used by HTTP. The registry authority supplies the
// identity and grant; neither comes from JSON. Credential adapters are wired
// separately, never replaced with an administrator token.
func (s *Services) RegisterMeteringCapabilities(registry *catalog.Registry, cache metering.StatisticsCache, incremental *meteringstore.IncrementalCache) error {
	descriptor := catalog.Descriptor{
		Name: "metering.usage.query", Version: "1.0", Owner: "metering", Kind: "query", Sensitivity: "account_usage",
		Authorization: "Current credential scope and account ownership; cross-account queries require an administrative grant and a current administrator account.",
		Idempotency:   "read_only", Execution: "synchronous", TimeoutMillis: 30000, Quota: "64 KiB input; 1-200 groups per page; maximum 366 day range",
		RateLimitPerMinute: 60,
		ErrorCodes:         []string{"unauthenticated", "permission_denied", "invalid_argument", "rate_limited", "unavailable", "deadline_exceeded"}, Compatibility: "Additive response fields within v1; breaking changes require a new major version", Deprecation: "none",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["from","to","bucket"],"properties":{"user_id":{"type":"integer","minimum":0},"subscription_id":{"type":"integer","minimum":0},"node_id":{"type":"integer","minimum":0},"protocol_endpoint_id":{"type":"integer","minimum":0},"from":{"type":"string","format":"date-time"},"to":{"type":"string","format":"date-time"},"bucket":{"enum":["minute","hour","day"]},"limit":{"type":"integer","minimum":1,"maximum":200,"default":50},"include_totals":{"type":"boolean","default":false},"cursor":{"type":"object","additionalProperties":false,"required":["at","id","direction"],"properties":{"at":{"type":"string","format":"date-time"},"id":{"type":"integer","minimum":1},"direction":{"enum":["older","newer"]}}}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["items","statistics","has_more"],"properties":{"items":{"type":"array","maxItems":200,"items":{"type":"object","required":["id","user_id","node_id","raw_bytes","upload_bytes","download_bytes","protocol_multiplier_milli","used_bytes","record_at","record_count"],"properties":{"id":{"type":"integer"},"user_id":{"type":"integer"},"subscription_id":{"type":"integer"},"node_id":{"type":"integer"},"raw_bytes":{"type":"integer"},"upload_bytes":{"type":"integer"},"download_bytes":{"type":"integer"},"protocol_multiplier_milli":{"type":"integer"},"used_bytes":{"type":"integer"},"record_at":{"type":"string","format":"date-time"},"record_count":{"type":"integer"}}}},"statistics":{"type":["object","null"]},"has_more":{"type":"boolean"}}}`),
	}
	service := s.Usage(cache, incremental)
	return registry.Register(descriptor, func(ctx context.Context, grant catalog.Grant, raw json.RawMessage) (any, error) {
		in, err := catalog.DecodeObject[usageCapabilityInput](raw)
		if err != nil {
			return nil, err
		}
		if in.Bucket != "minute" && in.Bucket != "hour" && in.Bucket != "day" {
			return nil, catalog.ErrInput
		}
		if !grant.Administrative && in.UserID != 0 && in.UserID != grant.Principal.AccountID {
			return nil, catalog.ErrDenied
		}
		limit := 50
		if in.Limit != nil {
			limit = *in.Limit
			if limit < 1 || limit > 200 {
				return nil, catalog.ErrInput
			}
		}
		q := metering.UsageQuery{RecordsQuery: metering.RecordsQuery{Administrative: grant.Administrative, UserID: in.UserID, SubscriptionID: in.SubscriptionID, NodeID: in.NodeID, ProtocolEndpointID: in.ProtocolEndpointID, From: in.From, To: in.To, Limit: limit}, Bucket: in.Bucket, IncludeTotals: in.IncludeTotals}
		if in.Cursor != nil {
			q.Cursor = &metering.RecordCursor{At: in.Cursor.At, ID: in.Cursor.ID, Direction: in.Cursor.Direction}
		}
		out, err := service.Read(ctx, grant.Principal.AccountID, q)
		if err != nil {
			if errors.Is(err, metering.ErrTrendPermission) {
				return nil, catalog.ErrDenied
			}
			var invalid *metering.PolicyValidation
			if errors.As(err, &invalid) {
				return nil, catalog.ErrInput
			}
			return nil, err
		}
		return usageCapabilityOutput{Items: out.Rows, Statistics: out.Statistics, HasMore: out.HasMore}, nil
	})
}
