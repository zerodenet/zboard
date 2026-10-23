package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
	pluginv1.UnimplementedCommerceQuoteProviderServer
	started atomic.Uint64
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.quote", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1", pluginv1.CommerceQuoteProviderCapability}}, nil
}
func (s *server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true, Message: fmt.Sprint(s.started.Load())}, nil
}
func (*server) ValidateConfig(_ context.Context, request *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: request.ConfigJson}, nil
}
func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (s *server) EvaluateCommerceQuote(ctx context.Context, request *pluginv1.CommerceQuoteRequest) (*pluginv1.CommerceQuoteResult, error) {
	in, err := request.DiscountContext()
	if err != nil {
		return nil, err
	}
	s.started.Add(1)
	if in.Selection == "slow" {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	terms := `{"discount_minor":2000,"kind":"read-only-fixture"}`
	if in.Selection == "one-order" {
		terms = `{"discount_minor":200,"kind":"read-only-fixture"}`
	}
	hash, _ := pluginv1.CommerceDiscountTermsHash(terms)
	agreement := pluginv1.CommerceDiscountAgreement{ID: "fixture", Version: "1", Scope: "initial_only", TermsHash: hash, TermsJSON: terms}
	if in.Selection == "recurring" {
		agreement.Scope = "recurring"
	}
	if in.Agreement != nil {
		agreement = *in.Agreement
	}
	discount := int64(2000)
	if in.Selection == "one-order" {
		agreement.Scope = "order_only"
		discount = 200
	}
	return pluginv1.CommerceDiscountResult(pluginv1.CommerceDiscountOffer{Status: "available", InputFingerprint: in.InputFingerprint, Currency: in.Currency, DiscountMinor: discount, ExpiresAt: time.Now().Add(time.Minute), Agreement: &agreement})
}
func main() { pluginv1.Serve(&server{}) }
