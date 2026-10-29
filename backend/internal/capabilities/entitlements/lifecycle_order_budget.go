package entitlements

// These bounds limit each materialized identity page and the compact 64-bit
// identity proof, not arbitrary paid history eligibility or total process RSS.
const (
	LifecycleOrderPageSize          = 200
	LifecycleOrderIdentityByteLimit = 512 << 10
)
