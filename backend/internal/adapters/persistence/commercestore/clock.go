package commercestore

import "time"

// An optional trusted clock makes dated financial transitions reproducible.
// Transport input never selects it. Production keeps the existing UTC wall clock.
func commerceNow(clock func() time.Time) time.Time {
	if clock != nil {
		return clock().UTC()
	}
	return time.Now().UTC()
}
