package metering

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrNodeReportCredentialChanged = errors.New("node report credential changed")
var ErrNodeReportNonceReplayed = errors.New("node report nonce replayed")
var ErrSubscriptionNotFound = errors.New("no active subscription")
var ErrProtocolEndpointUnavailable = errors.New("protocol endpoint is unavailable on this node")
var ErrNoBillableTraffic = errors.New("selected traffic direction contains no billable bytes")

// AuthenticatedNodeReport is supplied only after the node adapter has verified
// the signed request. ExpectedCredential is an opaque stored credential value
// for transaction-time rotation fencing; it is never an external request field.
type AuthenticatedNodeReport struct {
	NodeID, UserID, ProtocolEndpointID   uint
	ReportID, Nonce, Version, Meta       string
	ExpectedCredential                   string `json:"-"`
	Timestamp                            time.Time
	RawBytes, UploadBytes, DownloadBytes int64
}
type NodeReportResult struct {
	Record                    TrafficRecord
	Duplicate, QuotaExhausted bool
	FlowUsed, FlowTotal       int64
	SubscriptionEnd           time.Time
}
type NodeReportRepository interface {
	Record(context.Context, AuthenticatedNodeReport) (NodeReportResult, error)
}
type NodeReports struct{ Repository NodeReportRepository }

func (s NodeReports) Record(ctx context.Context, in AuthenticatedNodeReport) (NodeReportResult, error) {
	if err := ctx.Err(); err != nil {
		return NodeReportResult{}, err
	}
	in.ReportID = strings.TrimSpace(in.ReportID)
	valid := len(in.ReportID) >= 8 && len(in.ReportID) <= 64
	for _, c := range in.ReportID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c)) {
			valid = false
		}
	}
	if !valid || in.NodeID == 0 || in.UserID == 0 || in.ProtocolEndpointID == 0 || in.Nonce == "" || in.ExpectedCredential == "" || in.Timestamp.IsZero() || len(in.Version) > 64 {
		return NodeReportResult{}, errors.New("invalid authenticated node report")
	}
	if in.RawBytes < 0 || in.UploadBytes < 0 || in.DownloadBytes < 0 || in.RawBytes > 1<<50 || in.UploadBytes > 1<<50 || in.DownloadBytes > 1<<50 {
		return NodeReportResult{}, errors.New("invalid traffic byte values")
	}
	if in.UploadBytes == 0 && in.DownloadBytes == 0 {
		in.DownloadBytes = in.RawBytes
	}
	if in.UploadBytes == 0 && in.DownloadBytes == 0 {
		return NodeReportResult{}, ErrNoBillableTraffic
	}
	return s.Repository.Record(ctx, in)
}
