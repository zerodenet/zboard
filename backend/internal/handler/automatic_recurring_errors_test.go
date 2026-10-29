package handler

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

func TestAutomaticRecurringHTTPProviderErrorsPreserveRecoveryBoundary(t *testing.T) {
	for _, item := range []struct {
		err  error
		code int
		kind string
	}{
		{commerce.ErrDiscountQuoteUnavailable, 503, "discount_unavailable"},
		{commerce.ErrDiscountQuoteInvalid, 502, "discount_invalid"},
		{errors.Join(commerce.ErrDiscountConfirmationRequired, &commerce.ValidationError{Message: "confirmation"}), 409, "discount_confirmation_required"},
		{errors.New("private storage details"), 500, "commerce_persistence_failed"},
	} {
		response := httptest.NewRecorder()
		automaticRenewalError(response, item.err)
		var body struct {
			Error *APIError `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != item.code || body.Error == nil || body.Error.Code != item.kind || body.Error.Version != 1 {
			t.Fatalf("wrong provider/recovery classification: %d %s %v", response.Code, response.Body.String(), err)
		}
	}
}
