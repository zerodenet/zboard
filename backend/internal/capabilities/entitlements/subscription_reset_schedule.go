package entitlements

import "time"

// Administrative correction is prospective. Never run a stale old schedule or
// grant missed cycles while changing it. Usage, quota and paid facts stay intact.
func ChangeTrafficResetSchedule(sub Subscription, policy int16, now time.Time) (Subscription, error) {
	if policy < 0 || policy > 5 || sub.Status == "ended" || sub.EndedAt != nil || sub.StartAt.IsZero() || now.IsZero() {
		return Subscription{}, ErrSubscriptionQuotaInvalid
	}
	if policy >= 1 && policy <= 4 && (sub.EndsOnQuotaExhaustion || IsPerpetualEnd(sub.EndAt)) {
		return Subscription{}, ErrSubscriptionResetPolicyNotApplicable
	}
	sub.ResetPolicy = policy
	if now.Before(sub.StartAt) {
		now = sub.StartAt
	}
	sub.NextResetAt = NextTrafficResetAfter(sub.StartAt, policy, now)
	return sub, nil
}
