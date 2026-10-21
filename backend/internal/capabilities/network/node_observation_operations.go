package network

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrObservationIntentQuery = errors.New("invalid observation intent query")

// ObservationIntentQuery is intentionally global and single-status. Neither a
// node/reason scan nor raw retained material is part of this metadata endpoint.
type ObservationIntentQuery struct {
	Status string
	Cursor string
	Limit  int
}
type ObservationIntentPosition struct {
	AvailableAt time.Time
	ID          string
}
type observationIntentCursor struct {
	Version     int    `json:"v"`
	Status      string `json:"status"`
	AvailableAt string `json:"available_at"`
	ID          string `json:"id"`
}

func ObservationIntentStatus(status string) bool {
	switch status {
	case "held", "pending", "claimed", "applied":
		return true
	}
	return false
}
func observationIntentID(id string) bool {
	if len(id) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(decoded) == id
}
func (q ObservationIntentQuery) Parse() (ObservationIntentQuery, *ObservationIntentPosition, error) {
	if q.Status == "" {
		q.Status = "held"
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if !ObservationIntentStatus(q.Status) || q.Limit < 1 || q.Limit > 100 {
		return q, nil, ErrObservationIntentQuery
	}
	if q.Cursor == "" {
		return q, nil, nil
	}
	if len(q.Cursor) > 512 {
		return q, nil, ErrObservationIntentQuery
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(q.Cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != q.Cursor {
		return q, nil, ErrObservationIntentQuery
	}
	var cursor observationIntentCursor
	if err = json.Unmarshal(raw, &cursor); err != nil {
		return q, nil, ErrObservationIntentQuery
	}
	// Only the server's canonical form is accepted: rejects duplicate/unknown
	// fields, noncanonical encodings and extra JSON without a permissive fallback.
	canonical, err := json.Marshal(cursor)
	if err != nil || !bytes.Equal(raw, canonical) || cursor.Version != 1 || cursor.Status != q.Status || !observationIntentID(cursor.ID) {
		return q, nil, ErrObservationIntentQuery
	}
	at, err := time.Parse(time.RFC3339Nano, cursor.AvailableAt)
	if err != nil || at.IsZero() || at.Year() < 1970 || at.Year() > 9999 || at.Nanosecond()%int(time.Millisecond) != 0 || at.UTC().Format(time.RFC3339Nano) != cursor.AvailableAt {
		return q, nil, ErrObservationIntentQuery
	}
	return q, &ObservationIntentPosition{AvailableAt: at, ID: cursor.ID}, nil
}
func ObservationIntentNextCursor(status string, at time.Time, id string) (string, error) {
	cursor := observationIntentCursor{Version: 1, Status: status, AvailableAt: at.UTC().Format(time.RFC3339Nano), ID: id}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	if _, _, err = (ObservationIntentQuery{Status: status, Cursor: value, Limit: 1}).Parse(); err != nil {
		return "", err
	}
	return value, nil
}

// These are queue metadata times, not event/sample time or a freshness claim.
// A header/source reference is not verification of retained material integrity.
type ObservationIntentItem struct {
	ID                   string     `json:"id"`
	NodeID               string     `json:"node_id"`
	Status               string     `json:"status"`
	Reason               string     `json:"reason"`
	AssetRelation        string     `json:"asset_relation"`
	Attempts             string     `json:"attempts"`
	MaterialBytes        string     `json:"material_bytes"`
	PartCount            string     `json:"part_count"`
	EnqueuedAt           time.Time  `json:"enqueued_at"`
	AvailableAt          time.Time  `json:"available_at"`
	DispositionUpdatedAt time.Time  `json:"disposition_updated_at"`
	LeaseUntil           *time.Time `json:"lease_until,omitempty"`
	MaterialVerified     bool       `json:"material_verified"`
}
type ObservationIntentPage struct {
	Items      []ObservationIntentItem `json:"items"`
	Status     string                  `json:"status"`
	HasMore    bool                    `json:"has_more"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}
type ObservationIntentRepository interface {
	ListObservationIntents(context.Context, uint, ObservationIntentQuery) (ObservationIntentPage, error)
}
type ObservationIntentOperations struct{ Repository ObservationIntentRepository }

func (s ObservationIntentOperations) List(ctx context.Context, actor uint, q ObservationIntentQuery) (ObservationIntentPage, error) {
	if s.Repository == nil || actor == 0 {
		return ObservationIntentPage{}, ErrNodeAdministrationPermission
	}
	q, _, err := q.Parse()
	if err != nil {
		return ObservationIntentPage{}, err
	}
	return s.Repository.ListObservationIntents(ctx, actor, q)
}
