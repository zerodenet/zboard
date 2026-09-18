package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	fairUseStateNormal    = "normal"
	fairUseStateSuspected = "suspected"
	fairUseStateViolated  = "violated"

	fairUsePolicyScopePlatform     = "platform"
	fairUsePolicyScopePlan         = "plan"
	fairUsePolicyScopeSubscription = "subscription"

	fairUseEnforcementObserve  = "observe"
	fairUseEnforcementWarn     = "warn"
	fairUseEnforcementRestrict = "restrict"

	fairUseEvaluationWorkerInterval       = 15 * time.Second
	fairUseEvaluationBatchSize            = 100
	fairUseDefaultEvaluationInterval      = 60
	fairUseMinEvaluationInterval          = 30
	fairUseMaxEvaluationInterval          = 3600
	fairUseMinRestrictionDurationSeconds  = 60
	fairUseMaxRestrictionDurationSeconds  = 7 * 24 * 60 * 60
	fairUseDefaultRestrictionDurationSecs = 60 * 60
)

type fairUsePolicy = meteringstore.PolicyRecord

type fairUsePolicySource = metering.PolicySource

type subscriptionFairUseState = meteringstore.StateRecord
type subscriptionFairUseEvent = meteringstore.EventRecord
type fairUseEventView = metering.Event

type fairUsePolicyInput = metering.PolicyInput

type fairUseEvaluationResult = metering.EvaluationResult

func defaultFairUsePolicy(scope string, id uint) fairUsePolicy {
	return fairUsePolicy(metering.DefaultPolicy(scope, id))
}
func validateFairUsePolicy(input fairUsePolicyInput) error { return metering.ValidatePolicy(input) }

func parseFairUseResourceID(path, prefix, suffix string) (uint, error) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return 0, errors.New("invalid Fair Use path")
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if raw == "" || strings.Contains(raw, "/") {
		return 0, errors.New("invalid resource id")
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid resource id")
	}
	id := uint(value)
	if uint64(id) != value {
		return 0, errors.New("resource id is out of range")
	}
	return id, nil
}

func parseFairUseResourceSubscriptionID(path, suffix string) (uint, error) {
	return parseFairUseResourceID(path, "/api/v1/admin/subscriptions/", suffix)
}

func parseFairUseResourcePlanID(path, suffix string) (uint, error) {
	return parseFairUseResourceID(path, "/api/v1/admin/plans/", suffix)
}

func fairUsePolicySourceOf(p fairUsePolicy) fairUsePolicySource {
	return metering.PolicySourceOf(metering.Policy(p))
}
func chooseFairUsePolicy(sub, plan, platform *fairUsePolicy) (fairUsePolicy, fairUsePolicySource) {
	convert := func(p *fairUsePolicy) *metering.Policy {
		if p == nil {
			return nil
		}
		v := metering.Policy(*p)
		return &v
	}
	p, source := metering.ChoosePolicy(convert(sub), convert(plan), convert(platform))
	return fairUsePolicy(p), source
}

func decodeFairUsePolicyInput(r *http.Request) (fairUsePolicyInput, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, nodeReportMaxBodyBytes+1))
	if err != nil || len(body) == 0 || len(body) > nodeReportMaxBodyBytes {
		return fairUsePolicyInput{}, errors.New("invalid Fair Use policy body")
	}
	var input fairUsePolicyInput
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fairUsePolicyInput{}, errors.New("invalid Fair Use policy JSON")
	}
	return input, nil
}

func (h *handlers) AdminSubscriptionFairUseEvaluateHandler(w http.ResponseWriter, r *http.Request) {
	actor, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	id, err := parseFairUseResourceSubscriptionID(r.URL.Path, "/fair-use/evaluate")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.backgroundJobs()
	receipt, err := h.services.FairUseEvaluationRequests.Enqueue(r.Context(), actor.UserID, id)
	if err != nil {
		writeFairUsePolicyError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, "evaluation queued", receipt)
}

func fairUseRiskState(score int, p fairUsePolicy) string {
	return metering.RiskState(score, metering.Policy(p))
}
func fairUseScoreTransition(p fairUsePolicy, score int, starts, nodes int64) (int, string, string) {
	return metering.ScoreTransition(metering.Policy(p), score, starts, nodes)
}

func fairUseEvaluationIsDue(last *time.Time, interval int, now time.Time) bool {
	return metering.EvaluationDue(last, interval, now)
}

func (h *handlers) StartFairUseEvaluationWorker() {
	h.startScheduledJob("fair_use", fairUseEvaluationWorkerInterval, func(ctx context.Context) error { return h.runFairUseEvaluationCycleContext(ctx, time.Now().UTC()) })
}

func (h *handlers) CloseFairUseEvaluationWorker() { h.closeScheduledJob("fair_use") }

func (h *handlers) fairUseEvaluationCandidateIDs(now time.Time) ([]uint, error) {
	return h.services.FairUseEvaluationSource.Candidates(context.Background(), now)
}

func (h *handlers) runFairUseEvaluationCycleContext(ctx context.Context, now time.Time) error {
	if h.backgroundWorkPaused() {
		return nil
	}
	subscriptionIDs, err := h.services.FairUseEvaluationSource.Candidates(ctx, now)
	if err != nil {
		log.Printf("fair use evaluation policy scan failed: %v", err)
		return err
	}
	var failures []error
	for _, subscriptionID := range subscriptionIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		result, err := h.services.FairUseEvaluator.Evaluate(ctx, subscriptionID, now)
		if err != nil {
			log.Printf("fair use evaluation failed: subscription_id=%d error=%v", subscriptionID, err)
			failures = append(failures, err)
			continue
		}
		if result.Evaluated && result.State.State != fairUseStateNormal {
			log.Printf("fair use evaluation state: subscription_id=%d state=%s score=%d source=%s:%d reason=%s", subscriptionID, result.State.State, result.State.Score, result.PolicySource.ScopeType, result.PolicySource.ScopeID, result.Reason)
		}
	}
	return errors.Join(failures...)
}
