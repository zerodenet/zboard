package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
