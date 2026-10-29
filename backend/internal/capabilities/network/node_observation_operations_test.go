package network

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestObservationIntentCursorIsVersionedCanonicalAndStatusBound(t *testing.T) {
	at := time.Date(2026, 10, 21, 1, 2, 3, 456000000, time.UTC)
	id := strings.Repeat("a", 64)
	cursor, err := ObservationIntentNextCursor("held", at, id)
	if err != nil {
		t.Fatal(err)
	}
	q, p, err := (ObservationIntentQuery{Cursor: cursor}).Parse()
	if err != nil || p == nil || q.Status != "held" || q.Limit != 50 || p.ID != id || !p.AvailableAt.Equal(at) {
		t.Fatal(q, p, err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(cursor)
	for name, value := range map[string]string{
		"version":        strings.Replace(string(raw), `"v":1`, `"v":2`, 1),
		"unknown":        strings.TrimSuffix(string(raw), "}") + `,"extra":1}`,
		"duplicate":      strings.TrimSuffix(string(raw), "}") + `,"v":1}`,
		"space":          " " + string(raw),
		"notUTC":         strings.Replace(string(raw), "01:02:03.456Z", "02:02:03.456+01:00", 1),
		"submillisecond": strings.Replace(string(raw), ".456Z", ".456001Z", 1),
		"zero":           strings.Replace(string(raw), at.Format(time.RFC3339Nano), "0001-01-01T00:00:00Z", 1),
		"id":             strings.Replace(string(raw), id, strings.Repeat("A", 64), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := (ObservationIntentQuery{Cursor: base64.RawURLEncoding.EncodeToString([]byte(value))}).Parse()
			if !errors.Is(err, ErrObservationIntentQuery) {
				t.Fatal("accepted invalid cursor", err)
			}
		})
	}
	for _, q := range []ObservationIntentQuery{{Status: "pending", Cursor: cursor}, {Status: "all"}, {Status: "HELD"}, {Limit: 101}, {Limit: -1}, {Cursor: "%"}, {Cursor: cursor + "="}, {Cursor: cursor[:10] + "\n" + cursor[10:]}, {Cursor: strings.Repeat("a", 513)}} {
		if _, _, err := q.Parse(); !errors.Is(err, ErrObservationIntentQuery) {
			t.Fatal("accepted invalid query", q, err)
		}
	}
	for _, status := range []string{"pending", "claimed", "applied", "held"} {
		if _, _, err := (ObservationIntentQuery{Status: status, Limit: 100}).Parse(); err != nil {
			t.Fatal(status, err)
		}
	}
}

type observationIntentRepositoryProbe struct{ called bool }

func (p *observationIntentRepositoryProbe) ListObservationIntents(context.Context, uint, ObservationIntentQuery) (ObservationIntentPage, error) {
	p.called = true
	return ObservationIntentPage{}, nil
}
func TestObservationIntentInvalidQueryDoesNotReachRepository(t *testing.T) {
	probe := &observationIntentRepositoryProbe{}
	service := ObservationIntentOperations{Repository: probe}
	if _, err := service.List(context.Background(), 1, ObservationIntentQuery{Status: "all"}); !errors.Is(err, ErrObservationIntentQuery) || probe.called {
		t.Fatal(probe.called, err)
	}
	if _, err := service.List(context.Background(), 0, ObservationIntentQuery{}); !errors.Is(err, ErrNodeAdministrationPermission) || probe.called {
		t.Fatal(probe.called, err)
	}
}
