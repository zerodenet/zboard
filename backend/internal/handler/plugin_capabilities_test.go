package handler

import (
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/capabilities/catalog"
	"github.com/zerodenet/zboard/backend/internal/plugins"
	"net/http/httptest"
	"testing"
)

func TestPluginCapabilityErrorsDoNotInvalidateHostLogin(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{catalog.ErrDenied, 403}, {catalog.ErrRateLimited, 429}, {plugins.ErrUnavailable, 503}} {
		w := httptest.NewRecorder()
		pluginCapabilityError(w, test.err)
		if w.Code != test.status {
			t.Fatalf("status=%d want=%d", w.Code, test.status)
		}
		var response APIResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Error == nil || response.Error.Retryable == nil {
			t.Fatalf("missing capability error detail: %s", w.Body.String())
		}
		if test.err == catalog.ErrDenied && (response.Error.Code != "permission_denied" || *response.Error.Retryable) {
			t.Fatalf("permission error=%+v", response.Error)
		}
	}
}
