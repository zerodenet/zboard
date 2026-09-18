package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
	"time"
)

func TestFairUseEvaluationPersistenceAndAuthorizedQueries(t *testing.T) {
	h, token := newAnnouncementTestHandlers(t)
	actor, err := h.authFromRequest(announcementRequest("GET", "/", token, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.Subscription{UserID: actor.UserID, Config: "{}", Status: "active", EndAt: time.Now().Add(time.Hour)}
	if err = h.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	policy := metering.DefaultPolicy("platform", 0)
	policy.Enabled = true
	raw, _ := json.Marshal(policy)
	var input metering.PolicyInput
	_ = json.Unmarshal(raw, &input)
	saved, err := h.services.FairUsePolicies.Save(ctx, actor.UserID, metering.PolicyScope{Type: "platform"}, input)
	if err != nil {
		t.Fatal(err)
	}
	policy = saved.Effective
	initial, err := h.services.FairUseObservations.State(ctx, actor.UserID, sub.ID)
	if err != nil || initial.State != "normal" || initial.TelemetryCompleteness != "unknown" {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	observation := metering.EvaluationObservation{Completeness: "complete", ConnectionStarts: 1000, WorkingNodes: 5, Metrics: []byte(`{"sample":1}`)}
	change, err := h.services.ApplyFairUseEvaluation(ctx, sub.ID, policy, observation, now)
	if err != nil || !change.Evaluated || change.State.Score != 25 {
		t.Fatalf("apply: %+v %v", change, err)
	}
	repeated, err := h.services.ApplyFairUseEvaluation(ctx, sub.ID, policy, observation, now)
	if err != nil || !repeated.Skipped || repeated.State.Score != 25 {
		t.Fatalf("repeat: %+v %v", repeated, err)
	}
	observation.Completeness = "partial"
	change, err = h.services.ApplyFairUseEvaluation(ctx, sub.ID, policy, observation, now.Add(time.Minute))
	if err != nil || !change.Skipped || change.State.Score != 25 {
		t.Fatalf("partial telemetry: %+v %v", change, err)
	}
	events, err := h.services.FairUseObservations.Events(ctx, actor.UserID, sub.ID, 1)
	if err != nil || len(events) != 1 || events[0].EventType != "coverage_changed" {
		t.Fatalf("events: %+v %v", events, err)
	}
	// A policy changed after sampling must not consume the old sample or advance state.
	input.ExpectedRevision = policy.Revision
	input.Enabled = false
	disabled, err := h.services.FairUsePolicies.Save(ctx, actor.UserID, metering.PolicyScope{Type: "platform"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.services.ApplyFairUseEvaluation(ctx, sub.ID, policy, observation, now.Add(2*time.Minute)); !errors.Is(err, metering.ErrEvaluationPolicyChanged) {
		t.Fatalf("stale sample: %v", err)
	}
	skipped, err := h.services.FairUseEvaluator.Evaluate(ctx, sub.ID, now.Add(2*time.Minute))
	if err != nil || !skipped.Skipped || skipped.Reason != "policy_disabled" {
		t.Fatalf("disabled policy: %+v %v", skipped, err)
	}
	input.ExpectedRevision = disabled.Effective.Revision
	input.Enabled = true
	restored, err := h.services.FairUsePolicies.Save(ctx, actor.UserID, metering.PolicyScope{Type: "platform"}, input)
	if err != nil {
		t.Fatal(err)
	}
	policy = restored.Effective
	if err = h.db.Exec("CREATE TRIGGER reject_fair_event BEFORE INSERT ON subscription_fair_use_events BEGIN SELECT RAISE(ABORT, 'event rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	observation.Completeness = "complete"
	if _, err = h.services.ApplyFairUseEvaluation(ctx, sub.ID, policy, observation, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expected event write failure")
	}
	state, err := h.services.FairUseObservations.State(ctx, actor.UserID, sub.ID)
	if err != nil || state.Score != 25 || state.TelemetryCompleteness != "partial" {
		t.Fatalf("rollback: %+v %v", state, err)
	}
	// Delayed delivery must appear only in the receive-time evaluation window.
	event := subscriptionFlowStartEvent{SubscriptionID: sub.ID, UserID: actor.UserID, NodeID: 7, OccurredAt: now.Add(-time.Hour), ReceivedAt: now}
	if err = h.db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := h.services.FairUseTelemetry.Read(ctx, actor.UserID, sub.ID, 60, 300, now)
	if err != nil || metrics.ConnectionStarts.Count != 0 || metrics.ReceivedConnectionStarts.Count != 1 || metrics.WorkingNodes.Count != 0 || metrics.ReceivedWorkingNodes.Count != 1 {
		t.Fatalf("delayed telemetry: %+v %v", metrics, err)
	}
	credential := model.ProtocolCredential{NodeID: 7, UserID: actor.UserID, SubscriptionID: sub.ID, PrincipalKey: "collected-principal", CredentialID: "collected-credential", Secret: "opaque", Status: "revoked", ExpiresAt: now.Add(-time.Hour)}
	if err = h.db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	collected := metering.FlowStart{NodeID: 7, EventID: "collection-event", PrincipalKey: credential.PrincipalKey, OccurredAt: now.Add(-time.Hour), ReceivedAt: now}
	for attempt := 0; attempt < 2; attempt++ {
		if err = h.services.FairUseFlowCollection.Record(ctx, collected); err != nil {
			t.Fatal(err)
		}
	}
	collected.NodeID = 8
	collected.EventID = "other-node-event"
	if err = h.services.FairUseFlowCollection.Record(ctx, collected); err != nil {
		t.Fatal(err)
	}
	series, err := h.services.FairUseObservationSeries.Read(ctx, actor.UserID, sub.ID, "1d", now)
	if err != nil || series.TotalConnectionStarts != 2 || series.DistinctWorkingNodes != 1 || len(series.Buckets) != 289 {
		t.Fatalf("collection mapping/dedup/series: %+v %v", series, err)
	}
	if err = h.db.Model(&model.User{}).Where("id = ?", actor.UserID).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = h.services.FairUseObservations.Events(ctx, actor.UserID, sub.ID, 50); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("revoked reader: %v", err)
	}
	if _, err = h.services.FairUseTelemetry.Read(ctx, actor.UserID, sub.ID, 60, 300, now); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("revoked telemetry reader: %v", err)
	}
	if _, err = h.services.FairUseObservationSeries.Read(ctx, actor.UserID, sub.ID, "1d", now); !errors.Is(err, metering.ErrPolicyPermission) {
		t.Fatalf("revoked series reader: %v", err)
	}
}
