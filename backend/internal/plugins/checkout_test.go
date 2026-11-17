package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type slotActionClient struct {
	pluginv1.PluginControlClient
	request *pluginv1.PageActionRequest
	err     error
	result  []byte
}

func (c *slotActionClient) HandlePageAction(_ context.Context, r *pluginv1.PageActionRequest, _ ...grpc.CallOption) (*pluginv1.PageActionResponse, error) {
	c.request = r
	if c.err != nil {
		return nil, c.err
	}
	if c.result != nil {
		return &pluginv1.PageActionResponse{ResultJson: c.result}, nil
	}
	return &pluginv1.PageActionResponse{ResultJson: []byte(`{"ok":true}`)}, nil
}
func TestSlotActionsExpireAndNativeCommerceRejectsRevokedAdmission(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.Capabilities = append(m.Capabilities, UISlotCapability, CommerceOrdersReadCapability, CommercePaymentsWriteCapability)
		m.Contributions.Pages = nil
		m.Contributions.Slots = []Slot{{ID: "checkout", Surface: "account", Slot: CheckoutPaymentSlot, Title: "Payment", Entrypoint: "ui/index.html"}}
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = []byte("signed fixture")
	})
	m, db, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&v.PluginInstallation).Updates(map[string]any{"enabled": true, "state": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	v.Enabled = true
	v.State = "active"
	api := &slotActionClient{}
	proc := &process{api: api}
	m.processes[v.ID] = proc
	defer delete(m.processes, v.ID)
	m.SetHostServices(pageActionHostServices{})
	session, err := m.CreateSlotSession(v.ID, "checkout", CheckoutPaymentSlot, "account", 41, false, 41)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SessionPageAction(t.Context(), session.Token, 41, false, "method.options", json.RawMessage(`{"actor_id":"forged","slot_id":"other"}`)); err != nil {
		t.Fatal(err)
	}
	if api.request.ActorId != v.ID+":41" || api.request.SlotId != "checkout" || api.request.Slot != CheckoutPaymentSlot || api.request.Purpose != "slot" || api.request.PageId != "" {
		t.Fatalf("untrusted native context: %+v", api.request)
	}
	api.request = nil
	if _, err := m.SessionCheckoutAction(t.Context(), session.Token, 41, false, 0, "method.prepare", json.RawMessage(`{}`)); err == nil || api.request != nil {
		t.Fatalf("empty checkout order reached account-scoped action: %v", err)
	}
	for _, failure := range []struct {
		err, want error
		result    []byte
	}{
		{err: status.Error(codes.DeadlineExceeded, "timed out"), want: context.DeadlineExceeded},
		{err: errors.New("process disconnected"), want: ErrUnavailable},
		{result: []byte(`invalid-json`), want: ErrUnavailable},
	} {
		api.err, api.result = failure.err, failure.result
		if _, err := m.SessionPageAction(t.Context(), session.Token, 41, false, "method.options", json.RawMessage(`{}`)); !errors.Is(err, failure.want) {
			t.Fatalf("runtime failure classified as client input: %v; want %v", err, failure.want)
		}
	}
	api.err, api.result = nil, nil
	m.mu.Lock()
	old := m.sessions[session.Token]
	old.ExpiresAt = time.Now().Add(-time.Second)
	m.sessions[session.Token] = old
	m.mu.Unlock()
	if _, err := m.SessionPageAction(t.Context(), session.Token, 41, false, "method.options", json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expired action: %v", err)
	}
	if err := db.Where("plugin_id = ?", v.ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(pluginv1.HostCallRequest{Capability: CommercePaymentsWriteCapability, Operation: "commerce.payments.record", Payload: json.RawMessage(`{"principal_id":"forged"}`)})
	recorder := httptest.NewRecorder()
	m.handleHostService(recorder, httptest.NewRequest("POST", "/service", nil), v, proc, body)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("revoked native call: %d %s", recorder.Code, recorder.Body.String())
	}
}
func TestPaymentIdentityAndAdmissionSurviveProcessGeneration(t *testing.T) {
	v := Installation{PluginInstallation: model.PluginInstallation{ID: "example.payment", SigningKey: "key-one", Generation: 1}}
	identity := paymentIdentity(v)
	v.Generation = 200
	v.ConfigRevision = 400
	v.Version = "3.0.0"
	if paymentIdentity(v) != identity || len(identity) > 32 {
		t.Fatal("payment namespace changed on lifecycle update")
	}
	v.SigningKey = "key-two"
	if paymentIdentity(v) == identity {
		t.Fatal("publisher replacement reused old payment authority")
	}
	m := &Manager{}
	for i := 0; i < 60; i++ {
		if err := m.admitPluginCall(identity+":orders", 60); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.admitPluginCall(identity+":orders", 60); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("unbounded native requests: %v", err)
	}
}
