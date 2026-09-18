package v1

import "testing"

func TestCanonicalCapabilityErrorCodesDeclareRetryability(t *testing.T) {
	for _, code := range []ErrorCode{
		ErrorInvalidArgument,
		ErrorUnauthenticated,
		ErrorPermissionDenied,
		ErrorNotFound,
		ErrorConflict,
		ErrorRateLimited,
		ErrorUnavailable,
		ErrorDeadlineExceeded,
	} {
		if !IsErrorCode(string(code)) {
			t.Errorf("missing canonical code %q", code)
		}
	}
	if IsErrorCode("internal_error") {
		t.Fatal("non-contract error accepted")
	}
	if IsRetryable(ErrorPermissionDenied) || !IsRetryable(ErrorRateLimited) || !IsRetryable(ErrorUnavailable) || !IsRetryable(ErrorDeadlineExceeded) {
		t.Fatal("unexpected retryability policy")
	}
}
