package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc"
)

type actionLifecycleClient struct {
	pluginv1.PluginControlClient
	after func()
}

func (c *actionLifecycleClient) HandlePageAction(context.Context, *pluginv1.PageActionRequest, ...grpc.CallOption) (*pluginv1.PageActionResponse, error) {
	if c.after != nil {
		c.after()
	}
	return nil, pluginv1.NewActionError("resource_busy", "public rejection", true)
}

func TestPublicActionErrorCannotSurviveRevocationOrProcessReplacement(t *testing.T) {
	for _, mode := range []string{"current", "revoked", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
				m.Capabilities = append(m.Capabilities, UISlotCapability)
				m.Contributions.Pages = nil
				m.Contributions.Slots = []Slot{{ID: "actions", Surface: "account", Slot: "account.subscriptions.actions", Title: "Actions", Entrypoint: "ui/index.html"}}
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
			client := &actionLifecycleClient{}
			m.processes[v.ID] = &process{api: client}
			defer delete(m.processes, v.ID)
			session, err := m.CreateSlotSession(v.ID, "actions", "account.subscriptions.actions", "account", 41, false, 41)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "revoked" {
				client.after = func() { m.RevokeSession(session.Token, 41) }
			}
			if mode == "replaced" {
				client.after = func() { m.mu.Lock(); m.processes[v.ID] = &process{api: client}; m.mu.Unlock() }
			}
			_, err = m.SessionPageAction(t.Context(), session.Token, 41, false, "resource.use", json.RawMessage(`{}`))
			if mode == "current" {
				var failure *pluginv1.ActionError
				if !errors.As(err, &failure) || failure.Code != "resource_busy" {
					t.Fatalf("lost public rejection: %v", err)
				}
			} else if !errors.Is(err, ErrInvalidSession) {
				t.Fatalf("stale public error accepted: %v", err)
			}
		})
	}
}
