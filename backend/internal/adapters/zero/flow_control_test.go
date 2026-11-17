package zero

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFlowControlScopeAndEvidence(t *testing.T) {
	for _, mode := range []string{"closed", "ack_only", "generation", "reauthorized", "other_principal", "wrong_id", "lost_ack", "bounded"} {
		t.Run(mode, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			requests := make(chan string, 600)
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				dec := json.NewDecoder(server)
				enc := json.NewEncoder(server)
				closed := false
				healthQueries := 0
				flowSequence := 0
				for {
					var req map[string]json.RawMessage
					if dec.Decode(&req) != nil {
						return
					}
					var id uint64
					json.Unmarshal(req["id"], &id)
					response := map[string]any{"api_id": "zero.api.v1", "id": id, "ok": true}
					if mode == "wrong_id" {
						response["id"] = id + 1
					}
					if req["request"] != nil {
						var q map[string]json.RawMessage
						json.Unmarshal(req["request"], &q)
						if q["health"] != nil {
							healthQueries++
							core := "core"
							if mode == "generation" && healthQueries >= 3 {
								core = "replacement"
							}
							response["result"] = map[string]any{"health": map[string]any{"core_instance_id": core, "config_revision": 1}}
						}
						if q["principal_flows"] != nil {
							response["result"] = map[string]any{"principal_flows": map[string]any{"core_instance_id": "core", "session_registry_revision": 1, "principals": []any{map[string]any{"principal_key": "allowed", "active_flows": 1}, map[string]any{"principal_key": "private-secret", "active_flows": 1}}}}
						}
						if q["active_flows"] != nil {
							var params struct {
								Filter struct {
									Principal string `json:"principal_key"`
								} `json:"filter"`
							}
							json.Unmarshal(q["active_flows"], &params)
							requests <- "list:" + params.Filter.Principal
							flows := []any{}
							if !closed || mode == "bounded" {
								principal := "private-secret"
								if mode == "other_principal" {
									principal = "allowed"
								}
								flowID := "private-flow"
								if mode == "bounded" {
									flowSequence++
									flowID = strings.Repeat("f", flowSequence)
								}
								flows = append(flows, map[string]any{"record": map[string]any{"flow_id": flowID, "state": "active", "auth": map[string]any{"principal_key": principal}}})
							}
							response["result"] = map[string]any{"active_flows": flows}
						}
					} else {
						var method string
						json.Unmarshal(req["method"], &method)
						requests <- method
						if mode == "lost_ack" {
							return
						}
						if mode != "ack_only" {
							closed = true
						}
						response["result"] = map[string]any{}
					}
					if enc.Encode(response) != nil {
						return
					}
				}
			}()
			guards := 0
			guard := func(_ context.Context, principal string, close func() error) (bool, error) {
				if principal == "allowed" {
					return false, nil
				}
				guards++
				if mode == "reauthorized" && guards > 1 {
					return false, nil
				}
				return true, close()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := DrainRevokedFlows(ctx, client, func(context.Context) error { return nil }, guard)
			<-done
			close(requests)
			if mode == "closed" {
				if err != nil || result.ConfirmedClosed != 1 || result.CloseCommands != 1 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else {
				if err == nil || result.ConfirmedClosed != 0 {
					t.Fatalf("false confirmation: result=%+v err=%v", result, err)
				}
			}
			commands := 0
			for request := range requests {
				if request == "list:allowed" {
					t.Fatal("listed another subscription")
				}
				if request == "flows.close" {
					commands++
				}
			}
			if (mode == "generation" || mode == "reauthorized" || mode == "other_principal" || mode == "wrong_id") && commands != 0 {
				t.Fatalf("unsafe commands=%d", commands)
			}
			if err != nil && strings.Contains(err.Error(), "private-secret") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestFlowControlCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := DrainRevokedFlows(ctx, client, func(context.Context) error { return nil }, func(context.Context, string, func() error) (bool, error) { return false, nil })
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("cancellation did not close socket", err)
	}
}
