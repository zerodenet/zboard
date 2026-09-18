package network

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrConnectorActivityUnavailable = errors.New("connector activity observation capability unavailable")

const (
	DefaultConnectorActivityTimeout = 10 * time.Second
	DefaultConnectorActivityPoll    = time.Second
)

type ConnectorActivityRepository interface {
	ReadConnectorLastSeen(context.Context, uint) (*time.Time, error)
}

type ConnectorActivityObserver struct {
	Repository   ConnectorActivityRepository
	Timeout      time.Duration
	PollInterval time.Duration
}

func (o ConnectorActivityObserver) Wait(parent context.Context, nodeID uint, activatedAt time.Time) (time.Time, error) {
	if o.Repository == nil || nodeID == 0 || activatedAt.IsZero() {
		return time.Time{}, ErrConnectorActivityUnavailable
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultConnectorActivityTimeout
	}
	poll := o.PollInterval
	if poll <= 0 {
		poll = DefaultConnectorActivityPoll
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		activity, err := o.Repository.ReadConnectorLastSeen(ctx, nodeID)
		if err != nil {
			return time.Time{}, err
		}
		if activity != nil && !activity.Before(activatedAt) {
			return activity.UTC(), nil
		}
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return time.Time{}, fmt.Errorf("connector event verification canceled: %w", ctx.Err())
			}
			return time.Time{}, fmt.Errorf("no fresh connector event arrived within %s: %w", timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}
