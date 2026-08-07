package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDuplicatePersistenceResponseDetection(t *testing.T) {
	buffered := newBufferedResponseWriter()
	buffered.WriteHeader(http.StatusInternalServerError)
	_, _ = buffered.Write([]byte(`{"message":"Error 1062 (23000): Duplicate entry 'starter' for key 'uni_plans_slug'"}`))
	if !isDuplicatePersistenceResponse(buffered) {
		t.Fatal("expected duplicate database response to be detected")
	}
}

func TestWritePlanSubscriptionLimitReachedUsesStableBusinessContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	writePlanSubscriptionLimitReached(recorder)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`"code":"plan_subscription_limit_reached"`,
		"有效订阅数量已达到上限",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response %s does not contain %q", body, expected)
		}
	}
	if strings.Contains(strings.ToLower(body), "select ") || strings.Contains(strings.ToLower(body), "sqlstate") {
		t.Fatalf("business response leaked database details: %s", body)
	}
}

func TestPlanSubscriptionLimitResponseRecognizesLegacyFulfillmentError(t *testing.T) {
	buffered := newBufferedResponseWriter()
	buffered.WriteHeader(http.StatusInternalServerError)
	_, _ = buffered.Write([]byte(`{"code":500,"message":"plan subscription capacity is exhausted"}`))
	if !isPlanSubscriptionLimitResponse(buffered) {
		t.Fatal("expected legacy fulfillment error to be translated")
	}
}
