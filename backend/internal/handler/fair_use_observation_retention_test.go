package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/clause"
)

type observationRetentionProbe struct {
	rawErr error
	calls  int
}

func (p *observationRetentionProbe) PruneFlowStarts(context.Context, time.Time, int) (int64, error) {
	p.calls++
	return 0, p.rawErr
}
func (p *observationRetentionProbe) PruneEvaluationEvents(context.Context, time.Time, int) (int64, error) {
	p.calls++
	return 1, nil
}

type observationEvaluationProbe struct {
	scanErr, evaluateErr error
	scans, evaluations   int
}

func (p *observationEvaluationProbe) Candidates(context.Context, time.Time) ([]uint, error) {
	p.scans++
	return []uint{1}, p.scanErr
}
func (p *observationEvaluationProbe) Snapshot(context.Context, uint) (metering.EvaluationSnapshot, error) {
	p.evaluations++
	return metering.EvaluationSnapshot{}, p.evaluateErr
}

func TestFairUseObservationRetentionCycleIndependentFailuresRemainVisible(t *testing.T) {
	retentionErr := errors.New("raw maintenance failed")
	scanErr := errors.New("evaluation candidate scan failed")
	evaluateErr := errors.New("evaluation failed")
	for _, tc := range []struct {
		name                  string
		raw, scan, evaluation error
		wantEvaluations       int
	}{
		{"cleanup failure still evaluates", retentionErr, nil, nil, 1},
		{"scan failure still cleans", nil, scanErr, nil, 0},
		{"both failures remain joined", retentionErr, scanErr, nil, 0},
		{"evaluation failure after cleanup", nil, nil, evaluateErr, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newAnnouncementTestHandlers(t)
			retention := &observationRetentionProbe{rawErr: tc.raw}
			evaluation := &observationEvaluationProbe{scanErr: tc.scan, evaluateErr: tc.evaluation}
			h.services.FairUseObservationRetention = metering.ObservationRetention{Repository: retention}
			h.services.FairUseEvaluationSource = evaluation
			h.services.FairUseEvaluator = metering.Evaluator{Source: evaluation}
			finish := h.observeJob("fair_use", "periodic", fairUseEvaluationWorkerInterval, 1)
			err := h.runFairUseEvaluationCycleContext(context.Background(), time.Now().UTC())
			finish(err)
			if err == nil || retention.calls != 2 || evaluation.scans != 1 || evaluation.evaluations != tc.wantEvaluations {
				t.Fatalf("independent work skipped: %v retention=%d scans=%d evaluations=%d", err, retention.calls, evaluation.scans, evaluation.evaluations)
			}
			for _, want := range []error{tc.raw, tc.scan, tc.evaluation} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("failure not joined: want=%v got=%v", want, err)
				}
			}
			seen := false
			for _, job := range h.backgroundJobs().observations.Snapshot() {
				if job.ID == "fair_use" {
					seen = true
					if job.Failures != 1 || job.LastResult != "failed" || job.LastError == "" {
						t.Fatalf("maintenance/evaluation failure invisible: %+v", job)
					}
				}
			}
			if !seen {
				t.Fatal("fair_use task observation missing")
			}
		})
	}
}

func TestFairUseObservationRetentionDoesNotChangeCommittedAccountingOrEvaluation(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	assertObservationRetentionPreservesAccounting(t, h, credential)
}

func TestMySQLFairUseObservationRetentionDoesNotChangeCommittedAccountingOrEvaluation(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	assertObservationRetentionPreservesAccounting(t, h, credential)
}

func assertObservationRetentionPreservesAccounting(t *testing.T, h *handlers, credential model.ProtocolCredential) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	maintenanceNow := now.Add(16 * 24 * time.Hour)
	events := accountingBenchmarkEvents(credential, 0, 4)
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	collected := metering.FlowStart{NodeID: credential.NodeID, EventID: "retained-diagnostic-identity", CoreInstanceID: "diagnostic-core", PrincipalKey: credential.PrincipalKey, OccurredAt: now, ReceivedAt: now}
	if err := h.services.FairUseFlowCollection.Record(ctx, collected); err != nil {
		t.Fatal(err)
	}
	policy := metering.DefaultPolicy("platform", 0)
	policy.Enabled = true
	record := meteringstore.PolicyRecord(policy)
	if err := h.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	selected, err := h.services.FairUseEvaluationSource.Snapshot(ctx, credential.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	policy = selected.Policy
	observation := metering.EvaluationObservation{Completeness: "complete", ConnectionStarts: 1000, WorkingNodes: 5, Metrics: []byte(`{}`)}
	change, err := h.services.ApplyFairUseEvaluation(ctx, credential.SubscriptionID, policy, observation, now)
	if err != nil || !change.Evaluated || change.State.Score != 25 {
		t.Fatalf("evaluation did not commit: %+v %v", change, err)
	}
	protected := []string{"traffic_records", "flow_usages", "zero_event_node_cursors", "metering_usage_projection_pending", "protocol_endpoint_usage_daily", "subscriptions", "protocol_credentials", "orders", "audit_logs", "node_config_publishes", "subscription_fair_use_states"}
	snapshot := func() map[string]string {
		t.Helper()
		out := make(map[string]string)
		for _, table := range protected {
			var rows []map[string]any
			if err := h.db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(table, err)
			}
			data, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(table, err)
			}
			out[table] = string(data)
		}
		return out
	}
	for _, table := range []string{"subscription_flow_start_events", "subscription_fair_use_events"} {
		statement := "CREATE TRIGGER reject_" + table + " BEFORE DELETE ON " + table + " BEGIN SELECT RAISE(ABORT, 'observation retention unavailable'); END"
		if !datastore.IsSQLite(h.db) {
			statement = "CREATE TRIGGER reject_" + table + " BEFORE DELETE ON " + table + " FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'observation retention unavailable'"
		}
		if err := h.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	// New receive/evaluation transactions must not execute either DELETE trigger.
	collected.EventID = "newly-accepted-with-broken-retention"
	if err := h.services.FairUseFlowCollection.Record(ctx, collected); err != nil {
		t.Fatal("receive blocked by maintenance", err)
	}
	change, err = h.services.ApplyFairUseEvaluation(ctx, credential.SubscriptionID, policy, observation, now.Add(time.Minute))
	if err != nil || !change.Evaluated {
		t.Fatalf("evaluation blocked by maintenance: %+v %v", change, err)
	}
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 1, 4)); err != nil {
		t.Fatal("new authoritative settlement blocked by broken observation retention", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 240, subStatusActive)
	before := snapshot()
	result, err := h.services.FairUseObservationRetention.Run(ctx, maintenanceNow)
	if err == nil || result.FlowStarts != 0 || result.EvaluationEvents != 0 || !strings.Contains(err.Error(), "flow-start") || !strings.Contains(err.Error(), "evaluation-event") {
		t.Fatalf("maintenance failures hidden: %+v %v", result, err)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("failed maintenance changed committed accounting, state, queue, publication or business audit")
	}
	for _, table := range []string{"subscription_flow_start_events", "subscription_fair_use_events"} {
		if err := h.db.Exec("DROP TRIGGER reject_" + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err = h.services.FairUseObservationRetention.Run(ctx, maintenanceNow)
	if err != nil || result.FlowStarts != 2 || result.EvaluationEvents == 0 {
		t.Fatalf("retention did not recover: %+v %v", result, err)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("successful observation cleanup changed protected facts")
	}
	// The old diagnostic unique identity expires with its row. Late replay may
	// be counted again diagnostically; it must not create or alter billing facts.
	collected.ReceivedAt = maintenanceNow
	for i := 0; i < 2; i++ {
		if err := h.services.FairUseFlowCollection.Record(ctx, collected); err != nil {
			t.Fatal(err)
		}
		if err := h.projectZeroNodeEvents(ctx, events); err != nil {
			t.Fatal(err)
		}
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 240, subStatusActive)
	var observations int64
	if err := h.db.Model(&meteringstore.FlowStartRecord{}).Count(&observations).Error; err != nil || observations != 1 {
		t.Fatalf("diagnostic replay horizon changed: %d %v", observations, err)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatal("late replay after diagnostic cleanup changed protected accounting facts")
	}
}
