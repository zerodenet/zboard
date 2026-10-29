package entitlements

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCredentialRetirementBudgetClassificationDoesNotHideFundingFailure(t *testing.T) {
	funding := errors.New("real funding conflict")
	mutable := NewCredentialExpiryMutableBudgetError(funding)
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"identity", ErrCredentialExpiryScopeBudget, "order_identity_budget"},
		{"wrapped identity", fmt.Errorf("source: %w", ErrCredentialExpiryScopeBudget), "order_identity_budget"},
		{"mutable", mutable, "mutable_order_budget"},
		{"wrapped mutable", fmt.Errorf("source: %w", mutable), "mutable_order_budget"},
		{"ordinary funding", funding, ""},
		{"wrapped ordinary", fmt.Errorf("source: %w", funding), ""},
		{"joined database failure", errors.Join(ErrCredentialExpiryScopeBudget, errors.New("database write failed")), ""},
		{"joined mutable failure", errors.Join(mutable, funding), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := CredentialExpiryBudgetReason(test.err); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
	if !errors.Is(mutable, funding) {
		t.Fatal("existing funding error identity lost")
	}
}

func TestSuccessfulGrantClearsOperationalRetirementRetry(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	retry := now.Add(time.Hour)
	original := Subscription{ID: 1, UserID: 1, PlanID: 1, PlanSKUID: 1, Lifecycle: "renewable", Status: "active", StartAt: now.AddDate(0, -1, 0), EndAt: now.AddDate(0, 1, 0), FlowTotal: 100, FlowUsed: 30, ResetQuotaBytes: 100, ResetPolicy: 2, RetirementRetryAt: &retry}
	req := GrantRequest{UserID: 1, PlanID: 1, PlanSKUID: 1, OrderType: "renewal", BillingUnit: "month", BillingValue: 1, TrafficBytes: 100, RenewalEffect: "extend_only"}
	policy := GrantPolicy{IsRenewable: true, ResetPolicy: 2}
	for _, kind := range []string{"early renewal", "recovery", "traffic reset", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			sub, request := original, req
			if kind == "recovery" {
				sub.EndAt = now.Add(-time.Hour)
			}
			if kind == "traffic reset" {
				request.OrderType = "traffic_reset"
				request.RenewalEffect = "none"
			}
			if kind == "replacement" {
				request.OrderType = "upgrade"
				request.ReplaceTerm = true
			}
			grant, err := ApplyGrant(sub, request, policy, now)
			if err != nil || grant.Subscription.RetirementRetryAt != nil {
				t.Fatal("successful grant retained stale retry", err, grant.Subscription.RetirementRetryAt)
			}
			if original.RetirementRetryAt == nil {
				t.Fatal("input mutated")
			}
		})
	}
}
