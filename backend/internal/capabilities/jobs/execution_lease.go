package jobs

import (
	"context"
	"time"
)

// Most scans finish before the first renewal and add no heartbeat writes.
// Long operations retain their original deadline while worker liveness is
// fenced independently. Renewal failure cancels execution, never authorizes
// another attempt or turns an uncertain external result into success.
func keepExecutionLease(ctx context.Context, cancel context.CancelFunc, store Store, claim Claim) func() error {
	renewable, ok := store.(RenewableLeaseStore)
	if !ok || claim.DeadlineAt.IsZero() || !claim.ExpiresAt.Before(claim.DeadlineAt) {
		return func() error { return nil }
	}
	poll, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	failure := make(chan error, 1)
	go func() {
		defer close(done)
		expiry := claim.ExpiresAt
		for {
			if !expiry.Before(claim.DeadlineAt) {
				<-poll.Done()
				return
			}
			remaining := time.Until(expiry)
			if remaining <= 0 {
				failure <- ErrLeaseLost
				cancel()
				return
			}
			timer := time.NewTimer(min(10*time.Second, max(time.Millisecond, remaining/3)))
			select {
			case <-poll.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			call, release := context.WithTimeout(poll, min(5*time.Second, max(time.Millisecond, time.Until(expiry)/2)))
			next, err := renewable.RenewLease(call, claim)
			release()
			if poll.Err() != nil {
				return
			}
			if err != nil {
				failure <- err
				cancel()
				return
			}
			if !next.After(time.Now()) || next.After(claim.DeadlineAt) {
				failure <- ErrLeaseLost
				cancel()
				return
			}
			expiry = next
		}
	}()
	return func() error {
		stop()
		<-done
		select {
		case err := <-failure:
			return err
		default:
			return nil
		}
	}
}
