package metering

import (
	"encoding/json"
	"errors"
	"time"
)

type EvaluationObservation struct {
	CurrentActiveFlows             *uint64
	ConnectionStarts, WorkingNodes int64
	Completeness, CoverageReason   string
	Metrics                        json.RawMessage
}
type EvaluationChange struct {
	State              State
	Event              *Event
	Evaluated, Skipped bool
	Reason             string
}

func EvaluationDue(last *time.Time, interval int, now time.Time) bool {
	return last == nil || !last.Add(time.Duration(interval)*time.Second).After(now)
}

// TransitionEvaluation freezes the score when telemetry is incomplete. It also
// records observation time so incomplete samples cannot cause a tight retry loop.
func TransitionEvaluation(policy Policy, state State, observation EvaluationObservation, now time.Time) EvaluationChange {
	if !EvaluationDue(state.LastEvaluatedAt, policy.EvaluationIntervalSeconds, now) {
		return EvaluationChange{State: state, Skipped: true, Reason: "evaluation_interval_not_due"}
	}
	previous := state
	state.CurrentActiveFlows = observation.CurrentActiveFlows
	state.ConnectionStarts = int(observation.ConnectionStarts)
	state.WorkingNodes = int(observation.WorkingNodes)
	state.TelemetryCompleteness = observation.Completeness
	state.LastEvaluatedAt = &now
	state.UpdatedAt = now
	out := EvaluationChange{}
	eventType := ""
	if observation.Completeness != "complete" {
		out.Skipped = true
		out.Reason = "telemetry_" + observation.Completeness + ": " + observation.CoverageReason
		if previous.LastEvaluatedAt == nil || previous.TelemetryCompleteness != observation.Completeness {
			eventType = "coverage_changed"
		}
	} else {
		state.Score, state.State, out.Reason = ScoreTransition(policy, state.Score, observation.ConnectionStarts, observation.WorkingNodes)
		state.LastCompleteAt = &now
		out.Evaluated = true
		switch {
		case previous.State != state.State:
			eventType = "state_changed"
		case state.Score > previous.Score:
			eventType = "risk_increased"
		case state.Score < previous.Score:
			eventType = "risk_recovered"
		case previous.TelemetryCompleteness != "complete":
			eventType = "coverage_restored"
		}
	}
	out.State = state
	if eventType != "" {
		out.Event = &Event{SubscriptionID: state.SubscriptionID, EventType: eventType, ScoreBefore: previous.Score, ScoreAfter: state.Score, StateBefore: previous.State, StateAfter: state.State, Metrics: observation.Metrics, Reason: out.Reason, OccurredAt: now}
	}
	return out
}

var ErrEvaluationPolicyChanged = errors.New("evaluation policy changed during sampling")

// SameEvaluationPolicy compares the complete selected override, including its
// creation identity, so delete/recreate at revision 1 cannot reuse an old sample.
func SameEvaluationPolicy(a, b Policy) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	a.CreatedAt = time.Time{}
	a.UpdatedAt = time.Time{}
	b.CreatedAt = time.Time{}
	b.UpdatedAt = time.Time{}
	return a == b
}
