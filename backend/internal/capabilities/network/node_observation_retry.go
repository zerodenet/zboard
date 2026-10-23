package network

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrObservationIntentRetryInvalid = errors.New("invalid observation retry candidate")
var ErrObservationIntentRetryConflict = errors.New("observation retry candidate changed or not recoverable")

type ObservationIntentRetryRequest struct {
	ExpectedAttempts string `json:"expected_attempts"`
	Reason           string `json:"reason"`
}
type ObservationIntentRetryReceipt struct {
	ID               string `json:"id"`
	ExpectedAttempts string `json:"expected_attempts"`
	AuditID          uint   `json:"audit_id,string"`
	Status           string `json:"status"`
}

func (in ObservationIntentRetryRequest) Validate(id string) (ObservationIntentRetryRequest, uint64, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	attempts, err := strconv.ParseUint(in.ExpectedAttempts, 10, 64)
	if !observationIntentID(id) || err != nil || attempts == 0 || attempts == math.MaxUint64 || strconv.FormatUint(attempts, 10) != in.ExpectedAttempts || !utf8.ValidString(in.Reason) || len(in.Reason) < 3 || len(in.Reason) > 400 {
		return in, 0, ErrObservationIntentRetryInvalid
	}
	return in, attempts, nil
}

type ObservationIntentRetryRepository interface {
	RetryObservationIntent(context.Context, uint, string, ObservationIntentRetryRequest) (ObservationIntentRetryReceipt, error)
}

// A retry admits one explicitly inspected held attempt; it never changes the
// material, the asset identity, the worker's ownership or its validation rules.
func (s ObservationIntentOperations) Retry(ctx context.Context, actor uint, id string, in ObservationIntentRetryRequest) (ObservationIntentRetryReceipt, error) {
	if actor == 0 {
		return ObservationIntentRetryReceipt{}, ErrNodeAdministrationPermission
	}
	in, _, err := in.Validate(id)
	if err != nil {
		return ObservationIntentRetryReceipt{}, err
	}
	repo, ok := s.Repository.(ObservationIntentRetryRepository)
	if !ok {
		return ObservationIntentRetryReceipt{}, ErrNodeAdministrationUnavailable
	}
	return repo.RetryObservationIntent(ctx, actor, id, in)
}
