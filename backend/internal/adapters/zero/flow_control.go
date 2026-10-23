package zero

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrFlowControlUnavailable = errors.New("native connection control is unavailable")
	ErrFlowControlEvidence    = errors.New("native connection control evidence is incomplete or changed")
	ErrFlowControlPending     = errors.New("native connection closure remains unconfirmed")
)

// FlowRevocationGuard rechecks authoritative authorization immediately before
// invoking close. Implementations must fence concurrent entitlement changes
// until close returns. Principals may be protocol secrets: never log them.
type FlowRevocationGuard func(context.Context, string, func() error) (bool, error)

type FlowControlResult struct{ ExaminedPrincipals, CloseCommands, ConfirmedClosed int }

type nativeFlowControl struct {
	conn     io.ReadWriteCloser
	scan     *bufio.Scanner
	sequence uint64
}

type nativeControlHealth struct {
	Core     string `json:"core_instance_id"`
	Revision uint64 `json:"config_revision"`
}

type nativeControlFlow struct {
	Record *struct {
		ID    string `json:"flow_id"`
		State string `json:"state"`
		Auth  *struct {
			Principal string `json:"principal_key"`
		} `json:"auth"`
	} `json:"record"`
}

// DrainRevokedFlows consumes the existing native API on one continuous
// connection. It neither applies configuration nor infers delivery from health.
// confirmConfiguration must verify the expected configuration separately; health
// brackets that check solely to fence process/configuration changes.
func DrainRevokedFlows(ctx context.Context, conn io.ReadWriteCloser, confirmConfiguration func(context.Context) error, guard FlowRevocationGuard) (FlowControlResult, error) {
	var result FlowControlResult
	if conn == nil || confirmConfiguration == nil || guard == nil {
		return result, ErrFlowControlUnavailable
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	c := nativeFlowControl{conn: conn, scan: bufio.NewScanner(conn)}
	c.scan.Buffer(make([]byte, 4096), 2<<20)
	health, err := c.health()
	if err != nil {
		return result, err
	}
	if err = confirmConfiguration(ctx); err != nil {
		return result, err
	}
	if err = c.sameHealth(health); err != nil {
		return result, err
	}
	var registry struct {
		Core       string  `json:"core_instance_id"`
		Revision   *uint64 `json:"session_registry_revision"`
		Principals []struct {
			Key   string  `json:"principal_key"`
			Count *uint64 `json:"active_flows"`
		} `json:"principals"`
	}
	if err = c.query("principal_flows", struct{}{}, &registry); err != nil {
		return result, err
	}
	if registry.Core != health.Core || registry.Revision == nil || registry.Principals == nil || len(registry.Principals) > 4096 {
		return result, ErrFlowControlEvidence
	}
	for _, principal := range registry.Principals {
		if principal.Count == nil {
			return result, ErrFlowControlEvidence
		}
		if *principal.Count == 0 {
			continue
		}
		if principal.Key == "" {
			return result, ErrFlowControlEvidence
		}
		result.ExaminedPrincipals++
		forbidden, err := guard(ctx, principal.Key, func() error { return nil })
		if err != nil {
			return result, err
		}
		if !forbidden {
			continue
		}
		// No offset exists in this native API. Drain bounded, principal-filtered
		// batches and query again; never call an incomplete node-wide page complete.
		issued := make(map[string]bool)
		for batch := 0; ; batch++ {
			if batch >= 8 {
				return result, ErrFlowControlPending
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			flows, err := c.flowsAfterClose(ctx, principal.Key, issued)
			if err != nil {
				return result, err
			}
			if err := c.sameHealth(health); err != nil {
				return result, err
			}
			if len(flows) == 0 {
				result.ConfirmedClosed += len(issued)
				break
			}
			for _, flow := range flows {
				if flow.Record == nil || flow.Record.ID == "" || flow.Record.Auth == nil || flow.Record.Auth.Principal != principal.Key || (flow.Record.State != "active" && flow.Record.State != "opening") {
					return result, ErrFlowControlEvidence
				}
				if issued[flow.Record.ID] {
					return result, ErrFlowControlPending
				}
				closed := false
				_, err := guard(ctx, principal.Key, func() error {
					if err := c.sameHealth(health); err != nil {
						return err
					}
					if result.CloseCommands >= 512 {
						return ErrFlowControlPending
					}
					result.CloseCommands++
					if err := c.command("flows.close", map[string]string{"flow_id": flow.Record.ID}); err != nil {
						return err
					}
					issued[flow.Record.ID] = true
					closed = true
					return nil
				})
				if err != nil {
					return result, err
				}
				if !closed {
					return result, ErrFlowControlPending
				}
			}
			// Only an empty principal-filtered query proves there are no remaining
			// flows. A truncated/reordered page cannot confirm any missing ID.
		}
	}
	return result, c.sameHealth(health)
}

// Native cancellation is asynchronous. Recheck without replaying a command
// while that exact flow is still observed. A timeout retains pending work.
func (c *nativeFlowControl) flowsAfterClose(ctx context.Context, principal string, issued map[string]bool) ([]nativeControlFlow, error) {
	deadline := time.Now().Add(time.Second)
	for {
		flows, err := c.flows(principal)
		if err != nil {
			return nil, err
		}
		outstanding := false
		for _, flow := range flows {
			if flow.Record != nil && issued[flow.Record.ID] {
				outstanding = true
			}
		}
		if !outstanding {
			return flows, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrFlowControlPending
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *nativeFlowControl) health() (nativeControlHealth, error) {
	var h nativeControlHealth
	if err := c.query("health", struct{}{}, &h); err != nil {
		return h, err
	}
	if h.Core == "" || h.Revision == 0 {
		return h, ErrFlowControlEvidence
	}
	return h, nil
}
func (c *nativeFlowControl) sameHealth(expected nativeControlHealth) error {
	h, err := c.health()
	if err != nil {
		return err
	}
	if h != expected {
		return ErrFlowControlEvidence
	}
	return nil
}
func (c *nativeFlowControl) flows(principal string) ([]nativeControlFlow, error) {
	var flows []nativeControlFlow
	err := c.query("active_flows", map[string]any{"limit": 64, "filter": map[string]string{"principal_key": principal}}, &flows)
	if len(flows) > 64 {
		return nil, ErrFlowControlEvidence
	}
	return flows, err
}
func (c *nativeFlowControl) query(name string, params any, result any) error {
	var wrapped map[string]json.RawMessage
	if err := c.call(map[string]any{"type": "query", "request": map[string]any{name: params}}, &wrapped); err != nil {
		return err
	}
	raw, ok := wrapped[name]
	if !ok || string(raw) == "null" {
		return ErrFlowControlEvidence
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return ErrFlowControlEvidence
	}
	return nil
}
func (c *nativeFlowControl) command(name string, params any) error {
	return c.call(map[string]any{"type": "command", "method": name, "params": params}, nil)
}
func (c *nativeFlowControl) call(request map[string]any, result any) error {
	c.sequence++
	request["id"] = c.sequence
	if err := json.NewEncoder(c.conn).Encode(request); err != nil {
		return ErrFlowControlPending
	}
	if !c.scan.Scan() {
		return ErrFlowControlPending
	}
	var response struct {
		API    string          `json:"api_id"`
		ID     uint64          `json:"id"`
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(c.scan.Bytes(), &response); err != nil {
		return ErrFlowControlEvidence
	}
	if response.API != "zero.api.v1" || response.ID != c.sequence {
		return ErrFlowControlEvidence
	}
	if !response.OK {
		if response.Error != nil && (response.Error.Code == "unsupported" || response.Error.Code == "permission_denied" || response.Error.Code == "unknown_method") {
			return ErrFlowControlUnavailable
		}
		return ErrFlowControlPending
	}
	if result != nil {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return ErrFlowControlEvidence
		}
	}
	return nil
}
