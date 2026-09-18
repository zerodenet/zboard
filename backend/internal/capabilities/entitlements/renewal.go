package entitlements

import (
	"fmt"
	"strings"
)

type Renewal struct {
	ExtendPeriod  bool
	AddQuota      bool
	MakePermanent bool
}

func RenewalForGrant(order GrantRequest) (Renewal, error) {
	switch order.OrderType {
	case "traffic_pack":
		return Renewal{AddQuota: true}, nil
	case "upgrade":
		return Renewal{ExtendPeriod: true, AddQuota: true}, nil
	case "renewal":
		effect := strings.TrimSpace(order.RenewalEffect)
		if effect == "" {
			// Compatibility for an order created before the renewal-effect snapshot
			// existed. Reconciliation persists this same interpretation.
			if order.BillingUnit == "once" {
				effect = "add_quota_only"
			} else {
				effect = "extend_and_add_quota"
			}
		}
		switch effect {
		case "extend_only":
			return Renewal{ExtendPeriod: true}, nil
		case "extend_and_add_quota":
			return Renewal{ExtendPeriod: true, AddQuota: true}, nil
		case "add_quota_only":
			return Renewal{AddQuota: true, MakePermanent: order.BillingUnit == "once"}, nil
		default:
			return Renewal{}, fmt.Errorf("unsupported renewal effect %q", effect)
		}
	default:
		return Renewal{ExtendPeriod: true, AddQuota: true}, nil
	}
}
