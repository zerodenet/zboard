package messaging

import (
	"context"
	"time"
)

type PendingRegistrationEvent struct {
	AccountID  uint
	OccurredAt time.Time
}
type RegistrationEventStatus struct {
	Pending  int64
	OldestAt *time.Time
	Items    []PendingRegistrationEvent
}
type RegistrationStatusRepository interface {
	Pending(context.Context, uint, int, int) (RegistrationEventStatus, error)
}
type RegistrationStatus struct{ Repository RegistrationStatusRepository }

func (s RegistrationStatus) Pending(ctx context.Context, actor uint, limit, offset int) (RegistrationEventStatus, error) {
	if actor == 0 {
		return RegistrationEventStatus{}, ErrTemplatePermission
	}
	if limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return RegistrationEventStatus{}, ErrInvalidMessage
	}
	return s.Repository.Pending(ctx, actor, limit, offset)
}
