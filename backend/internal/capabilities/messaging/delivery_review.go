package messaging

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrDeliveryReviewRequired = errors.New("邮件接收结果待核验，请先核验未知结果再重试")
	ErrDeliveryReviewConflict = errors.New("投递状态或执行次数已变化，请刷新后核验")
)

type DeliveryReviewInput struct {
	ExpectedAttempt int        `json:"expected_attempt"`
	Acceptance      Acceptance `json:"acceptance"`
	Reason          string     `json:"reason"`
}
type DeliveryReviewRepository interface {
	Review(context.Context, uint, uint, uint, DeliveryReviewInput) error
}
type DeliveryReview struct{ Repository DeliveryReviewRepository }

func (s DeliveryReview) Review(ctx context.Context, actor, taskID, itemID uint, in DeliveryReviewInput) error {
	if actor == 0 {
		return ErrTemplatePermission
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if taskID == 0 || itemID == 0 || in.ExpectedAttempt < 1 || (in.Acceptance != Accepted && in.Acceptance != NotAccepted) || len(in.Reason) < 5 || len(in.Reason) > 2000 {
		return ErrInvalidMessage
	}
	return s.Repository.Review(ctx, actor, taskID, itemID, in)
}
