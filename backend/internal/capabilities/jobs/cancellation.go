package jobs

import (
	"context"
	"strings"
	"unicode/utf8"
)

type Cancellation struct {
	RunID  string
	Reason string
}

type CancellationRepository interface {
	RecordCancellation(context.Context, Reviewer, Cancellation) (State, error)
}

type ActiveCancellation interface {
	CancelActive(string)
}

type CancellationService struct {
	Repository CancellationRepository
	Active     ActiveCancellation
}

func (s CancellationService) Cancel(ctx context.Context, actor Reviewer, in Cancellation) (State, error) {
	if actor.AccountID == 0 {
		return "", ErrPermission
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if s.Repository == nil || in.RunID == "" || len(in.RunID) > 36 || in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 {
		return "", ErrInvalid
	}
	state, err := s.Repository.RecordCancellation(ctx, actor, in)
	if err == nil && state == CancelRequested && s.Active != nil {
		s.Active.CancelActive(in.RunID)
	}
	return state, err
}
