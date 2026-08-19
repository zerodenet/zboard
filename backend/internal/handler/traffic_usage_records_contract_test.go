package handler

import (
	"strings"
	"testing"
)

func TestTrafficUsageBucketSupportsMinuteAndHourPrecision(t *testing.T) {
	minute, err := parseTrafficUsageBucket("minute")
	if err != nil {
		t.Fatal(err)
	}
	if minute.Name != trafficUsageBucketMinute || !strings.Contains(minute.Expression, "%Y-%m-%d %H:%i:00") {
		t.Fatalf("minute bucket = %#v", minute)
	}

	hour, err := parseTrafficUsageBucket("hour")
	if err != nil {
		t.Fatal(err)
	}
	if hour.Name != trafficUsageBucketHour || !strings.Contains(hour.Expression, "%Y-%m-%d %H:00:00") {
		t.Fatalf("hour bucket = %#v", hour)
	}

	if _, err := parseTrafficUsageBucket("day"); err == nil {
		t.Fatal("unsupported bucket should fail")
	}
}

func TestTrafficUsageBucketMatchesHumanFacingBillingDimensions(t *testing.T) {
	// Aggregated rows intentionally omit protocol_endpoint_id. Endpoint identity
	// remains available on raw records and as a pre-aggregation filter, while the
	// user-facing grouping is time + subscription + node + multiplier (and user
	// in the administrative scope).
	var _ = trafficUsageBucket{
		ID:                      1,
		UserID:                  2,
		SubscriptionID:          3,
		NodeID:                  4,
		ProtocolMultiplierMilli: 1000,
		RecordCount:             2,
	}
}
