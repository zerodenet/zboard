package jobs

import "context"

type SubmissionRepository interface {
	Submit(context.Context, Submission) (Run, error)
}

type Submissions struct{ Repository SubmissionRepository }

func (s Submissions) Submit(ctx context.Context, input Submission) (Run, error) {
	if s.Repository == nil {
		return Run{}, ErrInvalid
	}
	return s.Repository.Submit(ctx, input)
}
