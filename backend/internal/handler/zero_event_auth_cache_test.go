package handler

import (
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestZeroEventHandlerReusesPreparedRequestState(t *testing.T) {
	h := &handlers{}
	state := &zeroEventRequestState{
		event: zeroEventEnvelope{
			SchemaID: "zero.event.v1", EventID: "event-1", EventType: "flow.updated", SourceID: "node-5",
			Payload: json.RawMessage(`{"flow_id":"flow-1","traffic":{"bytes_up":0,"bytes_down":0}}`),
		},
		node: model.Node{ID: 5}, receivedAt: time.Now().UTC(),
	}
	request := withZeroEventState(httptest.NewRequest("POST", "/api/zero/events", nil), state)
	response := httptest.NewRecorder()
	h.ZeroEventHandler(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "no attributable principal_key") {
		t.Fatalf("prepared request was reparsed or reauthenticated: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthenticateZeroEventUsesValidCredentialCacheWithoutDatabase(t *testing.T) {
	h := &handlers{}
	h.zeroEventAuthCache.Store(uint(5), zeroEventCredentialCacheEntry{
		node: model.Node{ID: 5, Name: "cached-node"}, secret: "connector-secret", expiresAt: time.Now().Add(time.Minute),
	})
	request := httptest.NewRequest("POST", "/api/zero/events", nil)
	request.Header.Set("Authorization", "Bearer connector-secret")
	node, err := h.authenticateZeroEvent(request, "node-5")
	if err != nil || node.ID != 5 {
		t.Fatalf("cached authentication = %+v, %v", node, err)
	}
}

func TestAuthenticateZeroEventNegativeCacheRejectsRepeatedStaleSecretWithoutDatabase(t *testing.T) {
	h := &handlers{}
	request := httptest.NewRequest("POST", "/api/zero/events", nil)
	request.Header.Set("Authorization", "Bearer stale-connector-secret")
	h.zeroEventAuthFailures.Store(uint(5), zeroEventAuthFailureEntry{
		digest: sha256.Sum256([]byte("stale-connector-secret")), expiresAt: time.Now().Add(time.Minute),
	})
	if _, err := h.authenticateZeroEvent(request, "node-5"); err == nil {
		t.Fatal("negative credential cache must reject a repeated stale secret")
	}
}

func TestInvalidZeroEventCredentialCacheOutlivesPositiveCache(t *testing.T) {
	if zeroEventInvalidCredentialCacheTTL <= zeroEventCredentialCacheTTL {
		t.Fatalf("invalid credential cache TTL = %s, want longer than positive cache %s", zeroEventInvalidCredentialCacheTTL, zeroEventCredentialCacheTTL)
	}
}
