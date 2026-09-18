// Package catalog defines transport-neutral, authorized business operations.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	capabilityapi "github.com/zerodenet/zboard/backend/pkg/capabilityapi/v1"
)

var (
	ErrDenied      = errors.New("capability access denied")
	ErrUnknown     = errors.New("unknown capability")
	ErrInput       = errors.New("invalid capability input")
	ErrConflict    = errors.New("capability conflict")
	ErrRateLimited = errors.New("capability rate limited")
	ErrUnavailable = errors.New("capability unavailable")
)

const MaxInputBytes = 64 * 1024

// Credential is opaque to the catalog. Transport adapters pass proof to an
// authority implementation; caller-supplied account IDs are never grants.
type Credential struct{ Kind, Proof string }

type Principal struct {
	Kind         string
	Subject      string
	AccountID    uint
	CredentialID uint
	PluginID     string
	Generation   uint64
}

type Grant struct {
	Principal      Principal
	Administrative bool
}

type Authority interface {
	// Resolve must check current credential revocation, scopes and, for plugins,
	// installation generation. It is called again for every listing/invocation.
	Resolve(context.Context, Credential, string) (Grant, error)
}

type Admission interface {
	Admit(context.Context, Credential, Grant, Descriptor) error
}
type Descriptor struct {
	Name               string          `json:"name"`
	Version            string          `json:"version"`
	Owner              string          `json:"owner"`
	Kind               string          `json:"kind"`
	InputSchema        json.RawMessage `json:"input_schema"`
	OutputSchema       json.RawMessage `json:"output_schema"`
	Sensitivity        string          `json:"sensitivity"`
	Authorization      string          `json:"authorization"`
	Idempotency        string          `json:"idempotency"`
	Execution          string          `json:"execution"`
	TimeoutMillis      int64           `json:"timeout_ms"`
	Quota              string          `json:"quota"`
	RateLimitPerMinute int             `json:"rate_limit_per_minute"`
	ErrorCodes         []string        `json:"error_codes"`
	Compatibility      string          `json:"compatibility"`
	Deprecation        string          `json:"deprecation"`
}
type Handler func(context.Context, Grant, json.RawMessage) (any, error)
type operation struct {
	descriptor Descriptor
	handler    Handler
}
type Registry struct {
	mu         sync.RWMutex
	operations map[string]operation
	authority  Authority
	admission  Admission
}

func New(authority Authority, admission ...Admission) *Registry {
	registry := &Registry{authority: authority, operations: map[string]operation{}}
	if len(admission) > 0 {
		registry.admission = admission[0]
	}
	return registry
}
func clone(d Descriptor) Descriptor {
	d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
	d.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
	d.ErrorCodes = append([]string(nil), d.ErrorCodes...)
	return d
}
func (r *Registry) Register(d Descriptor, h Handler) error {
	if d.Name == "" || d.Version == "" || d.Owner == "" || d.Kind == "" || d.Sensitivity == "" || d.Authorization == "" || d.Idempotency == "" || d.Execution == "" || d.Quota == "" || d.RateLimitPerMinute < 1 || d.RateLimitPerMinute > 10000 || d.Compatibility == "" || d.Deprecation == "" || len(d.ErrorCodes) == 0 || d.TimeoutMillis < 1 || d.TimeoutMillis > 300000 || !json.Valid(d.InputSchema) || !json.Valid(d.OutputSchema) || h == nil {
		return errors.New("incomplete capability descriptor")
	}
	for _, code := range d.ErrorCodes {
		if !capabilityapi.IsErrorCode(code) {
			return fmt.Errorf("unsupported capability error code: %s", code)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.operations[d.Name]; ok {
		return fmt.Errorf("capability already registered: %s", d.Name)
	}
	r.operations[d.Name] = operation{clone(d), h}
	return nil
}
func (r *Registry) List(ctx context.Context, c Credential) ([]Descriptor, error) {
	if r.authority == nil {
		return nil, ErrDenied
	}
	r.mu.RLock()
	ops := make([]Descriptor, 0, len(r.operations))
	for _, op := range r.operations {
		ops = append(ops, clone(op.descriptor))
	}
	r.mu.RUnlock()
	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
	visible := make([]Descriptor, 0, len(ops))
	for _, d := range ops {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		grant, err := r.authority.Resolve(ctx, c, d.Name)
		if errors.Is(err, ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !validPrincipal(grant.Principal) {
			return nil, ErrUnavailable
		}
		visible = append(visible, d)
	}
	return visible, nil
}
func (r *Registry) Invoke(ctx context.Context, c Credential, name string, input json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.authority == nil {
		return nil, ErrDenied
	}
	r.mu.RLock()
	op, ok := r.operations[name]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknown
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(op.descriptor.TimeoutMillis)*time.Millisecond)
	defer cancel()
	grant, err := r.authority.Resolve(callCtx, c, name)
	if err != nil {
		return nil, err
	}
	if !validPrincipal(grant.Principal) {
		return nil, ErrUnavailable
	}
	if len(input) == 0 || len(input) > MaxInputBytes || !json.Valid(input) {
		return nil, ErrInput
	}
	if r.admission == nil {
		return nil, ErrUnavailable
	}
	if err := r.admission.Admit(callCtx, c, grant, op.descriptor); err != nil {
		return nil, err
	}
	if err := callCtx.Err(); err != nil {
		return nil, err
	}
	result, err := op.handler(callCtx, grant, append(json.RawMessage(nil), input...))
	if err != nil {
		return nil, err
	}
	if err := callCtx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func validPrincipal(principal Principal) bool {
	if principal.AccountID == 0 || strings.TrimSpace(principal.Subject) == "" || len(principal.Subject) > 191 {
		return false
	}
	switch principal.Kind {
	case "integration":
		return principal.CredentialID > 0 && principal.PluginID == "" && principal.Generation == 0
	case "plugin_session":
		return principal.CredentialID == 0 && strings.TrimSpace(principal.PluginID) != "" && principal.Generation > 0
	default:
		return false
	}
}
