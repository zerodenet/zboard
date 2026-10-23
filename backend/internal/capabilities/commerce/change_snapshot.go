package commerce

import "time"

// SubscriptionChangeSnapshot is the existing persisted business quote. Preserve
// its original JSON field names/version; it contains no kernel configuration.
// Shared quote/discount validation must bind the same target and actual-money
// sources without requiring the persistence adapter to duplicate price rules.
type SubscriptionChangeSnapshot struct {
	Version                    int            `json:",omitempty"`
	FullTerm                   bool           `json:",omitempty"`
	EntitlementEpoch           uint64         `json:",omitempty"`
	Residual                   int64          `json:",omitempty"`
	Applied                    int64          `json:",omitempty"`
	Surplus                    int64          `json:",omitempty"`
	ValueFingerprint           string         `json:",omitempty"`
	Sources                    map[uint]int64 `json:",omitempty"`
	PlanID, PlanSKUID          uint
	EndAt                      time.Time
	CycleStartUsed, ResetQuota int64
	SourceOrderID              uint
	QuotaEventID               uint
	QuotedAt                   time.Time
	Credit                     int64
}
