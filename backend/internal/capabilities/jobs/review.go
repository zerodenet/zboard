package jobs

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrPermission = errors.New("job operation not permitted")
var ErrOutcomeUnverified = errors.New("native resource outcome has not been reconciled")

// Reviewer is supplied by the authenticated transport, not by the request body.
// The repository rechecks the current account authorization in its transaction.
type Reviewer struct{ AccountID uint }
type Review struct {
	RunID   string
	Outcome State
	Reason  string
}
type ReviewRepository interface {
	RecordVerifiedOutcome(context.Context, Reviewer, Review) error
}
type ReviewService struct{ Repository ReviewRepository }

func (s ReviewService) Resolve(ctx context.Context, actor Reviewer, in Review) error {
	if actor.AccountID == 0 {
		return ErrPermission
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.RunID == "" || len(in.RunID) > 36 || in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 || (in.Outcome != Succeeded && in.Outcome != Failed) {
		return ErrInvalid
	}
	return s.Repository.RecordVerifiedOutcome(ctx, actor, in)
}
