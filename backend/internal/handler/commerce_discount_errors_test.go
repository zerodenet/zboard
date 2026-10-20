package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
)

type rejectedDiscountOrders struct{ err error }

func (s rejectedDiscountOrders) Create(context.Context, uint, commerce.OrderCreateRequest) (commerce.Order, error) {
	return commerce.Order{}, s.err
}
func (s rejectedDiscountOrders) Preview(context.Context, uint, commerce.OrderCreateRequest) (commerce.OrderPreview, error) {
	return commerce.OrderPreview{}, s.err
}

// These versioned errors are also the frontend's explicit no-commit contract.
// Unknown server/transport errors must never acquire this classification.
func TestCommerceDiscountHTTPRejectionContract(t *testing.T) {
	f := newOrderFixture(t)
	for _, test := range []struct {
		name string
		err  error
		code int
		kind string
	}{
		{"unavailable", commerce.ErrDiscountQuoteUnavailable, 503, "discount_unavailable"},
		{"invalid", commerce.ErrDiscountQuoteInvalid, 502, "discount_invalid"},
		{"changed", &commerce.ValidationError{Message: "quote changed", Fields: map[string]string{"quote_fingerprint": "confirm again"}}, 400, "validation_failed"},
		{"idempotency", &commerce.ValidationError{Message: "existing intent", Fields: map[string]string{"idempotency_key": "original request required"}}, 400, "validation_failed"},
		{"storage", fmt.Errorf("private storage failure"), 500, "commerce_persistence_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.h.services.OrderCreation = commerce.OrderCreation{Repository: rejectedDiscountOrders{err: test.err}}
			for _, handler := range []http.HandlerFunc{f.h.OrderPreviewHandler, f.h.OrderCreateCommerceValidatedHandler} {
				response := httptest.NewRecorder()
				handler(response, announcementRequest(http.MethodPost, "/api/v1/orders", f.token, fmt.Sprintf(`{"plan_sku_id":%d}`, f.skuRecord.ID)))
				var result struct {
					Data  json.RawMessage `json:"data"`
					Error *APIError       `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != test.code || result.Error == nil || result.Error.Version != 1 || result.Error.Code != test.kind {
					t.Fatalf("wrong no-commit classification: %d %s %v", response.Code, response.Body.String(), err)
				}
				if test.name == "idempotency" && result.Error.Fields["idempotency_key"] == "" {
					t.Fatal("conflict lost the original-intent recovery boundary")
				}
				if len(result.Data) != 0 && string(result.Data) != "null" {
					t.Fatal("rejected quote advertised a created order", string(result.Data))
				}
			}
		})
	}
}
