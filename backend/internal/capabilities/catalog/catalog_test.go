package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type testAuthority struct {
	allowed bool
	calls   int
}

func (a *testAuthority) Resolve(ctx context.Context, c Credential, name string) (Grant, error) {
	a.calls++
	if !a.allowed {
		return Grant{}, ErrDenied
	}
	return Grant{Principal: Principal{Kind: "integration", Subject: "integration:9", AccountID: 7, CredentialID: 9}}, nil
}

type testAdmission struct {
	calls int
	err   error
}

func (a *testAdmission) Admit(_ context.Context, _ Credential, grant Grant, descriptor Descriptor) error {
	a.calls++
	if grant.Principal.AccountID != 7 || descriptor.Name != "sample.read" {
		return ErrUnavailable
	}
	return a.err
}

func TestCatalogChecksEveryCallAndIsolatesDescriptors(t *testing.T) {
	auth := &testAuthority{allowed: true}
	admission := &testAdmission{}
	r := New(auth, admission)
	d := Descriptor{Name: "sample.read", Version: "1", Owner: "sample", Kind: "query", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Sensitivity: "account", Authorization: "account", Idempotency: "read", Execution: "synchronous", TimeoutMillis: 1000, Quota: "bounded", RateLimitPerMinute: 10, ErrorCodes: []string{"permission_denied"}, Compatibility: "v1", Deprecation: "none"}
	calls := 0
	if err := r.Register(d, func(ctx context.Context, g Grant, in json.RawMessage) (any, error) {
		calls++
		if g.Principal.AccountID != 7 || g.Principal.Subject != "integration:9" {
			t.Fatal("wrong grant")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	d.InputSchema[0] = 'x'
	list, err := r.List(context.Background(), Credential{})
	if err != nil || len(list) != 1 || !json.Valid(list[0].InputSchema) {
		t.Fatalf("list: %+v %v", list, err)
	}
	list[0].ErrorCodes[0] = "mutated"
	list, _ = r.List(context.Background(), Credential{})
	if list[0].ErrorCodes[0] != "permission_denied" {
		t.Fatal("descriptor mutation leaked")
	}
	if _, err = r.Invoke(context.Background(), Credential{}, d.Name, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if admission.calls != 1 {
		t.Fatalf("admission calls=%d", admission.calls)
	}
	auth.allowed = false
	if _, err = r.Invoke(context.Background(), Credential{}, d.Name, json.RawMessage(`{}`)); !errors.Is(err, ErrDenied) || calls != 1 {
		t.Fatalf("revoked invoke: %v calls=%d", err, calls)
	}
	list, err = r.List(context.Background(), Credential{})
	if err != nil || len(list) != 0 {
		t.Fatalf("revoked list: %+v %v", list, err)
	}
}

func TestCatalogRequiresBoundPrincipalAndAdmission(t *testing.T) {
	descriptor := Descriptor{Name: "sample.read", Version: "1", Owner: "sample", Kind: "query", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Sensitivity: "account", Authorization: "account", Idempotency: "read", Execution: "synchronous", TimeoutMillis: 1000, Quota: "bounded", RateLimitPerMinute: 1, ErrorCodes: []string{"permission_denied"}, Compatibility: "v1", Deprecation: "none"}
	auth := &testAuthority{allowed: true}
	registry := New(auth)
	if err := registry.Register(descriptor, func(context.Context, Grant, json.RawMessage) (any, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Invoke(context.Background(), Credential{}, descriptor.Name, json.RawMessage(`{}`)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing admission error=%v", err)
	}
	admission := &testAdmission{err: ErrRateLimited}
	registry = New(auth, admission)
	if err := registry.Register(descriptor, func(context.Context, Grant, json.RawMessage) (any, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Invoke(context.Background(), Credential{}, descriptor.Name, json.RawMessage(`{}`)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate error=%v", err)
	}
	if _, err := registry.Invoke(context.Background(), Credential{}, descriptor.Name, json.RawMessage(`{`)); !errors.Is(err, ErrInput) {
		t.Fatalf("invalid input error=%v", err)
	}
	if admission.calls != 1 {
		t.Fatalf("invalid input consumed admission: calls=%d", admission.calls)
	}
	invalid := AuthorityFunc(func(context.Context, Credential, string) (Grant, error) {
		return Grant{Principal: Principal{Kind: "integration", AccountID: 7, CredentialID: 9}}, nil
	})
	registry = New(invalid, admission)
	if err := registry.Register(descriptor, func(context.Context, Grant, json.RawMessage) (any, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.List(context.Background(), Credential{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbound list principal error=%v", err)
	}
}

func TestCatalogRejectsNonCanonicalErrorCodes(t *testing.T) {
	descriptor := Descriptor{Name: "sample.read", Version: "1", Owner: "sample", Kind: "query", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Sensitivity: "account", Authorization: "account", Idempotency: "read", Execution: "synchronous", TimeoutMillis: 1000, Quota: "bounded", RateLimitPerMinute: 1, ErrorCodes: []string{"forbidden"}, Compatibility: "v1", Deprecation: "none"}
	registry := New(&testAuthority{allowed: true}, &testAdmission{})
	if err := registry.Register(descriptor, func(context.Context, Grant, json.RawMessage) (any, error) { return nil, nil }); err == nil {
		t.Fatal("registered non-canonical capability error")
	}
}

type AuthorityFunc func(context.Context, Credential, string) (Grant, error)

func (f AuthorityFunc) Resolve(ctx context.Context, credential Credential, operation string) (Grant, error) {
	return f(ctx, credential, operation)
}
func TestDecodeClosedObject(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	for _, raw := range []string{`null`, `[]`, `{"admin":true}`, `{} {}`} {
		if _, err := DecodeObject[input](json.RawMessage(raw)); !errors.Is(err, ErrInput) {
			t.Fatalf("accepted %s", raw)
		}
	}
}
