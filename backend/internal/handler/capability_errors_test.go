package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
)

func TestIntegrationCapabilityErrorsUseCanonicalCodes(t *testing.T) {
	for _, test := range []struct {
		err       error
		status    int
		code      string
		retryable bool
	}{
		{catalog.ErrDenied, http.StatusUnauthorized, "unauthenticated", false},
		{catalog.ErrInput, http.StatusBadRequest, "invalid_argument", false},
		{catalog.ErrUnknown, http.StatusNotFound, "not_found", false},
		{catalog.ErrRateLimited, http.StatusTooManyRequests, "rate_limited", true},
		{catalog.ErrUnavailable, http.StatusServiceUnavailable, "unavailable", true},
		{context.DeadlineExceeded, http.StatusGatewayTimeout, "deadline_exceeded", true},
		{errors.New("database detail must stay hidden"), http.StatusInternalServerError, "unavailable", true},
	} {
		response := httptest.NewRecorder()
		integrationError(response, test.err)
		if response.Code != test.status {
			t.Errorf("%v status=%d want=%d", test.err, response.Code, test.status)
			continue
		}
		var body APIResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error == nil || body.Error.Code != test.code || body.Error.Retryable == nil || *body.Error.Retryable != test.retryable {
			t.Errorf("%v detail=%+v", test.err, body.Error)
		}
		if test.status == http.StatusInternalServerError && body.Message == test.err.Error() {
			t.Fatal("internal failure leaked")
		}
	}
}
