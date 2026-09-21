package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

func TestNativeHostServiceRequiresManifestAndAdmissionCapability(t *testing.T) {
	manager, _, _ := testManager(t, nil)
	proc := &process{}
	for _, capability := range []string{AccountAssertionCapability, SubscriptionProjectionCapability, MessageProjectionCapability} {
		request, err := json.Marshal(pluginv1.HostCallRequest{Capability: capability, Operation: "account.assert", Payload: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		for name, installation := range map[string]Installation{
			"manifest": {
				Admission: Admission{Accepted: true, Capabilities: []string{ConfigCapability, capability}},
				Manifest:  Manifest{Capabilities: []string{ConfigCapability}},
			},
			"admission": {
				Admission: Admission{Accepted: true, Capabilities: []string{ConfigCapability}},
				Manifest:  Manifest{Capabilities: []string{ConfigCapability, capability}},
			},
		} {
			installation.ID = "example.denied"
			manager.processes[installation.ID] = proc
			recorder := httptest.NewRecorder()
			httpRequest := httptest.NewRequest(http.MethodPost, "/service", nil)
			manager.handleHostService(recorder, httpRequest, installation, proc, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s capability %s returned %d, want 403", name, capability, recorder.Code)
			}
			delete(manager.processes, installation.ID)
		}
	}
}
