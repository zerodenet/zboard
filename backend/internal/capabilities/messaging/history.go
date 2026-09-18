package messaging

import (
	"context"
	"errors"
	"time"
)

var ErrDeliveryNotFound = errors.New("mail delivery not found")

type DeliveryAttempt struct {
	ID         uint       `json:"id"`
	Attempt    int        `json:"attempt"`
	Acceptance Acceptance `json:"acceptance"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}
type DeliveryHistoryPage struct {
	Items  []DeliveryAttempt `json:"items"`
	Total  int64             `json:"total"`
	Offset int               `json:"offset"`
	Limit  int               `json:"limit"`
}
type DeliveryHistoryRepository interface {
	List(context.Context, uint, uint, uint, int, int) (DeliveryHistoryPage, error)
}
type DeliveryHistory struct{ Repository DeliveryHistoryRepository }

func (s DeliveryHistory) List(ctx context.Context, actor, taskID, itemID uint, limit, offset int) (DeliveryHistoryPage, error) {
	if actor == 0 {
		return DeliveryHistoryPage{}, ErrTemplatePermission
	}
	if taskID == 0 || itemID == 0 || limit < 1 || limit > 50 || offset < 0 || offset > 1000000 {
		return DeliveryHistoryPage{}, ErrInvalidMessage
	}
	return s.Repository.List(ctx, actor, taskID, itemID, limit, offset)
}
