package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"
)

func TestTrafficAuthenticationQueryHonorsCancellation(t *testing.T) {
	f := newTrafficReadFixture(t)
	f.capture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	f.h.TrafficUsageRecordsHandler(response, announcementRequest(http.MethodGet, "/api/v1/traffic/records?paged=true", f.token, "").WithContext(ctx))
	if response.Code == http.StatusOK || len(f.log.queries) != 0 || len(f.log.authContexts) != 1 || f.log.authContexts[0] != ctx {
		t.Fatalf("status=%d auth=%d queries=%d", response.Code, len(f.log.authContexts), len(f.log.queries))
	}
}

func TestTrafficReferenceQueriesAllHonorCancellation(t *testing.T) {
	f := newTrafficReadFixture(t)
	readers := map[string]func(*gorm.DB, map[string]entityReference, []uint) error{
		"user": resolveUserReferences, "subscription": resolveSubscriptionReferences, "node": resolveNodeReferences,
		"protocol": resolveProtocolEndpointReferences, "plan": resolvePlanReferences, "sku": resolvePlanSKUReferences, "order": resolveOrderReferences,
	}
	for name, reader := range readers {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := reader(f.h.db.WithContext(ctx), map[string]entityReference{}, []uint{1})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("query ignored cancellation: %v", err)
			}
		})
	}
}
