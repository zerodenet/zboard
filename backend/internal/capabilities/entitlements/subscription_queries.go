package entitlements

import (
	"context"
	"errors"
	"strings"
	"time"
)

type SubscriptionSummary struct {
	ID                uint       `json:"id"`
	UserID            uint       `json:"user_id"`
	UserEmail         string     `json:"user_email"`
	PlanID            uint       `json:"plan_id"`
	PlanName          string     `json:"plan_name"`
	PlanSKUID         uint       `json:"plan_sku_id"`
	SKUName           string     `json:"sku_name"`
	NodeGroupID       uint       `json:"node_group_id"`
	SubscriptionType  int16      `json:"subscription_type"`
	StartAt           time.Time  `json:"start_at"`
	EndAt             time.Time  `json:"end_at"`
	Status            string     `json:"status"`
	FlowTotal         int64      `json:"flow_total"`
	FlowUsed          int64      `json:"flow_used"`
	SpeedLimitMbps    int        `json:"speed_limit_mbps"`
	DeviceLimit       int        `json:"device_limit"`
	FamilyLimit       int        `json:"family_limit"`
	RenewalPriceMinor int64      `json:"renewal_price_minor"`
	ResetPolicy       int16      `json:"reset_policy"`
	NextResetAt       *time.Time `json:"next_reset_at"`
	TrafficCalcMode   int16      `json:"traffic_calc_mode"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
type SubscriptionDetail struct {
	SubscriptionSummary
	ActiveCredentialCount int64 `json:"active_credential_count"`
	TotalCredentialCount  int64 `json:"total_credential_count"`
}
type SubscriptionQuery struct {
	UserID, ID                         uint
	Search, Status, EligibleFor, Quota string
	From, To                           time.Time
	Offset, Limit                      int
	LegacyArray                        bool
}
type SubscriptionPage struct {
	Items         []SubscriptionSummary
	Legacy        []Subscription
	Total         int64
	Offset, Limit int
}
type SubscriptionQueriesRepository interface {
	List(context.Context, uint, bool, SubscriptionQuery) (SubscriptionPage, error)
	Detail(context.Context, uint, uint) (SubscriptionDetail, error)
}
type SubscriptionQueries struct{ Repository SubscriptionQueriesRepository }

func (s SubscriptionQueries) Owned(ctx context.Context, actor uint, q SubscriptionQuery) (SubscriptionPage, error) {
	q.UserID = actor
	q.Quota = ""
	q.From = time.Time{}
	q.To = time.Time{}
	if q.LegacyArray {
		q.Search = ""
	}
	return s.list(ctx, actor, false, q)
}
func (s SubscriptionQueries) Administrative(ctx context.Context, actor uint, q SubscriptionQuery) (SubscriptionPage, error) {
	return s.list(ctx, actor, true, q)
}
func (s SubscriptionQueries) list(ctx context.Context, actor uint, admin bool, q SubscriptionQuery) (SubscriptionPage, error) {
	if actor == 0 {
		return SubscriptionPage{}, ErrAccessPermission
	}
	q.Search = strings.TrimSpace(q.Search)
	q.Status = strings.TrimSpace(q.Status)
	q.EligibleFor = strings.TrimSpace(q.EligibleFor)
	q.Quota = strings.TrimSpace(q.Quota)
	fail := func(message string) (SubscriptionPage, error) {
		return SubscriptionPage{}, &QueryError{Message: message}
	}
	if len(q.Search) > 128 {
		return fail("q must not exceed 128 bytes")
	}
	switch q.Status {
	case "", "active", "expired", "canceled":
	default:
		return fail("invalid status")
	}
	switch q.EligibleFor {
	case "", "change", "addon", "manage", "renew":
	default:
		return fail("invalid eligible_for")
	}
	switch q.Quota {
	case "", "available", "exhausted":
	default:
		return fail("invalid quota")
	}
	if !q.From.IsZero() || !q.To.IsZero() {
		if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
			return fail("invalid expiration window")
		}
	}
	if !q.LegacyArray {
		if q.Limit == 0 {
			q.Limit = 50
		}
		if q.Offset < 0 || q.Limit < 1 || q.Limit > 200 {
			return fail("invalid pagination")
		}
	}
	return s.Repository.List(ctx, actor, admin, q)
}

type QueryError struct{ Message string }

func (e *QueryError) Error() string { return e.Message }

var ErrAdministrativeRead = errors.New("current administrator required")

func (s SubscriptionQueries) Detail(ctx context.Context, actor, id uint) (SubscriptionDetail, error) {
	if actor == 0 {
		return SubscriptionDetail{}, ErrAdministrativeRead
	}
	if id == 0 {
		return SubscriptionDetail{}, ErrAccessNotFound
	}
	return s.Repository.Detail(ctx, actor, id)
}
func EffectiveStatus(sub Subscription, now time.Time) string {
	if sub.Status == "active" && (!sub.EndAt.After(now) || sub.FlowUsed >= sub.FlowTotal) {
		return "expired"
	}
	return sub.Status
}
