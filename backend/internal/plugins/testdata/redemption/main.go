// This native test fixture owns no real coupons or persistent value. Its in-memory
// replies exercise host admission and wire fences, not provider durability.
package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type observation struct {
	Calls          uint64          `json:"calls"`
	Generation     uint64          `json:"generation"`
	ConfigRevision uint64          `json:"config_revision"`
	Request        json.RawMessage `json:"request"`
}

type server struct {
	pluginv1.UnimplementedPluginControlServer
	pluginv1.UnimplementedCommerceQuoteProviderServer
	mu      sync.Mutex
	seen    observation
	release chan struct{}
	states  map[string]pluginv1.DiscountRedemptionResult
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.redemption", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1", pluginv1.CommerceQuoteProviderCapability, pluginv1.DiscountRedemptionCapability}}, nil
}

func (s *server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.seen)
	return &pluginv1.HealthResult{Healthy: true, Message: string(raw)}, nil
}

func (*server) ValidateConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: r.ConfigJson}, nil
}

func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}

// Test-only barrier control on the authenticated host transport. The manifest
// grants no UI surface or page capability; no browser can invoke this endpoint.
func (s *server) HandlePageAction(_ context.Context, r *pluginv1.PageActionRequest) (*pluginv1.PageActionResponse, error) {
	if r.Action != "fixture.release" {
		return nil, status.Error(codes.Unimplemented, "fixture control only")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release != nil {
		close(s.release)
		s.release = nil
	}
	return &pluginv1.PageActionResponse{ResultJson: []byte(`{"ok":true}`)}, nil
}

func (*server) EvaluateCommerceQuote(_ context.Context, r *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
	in, err := r.DiscountContext()
	if err != nil {
		return nil, err
	}
	terms := `{"discount_minor":2000,"kind":"native-host-fixture"}`
	hash, _ := pluginv1.CommerceDiscountTermsHash(terms)
	return pluginv1.CommerceDiscountResult(pluginv1.CommerceDiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: 2000, ExpiresAt: time.Now().UTC().Add(time.Minute), Redemption: pluginv1.DiscountRedemptionGuarantee, Agreement: &pluginv1.CommerceDiscountAgreement{ID: "finite", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}})
}

func (s *server) ExecuteCommerceRedemption(ctx context.Context, r *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
	in, err := r.DiscountRedemption()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.seen.Calls++
	s.seen.Generation, s.seen.ConfigRevision = r.Generation, r.ConfigRevision
	s.seen.Request = append(json.RawMessage(nil), r.ContextJson...)
	var release <-chan struct{}
	if strings.HasPrefix(in.Binding.AgreementID, "wait-") {
		s.release = make(chan struct{})
		release = s.release
	}
	s.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if in.Binding.AgreementID == "unimplemented" {
		return nil, status.Error(codes.Unimplemented, "redemption unavailable")
	}
	if in.Binding.AgreementID == "transport-deadline" {
		return nil, status.Error(codes.DeadlineExceeded, "transport deadline before caller timer")
	}
	if in.Binding.AgreementID == "transport-canceled" {
		return nil, status.Error(codes.Canceled, "transport canceled before caller timer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, exists := s.states[in.Binding.Key]
	if !exists {
		out = pluginv1.DiscountRedemptionResult{Version: 1, Key: in.Binding.Key, BindingHash: in.BindingHash, State: "absent", Guarantee: pluginv1.DiscountRedemptionGuarantee}
	}
	if out.BindingHash != in.BindingHash {
		return nil, status.Error(codes.FailedPrecondition, "binding changed")
	}
	switch in.Action {
	case "reserve":
		if out.State == "absent" {
			out.State, out.Token = "reserved", "fixture-exclusive-"+in.Binding.Key
		}
	case "commit":
		// Recovery authorization tests deliberately synthesize a valid protocol
		// reply after process restart; this is no proof of persistent stock.
		out.State, out.Token = "committed", in.Token
	case "release":
		if out.State != "committed" {
			out.State = "released"
		}
	}
	s.states[in.Binding.Key] = out
	switch in.Binding.AgreementID {
	case "wrong-hash":
		out.BindingHash = strings.Repeat("0", 64)
	case "wrong-key":
		out.Key = strings.Repeat("0", 64)
	case "wrong-token":
		out.Token = "different-hold"
	case "long-token":
		out.Token = strings.Repeat("x", 513)
	case "long-message":
		out.Message = strings.Repeat("x", 513)
	case "wrong-state":
		out.State = "approved-payment"
	case "wrong-guarantee":
		out.Guarantee = "expires-without-core"
	case "wrong-version":
		out.Version = 2
	case "oversized":
		return &pluginv1.CommerceQuoteResult{OfferJson: []byte(strings.Repeat(" ", pluginv1.MaxDiscountRedemptionBytes+1))}, nil
	case "empty":
		return &pluginv1.CommerceQuoteResult{}, nil
	}
	raw, _ := json.Marshal(out)
	if in.Binding.AgreementID == "unknown-field" {
		raw = append(raw[:len(raw)-1], []byte(`,"payment_authority":true}`)...)
	}
	if in.Binding.AgreementID == "trailing-json" {
		raw = append(raw, []byte(` {}`)...)
	}
	return &pluginv1.CommerceQuoteResult{OfferJson: raw}, nil
}

func main() { pluginv1.Serve(&server{states: map[string]pluginv1.DiscountRedemptionResult{}}) }
