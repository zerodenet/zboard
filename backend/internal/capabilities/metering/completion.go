package metering

import (
	"context"
	"errors"
	"time"
)

type TrafficRecord struct {
	ID                      uint      `json:"id"`
	UserID                  uint      `json:"user_id"`
	SubscriptionID          uint      `json:"subscription_id,omitempty"`
	NodeID                  uint      `json:"node_id"`
	ProtocolEndpointID      uint      `json:"protocol_endpoint_id"`
	ReportID                string    `json:"report_id,omitempty"`
	FlowID                  string    `json:"flow_id,omitempty"`
	EventType               string    `json:"event_type,omitempty"`
	EventRevision           uint64    `json:"event_revision,omitempty"`
	Nonce                   string    `json:"-"`
	RawBytes                int64     `json:"raw_bytes"`
	UploadBytes             int64     `json:"upload_bytes"`
	DownloadBytes           int64     `json:"download_bytes"`
	TrafficCalcMode         int16     `json:"traffic_calc_mode"`
	ProtocolMultiplierMilli int64     `json:"protocol_multiplier_milli"`
	UsedBytes               int64     `json:"used_bytes"`
	At                      time.Time `json:"record_at"`
	Meta                    string    `json:"meta"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type CredentialDecryptor interface{ Decrypt(string) (string, error) }

// CompletedFlow is submitted by an authenticated node-event adapter. Ownership
// is resolved from the authority database; callers cannot choose a subscription.
type CompletedFlow struct {
	NodeID                                                             uint
	SourceID, CoreInstanceID, EventID, FlowID, PrincipalKey, EventType string
	Sequence, Revision                                                 uint64
	BytesUp, BytesDown                                                 int64
	OccurredAt                                                         time.Time
}
type CompletionRepository interface {
	Complete(context.Context, CompletedFlow) (TrafficRecord, bool, error)
}
type CompletionAccounting struct{ Repository CompletionRepository }

func (s CompletionAccounting) Complete(ctx context.Context, in CompletedFlow) (TrafficRecord, bool, error) {
	if in.NodeID == 0 || in.EventType != "flow.completed" || in.EventID == "" || in.FlowID == "" || in.PrincipalKey == "" {
		return TrafficRecord{}, false, errors.New("invalid completed flow")
	}
	if in.BytesUp < 0 || in.BytesDown < 0 {
		return TrafficRecord{}, false, errors.New("negative completed flow counters")
	}
	return s.Repository.Complete(ctx, in)
}
