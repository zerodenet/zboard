package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"
)

type principalFlowHistoryRow struct {
	ID                      uint64    `gorm:"column:id"`
	NodeID                  uint      `gorm:"column:node_id"`
	CoreInstanceID          string    `gorm:"column:core_instance_id"`
	SessionRegistryRevision uint64    `gorm:"column:session_registry_revision"`
	PrincipalKey            string    `gorm:"column:principal_key"`
	ActiveFlows             uint64    `gorm:"column:active_flows"`
	ObservedAt              time.Time `gorm:"column:observed_at"`
}

type principalFlowBoundaryRow struct {
	ID             uint64    `gorm:"column:id"`
	NodeID         uint      `gorm:"column:node_id"`
	CoreInstanceID string    `gorm:"column:core_instance_id"`
	Source         string    `gorm:"column:source"`
	ObservedAt     time.Time `gorm:"column:observed_at"`
}

type principalFlowTimelineItem struct {
	Boundary    bool
	ID          uint64
	NodeID      uint
	Principal   string
	CoreID      string
	Revision    uint64
	ActiveFlows uint64
	ObservedAt  time.Time
}

type principalFlowStateKey struct {
	NodeID    uint
	Principal string
}

// TrafficTrendsWithPrincipalFlowReplayHandler enriches the existing traffic
// trend contract by replaying Core's absolute Principal observations in their
// observed state-transition order. Delivery order is intentionally irrelevant:
// Core documents that event sequence and SessionRegistry revision can diverge
// under concurrency.
func (h *handlers) TrafficTrendsWithPrincipalFlowReplayHandler(w http.ResponseWriter, r *http.Request) {
	recorded := httptest.NewRecorder()
	h.TrafficTrendsHandler(recorded, r)
	if recorded.Code != http.StatusOK {
		copyRecordedResponse(w, recorded)
		return
	}
	var wire struct {
		Code      int             `json:"code"`
		Message   string          `json:"message"`
		Data      json.RawMessage `json:"data"`
		Error     *APIError       `json:"error,omitempty"`
		Timestamp string          `json:"timestamp"`
	}
	if err := json.Unmarshal(recorded.Body.Bytes(), &wire); err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	var response trafficTrendResponse
	if err := json.Unmarshal(wire.Data, &response); err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	scopeType, scopeID, allowed, err := h.principalFlowTrendScope(r)
	if err != nil {
		ServerError(w, err)
		return
	}
	if !allowed || scopeID == 0 {
		copyRecordedResponse(w, recorded)
		return
	}
	from, err := time.Parse("2006-01-02", response.From)
	if err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	to, err := time.Parse("2006-01-02", response.To)
	if err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	baseline, events, boundaries, err := h.loadPrincipalFlowTrendTimeline(scopeType, scopeID, from, to.AddDate(0, 0, 1))
	if err != nil {
		ServerError(w, err)
		return
	}
	applyPrincipalFlowReplay(&response, from, baseline, events, boundaries)
	writeJSONResponse(w, http.StatusOK, wire.Message, response, wire.Error)
}

func (h *handlers) loadPrincipalFlowTrendTimeline(scopeType string, scopeID uint, from, end time.Time) ([]principalFlowHistoryRow, []principalFlowHistoryRow, []principalFlowBoundaryRow, error) {
	column, err := principalFlowScopeColumn(scopeType)
	if err != nil {
		return nil, nil, nil, err
	}

	// One state immediately before the requested interval for each node/principal
	// is enough to establish the interval baseline. NOT EXISTS keeps this query
	// compatible with MySQL variants without requiring window functions.
	baselineSQL := fmt.Sprintf(`
SELECT o.id, o.node_id, o.core_instance_id, o.session_registry_revision,
       o.principal_key, o.active_flows, o.observed_at
FROM principal_flow_observations o
WHERE o.%[1]s = ? AND o.observed_at < ?
  AND NOT EXISTS (
    SELECT 1 FROM principal_flow_observations newer
    WHERE newer.%[1]s = ?
      AND newer.node_id = o.node_id
      AND newer.principal_key = o.principal_key
      AND newer.observed_at < ?
      AND (
        newer.observed_at > o.observed_at
        OR (newer.observed_at = o.observed_at
            AND newer.core_instance_id = o.core_instance_id
            AND newer.session_registry_revision > o.session_registry_revision)
        OR (newer.observed_at = o.observed_at
            AND newer.core_instance_id = o.core_instance_id
            AND newer.session_registry_revision = o.session_registry_revision
            AND newer.id > o.id)
      )
  )`, column)
	var baseline []principalFlowHistoryRow
	if err := h.db.Raw(baselineSQL, scopeID, from, scopeID, from).Scan(&baseline).Error; err != nil {
		return nil, nil, nil, err
	}

	var events []principalFlowHistoryRow
	if err := h.db.Table("principal_flow_observations").
		Select("id, node_id, core_instance_id, session_registry_revision, principal_key, active_flows, observed_at").
		Where(column+" = ? AND observed_at >= ? AND observed_at < ?", scopeID, from, end).
		Find(&events).Error; err != nil {
		return nil, nil, nil, err
	}

	// Generation resets are already persisted by the current projection path.
	// They are timeline boundaries, not connection samples: when one occurs all
	// previously known Principal state for that node must be removed before new
	// generation observations at the same timestamp are applied.
	var boundaries []principalFlowBoundaryRow
	if err := h.db.Table("principal_flow_scope_observations").
		Select("id, node_id, core_instance_id, source, observed_at").
		Where("scope_type = ? AND scope_id = ? AND source <> ? AND observed_at < ?", scopeType, scopeID, "lifecycle", end).
		Find(&boundaries).Error; err != nil {
		return nil, nil, nil, err
	}

	baseline = filterPrincipalFlowBaselineAfterBoundaries(baseline, boundaries, from)
	return baseline, events, boundariesInRange(boundaries, from, end), nil
}

func principalFlowScopeColumn(scopeType string) (string, error) {
	switch scopeType {
	case principalFlowScopeUser:
		return "user_id", nil
	case principalFlowScopeSubscription:
		return "subscription_id", nil
	default:
		return "", fmt.Errorf("unsupported Principal flow scope %q", scopeType)
	}
}

func filterPrincipalFlowBaselineAfterBoundaries(baseline []principalFlowHistoryRow, boundaries []principalFlowBoundaryRow, from time.Time) []principalFlowHistoryRow {
	latest := make(map[uint]principalFlowBoundaryRow)
	for _, boundary := range boundaries {
		if !boundary.ObservedAt.Before(from) {
			continue
		}
		current, exists := latest[boundary.NodeID]
		if !exists || boundary.ObservedAt.After(current.ObservedAt) || (boundary.ObservedAt.Equal(current.ObservedAt) && boundary.ID > current.ID) {
			latest[boundary.NodeID] = boundary
		}
	}
	filtered := make([]principalFlowHistoryRow, 0, len(baseline))
	for _, row := range baseline {
		boundary, exists := latest[row.NodeID]
		if !exists {
			filtered = append(filtered, row)
			continue
		}
		if row.ObservedAt.After(boundary.ObservedAt) {
			filtered = append(filtered, row)
			continue
		}
		if row.ObservedAt.Equal(boundary.ObservedAt) && boundary.Source == "generation_reset" && strings.TrimSpace(row.CoreInstanceID) == strings.TrimSpace(boundary.CoreInstanceID) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func boundariesInRange(boundaries []principalFlowBoundaryRow, from, end time.Time) []principalFlowBoundaryRow {
	result := make([]principalFlowBoundaryRow, 0, len(boundaries))
	for _, boundary := range boundaries {
		if !boundary.ObservedAt.Before(from) && boundary.ObservedAt.Before(end) {
			result = append(result, boundary)
		}
	}
	return result
}

func applyPrincipalFlowReplay(response *trafficTrendResponse, from time.Time, baseline, events []principalFlowHistoryRow, boundaries []principalFlowBoundaryRow) {
	if response == nil || len(response.Points) == 0 {
		return
	}
	state := make(map[principalFlowStateKey]uint64, len(baseline))
	var current uint64
	covered := len(baseline) > 0
	for _, row := range baseline {
		key := principalFlowStateKey{NodeID: row.NodeID, Principal: row.PrincipalKey}
		previous := state[key]
		state[key] = row.ActiveFlows
		current = current - previous + row.ActiveFlows
	}

	timeline := make([]principalFlowTimelineItem, 0, len(events)+len(boundaries))
	for _, boundary := range boundaries {
		timeline = append(timeline, principalFlowTimelineItem{
			Boundary: true, ID: boundary.ID, NodeID: boundary.NodeID, CoreID: boundary.CoreInstanceID, ObservedAt: boundary.ObservedAt.UTC(),
		})
	}
	for _, row := range events {
		timeline = append(timeline, principalFlowTimelineItem{
			ID: row.ID, NodeID: row.NodeID, Principal: row.PrincipalKey, CoreID: row.CoreInstanceID,
			Revision: row.SessionRegistryRevision, ActiveFlows: row.ActiveFlows, ObservedAt: row.ObservedAt.UTC(),
		})
	}
	sort.SliceStable(timeline, func(left, right int) bool {
		a, b := timeline[left], timeline[right]
		if !a.ObservedAt.Equal(b.ObservedAt) {
			return a.ObservedAt.Before(b.ObservedAt)
		}
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.Boundary != b.Boundary {
			return a.Boundary // reset old generation before applying new state
		}
		if !a.Boundary && a.CoreID == b.CoreID && a.Revision != b.Revision {
			return a.Revision < b.Revision
		}
		return a.ID < b.ID
	})

	peaks := make([]*int64, len(response.Points))
	setPeak := func(index int, value uint64) {
		if index < 0 || index >= len(peaks) {
			return
		}
		converted := int64(value)
		if peaks[index] == nil || converted > *peaks[index] {
			copyValue := converted
			peaks[index] = &copyValue
		}
	}
	dayIndex := 0
	dayEnd := from.UTC().AddDate(0, 0, 1)
	if covered {
		setPeak(0, current)
	}
	var sampleCount int64
	for _, item := range timeline {
		for dayIndex+1 < len(response.Points) && !item.ObservedAt.Before(dayEnd) {
			dayIndex++
			dayEnd = dayEnd.AddDate(0, 0, 1)
			if covered {
				setPeak(dayIndex, current)
			}
		}
		if item.Boundary {
			for key, value := range state {
				if key.NodeID != item.NodeID {
					continue
				}
				current -= value
				delete(state, key)
			}
			covered = true
		} else {
			key := principalFlowStateKey{NodeID: item.NodeID, Principal: item.Principal}
			previous := state[key]
			state[key] = item.ActiveFlows
			current = current - previous + item.ActiveFlows
			covered = true
			sampleCount++
		}
		setPeak(dayIndex, current)
	}
	for dayIndex+1 < len(response.Points) {
		dayIndex++
		if covered {
			setPeak(dayIndex, current)
		}
	}

	var overall *int64
	for index := range response.Points {
		response.Points[index].PeakConnections = peaks[index]
		if peaks[index] != nil && (overall == nil || *peaks[index] > *overall) {
			value := *peaks[index]
			overall = &value
		}
	}
	response.ConnectionSampleCount = sampleCount
	response.PeakConnections = overall
}
