package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"time"
)

const principalFlowTrendReplayTimeout = 3 * time.Second

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
	replayContext, cancel := context.WithTimeout(r.Context(), principalFlowTrendReplayTimeout)
	defer cancel()
	baseline, events, boundaries, err := h.loadPrincipalFlowTrendTimeline(replayContext, scopeType, scopeID, from, to.AddDate(0, 0, 1))
	if err != nil {
		if errors.Is(replayContext.Err(), context.DeadlineExceeded) && r.Context().Err() == nil {
			copyRecordedResponse(w, recorded)
			return
		}
		ServerError(w, err)
		return
	}
	applyPrincipalFlowReplay(&response, from, baseline, events, boundaries)
	writeJSONResponse(w, http.StatusOK, wire.Message, response, wire.Error)
}

func (h *handlers) loadPrincipalFlowTrendTimeline(ctx context.Context, scopeType string, scopeID uint, from, end time.Time) ([]principalFlowHistoryRow, []principalFlowHistoryRow, []principalFlowBoundaryRow, error) {
	column, err := principalFlowScopeColumn(scopeType)
	if err != nil {
		return nil, nil, nil, err
	}

	baselineSQL := principalFlowBaselineSQL(column)
	var baseline []principalFlowHistoryRow
	db := h.db.WithContext(ctx)
	if err := db.Raw(baselineSQL, scopeType, scopeID, from, scopeID, from).Scan(&baseline).Error; err != nil {
		return nil, nil, nil, err
	}

	var events []principalFlowHistoryRow
	if err := db.Table("principal_flow_observations").
		Select("id, node_id, core_instance_id, session_registry_revision, principal_key, active_flows, observed_at").
		Where(column+" = ? AND observed_at >= ? AND observed_at < ?", scopeID, from, end).
		Find(&events).Error; err != nil {
		return nil, nil, nil, err
	}

	// The baseline query has already discarded generations closed before the
	// interval. Only boundaries inside the requested range can affect replay.
	var boundaries []principalFlowBoundaryRow
	if err := db.Table("principal_flow_scope_observations").
		Select("id, node_id, core_instance_id, source, observed_at").
		Where("scope_type = ? AND scope_id = ? AND source = ? AND observed_at >= ? AND observed_at < ?", scopeType, scopeID, "generation_reset", from, end).
		Find(&boundaries).Error; err != nil {
		return nil, nil, nil, err
	}

	return baseline, events, boundaries, nil
}

func principalFlowBaselineSQL(column string) string {
	return fmt.Sprintf(`
WITH latest_boundaries AS (
  SELECT node_id, core_instance_id, observed_at
  FROM (
    SELECT node_id, core_instance_id, observed_at,
           ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY observed_at DESC, id DESC) AS boundary_rank
    FROM principal_flow_scope_observations
    WHERE scope_type = ? AND scope_id = ? AND source = 'generation_reset' AND observed_at < ?
  ) boundary_rows
  WHERE boundary_rank = 1
), ranked_observations AS (
  SELECT o.id, o.node_id, o.core_instance_id, o.session_registry_revision,
         o.principal_key, o.active_flows, o.observed_at,
         ROW_NUMBER() OVER (
           PARTITION BY o.node_id, o.principal_key
           ORDER BY o.observed_at DESC, o.session_registry_revision DESC, o.id DESC
         ) AS observation_rank
  FROM principal_flow_observations o
  LEFT JOIN latest_boundaries boundary ON boundary.node_id = o.node_id
  WHERE o.%[1]s = ? AND o.observed_at < ?
    AND (
      boundary.node_id IS NULL
      OR o.observed_at > boundary.observed_at
      OR (o.observed_at = boundary.observed_at AND o.core_instance_id = boundary.core_instance_id)
    )
)
SELECT id, node_id, core_instance_id, session_registry_revision,
       principal_key, active_flows, observed_at
FROM ranked_observations
WHERE observation_rank = 1 AND active_flows > 0`, column)
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
