package jobs

import (
	"context"
	"errors"
	"strings"
)

var ErrBatchRequestPermission = errors.New("batch request requires current administrator")

type BatchRequestRepository interface {
	SubmitBatchRequest(context.Context, uint, BatchSubmission) (BatchSubmissionReceipt, error)
}

type BatchRequests struct{ Repository BatchRequestRepository }

func (s BatchRequests) Submit(ctx context.Context, actor uint, submission BatchSubmission) (BatchSubmissionReceipt, error) {
	if actor == 0 || strings.TrimSpace(submission.Type) == "" || strings.TrimSpace(submission.Scope) == "" || strings.TrimSpace(submission.Content) == "" || strings.TrimSpace(submission.IdempotencyKey) == "" || len(submission.Targets) == 0 {
		return BatchSubmissionReceipt{}, errors.New("invalid batch request")
	}
	return s.Repository.SubmitBatchRequest(ctx, actor, submission)
}
