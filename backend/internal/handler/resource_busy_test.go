package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestResourceContentionTransportPreservesRetryAndRedactsDetails(t *testing.T) {
	h := &handlers{}
	for name, write := range map[string]func(http.ResponseWriter, error){
		"wallet":       walletError,
		"funding":      fundingError,
		"subscription": writeSubscriptionQueryError,
		"server":       ServerError,
		"settlement": func(w http.ResponseWriter, err error) {
			h.writeOrderSettlement(w, commerce.SettlementResult{}, err, false)
		},
		"callback": func(w http.ResponseWriter, err error) {
			h.writeOrderSettlement(w, commerce.SettlementResult{}, err, true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			write(response, fmt.Errorf("private database context: %w", network.ErrResourcesBusy))
			var body APIResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || body.Error == nil || body.Error.Code != "unavailable" || body.Error.Retryable == nil || !*body.Error.Retryable || body.Data != nil {
				t.Fatalf("contention must be a retryable unavailable response without an applied result: %d %+v", response.Code, body)
			}
			if strings.Contains(response.Body.String(), "private database") {
				t.Fatal("private error context escaped the transport boundary")
			}
		})
	}
}

func TestResourceContentionDoesNotClassifyOtherFailures(t *testing.T) {
	for _, err := range []error{nil, errors.New("Error 1213: deadlock"), commerce.ErrPaymentFactConflict} {
		response := httptest.NewRecorder()
		if writeResourceBusy(response, err) || response.Body.Len() != 0 || len(response.Header()) != 0 {
			t.Fatal("unrelated errors must retain their existing failure handling")
		}
	}
}
