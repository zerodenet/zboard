package handler

import (
	"net/url"
	"testing"
	"time"
)

func TestHistoryCursorRoundTrip(t *testing.T) {
	key := historyKey{At: time.Date(2026, 7, 22, 8, 9, 10, 123000000, time.UTC), ID: 42, Source: "task"}
	encoded, err := encodeHistoryCursor(key, historyDirectionOlder)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeHistoryCursor(encoded, map[string]struct{}{"task": {}})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Direction != historyDirectionOlder || !decoded.At.Equal(key.At) || decoded.ID != key.ID || decoded.Source != key.Source {
		t.Fatalf("unexpected decoded cursor: %#v", decoded)
	}
	if _, err := decodeHistoryCursor(encoded, map[string]struct{}{"node_kernel": {}}); err == nil {
		t.Fatal("expected an incompatible source to be rejected")
	}
	if _, err := decodeHistoryCursor("not-base64", nil); err == nil {
		t.Fatal("expected malformed cursor to be rejected")
	}
}

func TestHistoryWindowUsesInclusiveDateEnd(t *testing.T) {
	window, err := parseHistoryWindowAt(url.Values{
		"from": {"2026-07-01"},
		"to":   {"2026-07-03"},
	}, 30, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	if !window.From.Equal(wantFrom) || !window.To.Equal(wantTo) {
		t.Fatalf("unexpected window: %#v", window)
	}
}

func TestHistoryWindowDefaultsAndBounds(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	window, err := parseHistoryWindowAt(url.Values{}, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := window.From.Format("2006-01-02"), "2026-07-16"; got != want {
		t.Fatalf("default from = %s, want %s", got, want)
	}
	if got, want := window.To.Format("2006-01-02"), "2026-07-23"; got != want {
		t.Fatalf("default to = %s, want %s", got, want)
	}
	if _, err := parseHistoryWindowAt(url.Values{
		"from": {"2025-01-01"},
		"to":   {"2026-07-22"},
	}, 30, now); err == nil {
		t.Fatal("expected an oversized history window to be rejected")
	}
}

func TestOptionalDateWindowRequiresPairAndUsesInclusiveEnd(t *testing.T) {
	window, present, err := parseOptionalDateWindow(url.Values{}, "created_from", "created_to", 366)
	if err != nil || present || !window.From.IsZero() || !window.To.IsZero() {
		t.Fatalf("empty optional window = %#v, %v, %v", window, present, err)
	}
	window, present, err = parseOptionalDateWindow(url.Values{
		"created_from": {"2026-07-01"},
		"created_to":   {"2026-07-03"},
	}, "created_from", "created_to", 366)
	if err != nil || !present {
		t.Fatalf("parse optional window: %#v, %v, %v", window, present, err)
	}
	if got, want := window.From.Format(time.RFC3339), "2026-07-01T00:00:00Z"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if got, want := window.To.Format(time.RFC3339), "2026-07-04T00:00:00Z"; got != want {
		t.Fatalf("to = %s, want %s", got, want)
	}
	if _, _, err := parseOptionalDateWindow(url.Values{"created_from": {"2026-07-01"}}, "created_from", "created_to", 366); err == nil {
		t.Fatal("expected an incomplete optional date pair to be rejected")
	}
	if _, _, err := parseOptionalDateWindow(url.Values{
		"created_from": {"2025-01-01"},
		"created_to":   {"2026-07-03"},
	}, "created_from", "created_to", 366); err == nil {
		t.Fatal("expected an oversized optional date window to be rejected")
	}
}

func TestHistoryPageCursorDirections(t *testing.T) {
	first := historyKey{At: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), ID: 20}
	last := historyKey{At: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC), ID: 10}
	next, previous, err := historyPageCursorValues(first, last, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || previous != nil {
		t.Fatalf("first page cursors = next %v, previous %v", next, previous)
	}
	requested := &historyCursor{Direction: historyDirectionOlder}
	next, previous, err = historyPageCursorValues(first, last, requested, false)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil || previous == nil {
		t.Fatalf("last older page cursors = next %v, previous %v", next, previous)
	}
	requested.Direction = historyDirectionNewer
	next, previous, err = historyPageCursorValues(first, last, requested, true)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || previous == nil {
		t.Fatalf("middle newer page cursors = next %v, previous %v", next, previous)
	}
}
