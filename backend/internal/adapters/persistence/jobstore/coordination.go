package jobstore

import (
	"context"
	"gorm.io/gorm"
)

// WithLedgerLock is an infrastructure coordination boundary for transactions
// spanning a Run and its domain evidence. All participants acquire the common
// ledger lock before locking domain operations, never in the reverse order.
func (s *Store) WithLedgerLock(ctx context.Context, fn func(*gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.lock(tx); err != nil {
			return err
		}
		return fn(tx)
	})
}
