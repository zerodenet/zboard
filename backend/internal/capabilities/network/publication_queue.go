package network

import (
	"context"
	"errors"
	"time"
)

const (
	PublicationLease           = 3 * time.Minute
	PublicationTimeout         = 2 * time.Minute
	PublicationFinalizeTimeout = 5 * time.Second
)

var (
	ErrPublicationUnavailable = errors.New("node publication capability unavailable")
	ErrPublicationLeaseLost   = errors.New("node publication lease lost")
	ErrPublicationFinalize    = errors.New("node publication result was not finalized")
)

type Publication struct {
	NodeID, EndpointID, RequestedBy uint
	Generation                      uint64
	Attempts                        uint
	LastError                       string
	NextAttemptAt, LeaseUntil       time.Time
	LeaseToken                      string
}

type PublicationStore interface {
	Pending(context.Context, time.Time) (bool, error)
	Claim(context.Context, time.Time) (Publication, bool, error)
	Renew(context.Context, Publication, time.Time) (bool, error)
	Complete(context.Context, Publication, time.Time, error) error
}

type PublicationExecutor interface {
	PublishNodeConfiguration(context.Context, Publication) error
}

type PublicationQueue struct {
	Store    PublicationStore
	Executor PublicationExecutor
}

func (q PublicationQueue) Pending(ctx context.Context, now time.Time) (bool, error) {
	if q.Store == nil {
		return false, ErrPublicationUnavailable
	}
	return q.Store.Pending(ctx, now.UTC())
}

func (q PublicationQueue) Claim(ctx context.Context, now time.Time) (Publication, bool, error) {
	if q.Store == nil {
		return Publication{}, false, ErrPublicationUnavailable
	}
	return q.Store.Claim(ctx, now.UTC())
}

func (q PublicationQueue) Execute(parent context.Context, item Publication) error {
	if q.Store == nil || q.Executor == nil || item.NodeID == 0 || item.LeaseToken == "" || item.Generation == 0 {
		return ErrPublicationUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, PublicationTimeout)
	defer cancel()
	stop := make(chan struct{})
	done := make(chan struct{})
	renewalFailure := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(PublicationLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewalCtx, release := context.WithTimeout(context.WithoutCancel(parent), PublicationFinalizeTimeout)
				ok, err := q.Store.Renew(renewalCtx, item, time.Now().UTC().Add(PublicationLease))
				release()
				if err != nil || !ok {
					if err == nil {
						err = ErrPublicationLeaseLost
					}
					renewalFailure <- err
					cancel()
					return
				}
			}
		}
	}()
	failure := q.Executor.PublishNodeConfiguration(ctx, item)
	close(stop)
	<-done
	select {
	case err := <-renewalFailure:
		failure = errors.Join(failure, err)
	default:
	}
	completionCtx, release := context.WithTimeout(context.WithoutCancel(parent), PublicationFinalizeTimeout)
	defer release()
	completionErr := q.Store.Complete(completionCtx, item, time.Now().UTC(), failure)
	if completionErr != nil {
		return errors.Join(failure, ErrPublicationFinalize, completionErr)
	}
	return failure
}

func PublicationRetryDelay(attempts uint) time.Duration {
	if attempts > 6 {
		attempts = 6
	}
	delay := 5 * time.Second * time.Duration(1<<attempts)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
