package handler

import (
	"strings"
	"testing"
)

func TestTrafficUsageBucketExpressionUsesMinutePrecision(t *testing.T) {
	if !strings.Contains(trafficUsageBucketExpression, "%Y-%m-%d %H:%i:00") {
		t.Fatalf("usage bucket must truncate timestamps to minute precision: %s", trafficUsageBucketExpression)
	}
}

func TestTrafficUsageBucketJSONKeepsExistingTrafficRecordShape(t *testing.T) {
	// The human-facing bucket intentionally keeps the existing traffic-record
	// fields so current admin/account tables remain compatible while the backend
	// changes the read granularity. RecordCount is additive metadata for future
	// drill-down UI.
	var _ = trafficUsageBucket{
		ID:                      1,
		UserID:                  2,
		SubscriptionID:          3,
		NodeID:                  4,
		ProtocolEndpointID:      5,
		ProtocolMultiplierMilli: 1000,
		RecordCount:             2,
	}
}
