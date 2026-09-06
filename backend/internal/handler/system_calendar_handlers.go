package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"time"
)

// TrafficTrendsSystemCalendarHandler keeps the existing trend contract while
// interpreting date-only filters and daily buckets in the configured system
// timezone. Absolute timestamps remain UTC in storage and API facts.
func (h *handlers) TrafficTrendsSystemCalendarHandler(w http.ResponseWriter, r *http.Request) {
	h.trafficTrends(w, r, true)
}

// TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler preserves the
// Principal-flow replay added by the existing endpoint while using system-local
// day boundaries instead of fixed UTC days.
func (h *handlers) TrafficTrendsSystemCalendarWithPrincipalFlowReplayHandler(w http.ResponseWriter, r *http.Request) {
	recorded := httptest.NewRecorder()
	h.TrafficTrendsSystemCalendarHandler(recorded, r)
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

	location := h.systemTimezoneLocation()
	from, err := parseSystemDate(response.From, location)
	if err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	to, err := parseSystemDate(response.To, location)
	if err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	days, err := inclusiveSystemDayCount(from, to, trafficTrendMaxDays)
	if err != nil {
		copyRecordedResponse(w, recorded)
		return
	}
	buckets := systemDayBuckets(from, days, location)
	if len(buckets) == 0 {
		copyRecordedResponse(w, recorded)
		return
	}
	start := buckets[0].StartUTC
	end := buckets[len(buckets)-1].EndUTC
	replayContext, cancel := context.WithTimeout(r.Context(), principalFlowTrendReplayTimeout)
	defer cancel()
	baseline, events, boundaries, err := h.loadPrincipalFlowTrendTimeline(replayContext, scopeType, scopeID, start, end)
	if err != nil {
		if errors.Is(replayContext.Err(), context.DeadlineExceeded) && r.Context().Err() == nil {
			copyRecordedResponse(w, recorded)
			return
		}
		ServerError(w, err)
		return
	}
	applyPrincipalFlowReplayInBuckets(&response, buckets, baseline, events, boundaries)
	writeJSONResponse(w, http.StatusOK, wire.Message, response, wire.Error)
}

func applyPrincipalFlowReplayInBuckets(response *trafficTrendResponse, buckets []systemCalendarBucket, baseline, events []principalFlowHistoryRow, boundaries []principalFlowBoundaryRow) {
	if response == nil || len(response.Points) == 0 || len(buckets) == 0 {
		return
	}
	limit := len(response.Points)
	if len(buckets) < limit {
		limit = len(buckets)
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
			Boundary: true, ID: boundary.ID, NodeID: boundary.NodeID,
			CoreID: boundary.CoreInstanceID, ObservedAt: boundary.ObservedAt.UTC(),
		})
	}
	for _, row := range events {
		timeline = append(timeline, principalFlowTimelineItem{
			ID: row.ID, NodeID: row.NodeID, Principal: row.PrincipalKey,
			CoreID: row.CoreInstanceID, Revision: row.SessionRegistryRevision,
			ActiveFlows: row.ActiveFlows, ObservedAt: row.ObservedAt.UTC(),
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
			return a.Boundary
		}
		if !a.Boundary && a.CoreID == b.CoreID && a.Revision != b.Revision {
			return a.Revision < b.Revision
		}
		return a.ID < b.ID
	})

	peaks := make([]*int64, limit)
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
	bucketIndex := 0
	if covered {
		setPeak(0, current)
	}
	var sampleCount int64
	for _, item := range timeline {
		for bucketIndex+1 < limit && !item.ObservedAt.Before(buckets[bucketIndex].EndUTC) {
			bucketIndex++
			if covered {
				setPeak(bucketIndex, current)
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
		setPeak(bucketIndex, current)
	}
	for bucketIndex+1 < limit {
		bucketIndex++
		if covered {
			setPeak(bucketIndex, current)
		}
	}

	var overall *int64
	for index := 0; index < limit; index++ {
		response.Points[index].PeakConnections = peaks[index]
		if peaks[index] != nil && (overall == nil || *peaks[index] > *overall) {
			value := *peaks[index]
			overall = &value
		}
	}
	response.ConnectionSampleCount = sampleCount
	response.PeakConnections = overall
}

// DashboardOverviewSystemCalendarHandler applies the configured IANA timezone
// to the dashboard's "today", 7-day, 30-day, and trend bucket boundaries.
func (h *handlers) DashboardOverviewSystemCalendarHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	now := time.Now().UTC()
	location := h.systemTimezoneLocation()
	period, err := resolveDashboardPeriodInLocation(r.URL.Query().Get("range"), now, location)
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	business, err := h.loadDashboardBusiness(period, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	service, coverage, err := h.loadDashboardService(period, now)
	if err != nil {
		ServerError(w, err)
		return
	}
	subscriptions, err := h.loadDashboardSubscriptionHealth(now)
	if err != nil {
		ServerError(w, err)
		return
	}
	attention, infrastructure, err := h.loadDashboardOperationalHealth(now)
	if err != nil {
		ServerError(w, err)
		return
	}
	trend, err := h.loadDashboardTrendInLocation(period, location)
	if err != nil {
		ServerError(w, err)
		return
	}

	OK(w, dashboardOverviewResponse{
		Period:         period,
		Business:       business,
		Service:        service,
		Subscriptions:  subscriptions,
		Attention:      attention,
		Infrastructure: infrastructure,
		Coverage:       coverage,
		Trend:          trend,
		AsOf:           now,
	})
}
