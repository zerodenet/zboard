package handler

import (
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestBuildTrafficTrendPointsFillsMissingDates(t *testing.T) {
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	points, count := buildTrafficTrendPoints(from, 3, []trafficTrendAggregateRow{
		{Day: "2026-08-01", UploadBytes: 10, DownloadBytes: 20, UsedBytes: 30, RecordCount: 2},
		{Day: "2026-08-03", UploadBytes: 40, DownloadBytes: 50, UsedBytes: 90, RecordCount: 1},
	})

	if count != 3 {
		t.Fatalf("record count = %d, want 3", count)
	}
	if len(points) != 3 {
		t.Fatalf("point count = %d, want 3", len(points))
	}
	if points[1].Date != "2026-08-02" || points[1].UsedBytes != 0 || points[1].RecordCount != 0 {
		t.Fatalf("missing date was not zero-filled: %+v", points[1])
	}
	if points[0].PeakConnections != nil || points[2].PeakConnections != nil {
		t.Fatal("traffic records must not invent connection samples")
	}
}

func TestNormalizeEntityKindKeepsAliasesConsistent(t *testing.T) {
	cases := map[string]string{
		"subscription":       "subscription",
		"subscriptions":      "subscription",
		"endpoint":           "protocol_endpoint",
		"protocol_endpoints": "protocol_endpoint",
		"sku":                "plan_sku",
		"plan-skus":          "plan_sku",
	}
	for input, want := range cases {
		if got := normalizeEntityKind(input); got != want {
			t.Fatalf("normalizeEntityKind(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMissingEntityReferencePreservesIdentityAsSecondaryMetadata(t *testing.T) {
	got := missingEntityReference("subscription", 42)
	want := entityReference{
		ID:          42,
		Kind:        "subscription",
		DisplayName: "已删除的订阅",
		Secondary:   "名称不可用",
		Missing:     true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing reference = %#v, want %#v", got, want)
	}
}

func TestParseTrafficTrendRangeRejectsUnboundedQueries(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	values := url.Values{"from": {"2025-01-01"}, "to": {"2026-08-07"}}
	if _, _, _, err := parseTrafficTrendRange(values, now); err == nil {
		t.Fatal("expected an oversized trend range to be rejected")
	}
}
