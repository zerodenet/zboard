// Package v1 defines the transport-neutral public capability contract.
package v1

type ErrorCode string

const (
	ErrorInvalidArgument  ErrorCode = "invalid_argument"
	ErrorUnauthenticated  ErrorCode = "unauthenticated"
	ErrorPermissionDenied ErrorCode = "permission_denied"
	ErrorNotFound         ErrorCode = "not_found"
	ErrorConflict         ErrorCode = "conflict"
	ErrorRateLimited      ErrorCode = "rate_limited"
	ErrorUnavailable      ErrorCode = "unavailable"
	ErrorDeadlineExceeded ErrorCode = "deadline_exceeded"
)

var retryable = map[ErrorCode]bool{
	ErrorInvalidArgument:  false,
	ErrorUnauthenticated:  false,
	ErrorPermissionDenied: false,
	ErrorNotFound:         false,
	ErrorConflict:         false,
	ErrorRateLimited:      true,
	ErrorUnavailable:      true,
	ErrorDeadlineExceeded: true,
}

func IsErrorCode(value string) bool {
	_, ok := retryable[ErrorCode(value)]
	return ok
}

func IsRetryable(code ErrorCode) bool {
	return retryable[code]
}
