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
	for _, capability := range []string{AccountAssertionCapability, SubscriptionProjectionCapability, MessageProjectionCapability,
		AccountSelfReadCapability, AccountAdminReadCapability, SubscriptionReadCapability, SubscriptionConfigReadCapability, SubscriptionAdminReadCapability,
		SubscriptionQuotaWriteCapability, SubscriptionTermWriteCapability, SubscriptionStatusWriteCapability,
		MessageReadCapability, MessageAckCapability, HostDiscoveryCapability} {
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

func TestNativeHostDiscoveryReturnsOnlyOwnAdmittedCapabilities(t *testing.T) {
	manager, _, _ := testManager(t, nil)
	proc := &process{}
	installation := Installation{Admission: Admission{Accepted: true, Capabilities: []string{ConfigCapability, HostDiscoveryCapability, AccountSelfReadCapability}}, Manifest: Manifest{Capabilities: []string{ConfigCapability, HostDiscoveryCapability, AccountSelfReadCapability}}}
	installation.ID = "example.discovery"
	manager.processes[installation.ID] = proc
	defer delete(manager.processes, installation.ID)
	request, _ := json.Marshal(pluginv1.HostCallRequest{Capability: HostDiscoveryCapability, Operation: "capabilities.list", Payload: json.RawMessage(`{}`)})
	recorder := httptest.NewRecorder()
	manager.handleHostService(recorder, httptest.NewRequest(http.MethodPost, "/service", nil), installation, proc, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", recorder.Code, recorder.Body.String())
	}
	var result pluginv1.HostCallResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var data struct {
		Capabilities []string `json:"capabilities"`
		HostVersion  string   `json:"host_version"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Capabilities) != 3 || data.Capabilities[1] != HostDiscoveryCapability || data.HostVersion == "" {
		t.Fatalf("wrong discovery data: %+v", data)
	}
}

func TestSplitStorageGrantsEnableOnlyPrivateStorageCallback(t *testing.T) {
	for _, capability := range []string{StorageReadCapability, StorageWriteCapability} {
		installation := Installation{
			Admission: Admission{Accepted: true, Capabilities: []string{ConfigReadCapability, capability}},
			Manifest:  Manifest{Capabilities: []string{ConfigReadCapability, capability}},
		}
		if !requiresHostCallback(installation) || isHostServiceCapability(capability) {
			t.Fatalf("split storage grant %s has wrong callback surface", capability)
		}
	}
}
