package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
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

func TestTrafficReferenceReadModelHonorsCancellation(t *testing.T) {
	f := newTrafficReadFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.h.services.EntityReferences.Resolve(ctx, observability.EntityReferenceRequest{Users: []uint{1}}, observability.EntityReferenceData{Users: map[string]entityReference{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("query ignored cancellation: %v", err)
	}
}
