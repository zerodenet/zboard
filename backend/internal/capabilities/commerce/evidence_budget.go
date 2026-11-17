package commerce

import "fmt"

// Resource exhaustion is distinct from an unknown historical price. Neither
// permits a partial quote, an automatic debit, or treating unknown value as zero.
var ErrSubscriptionEvidenceBudget = fmt.Errorf("subscription purchase evidence exceeds the bounded operation budget: %w", ErrSubscriptionValueUnavailable)
