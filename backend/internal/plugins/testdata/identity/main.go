package main

import (
	"context"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.identity", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1", "zboard.identity.provider.v1"}}, nil
}
func (*server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) ValidateConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: r.ConfigJson}, nil
}
func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) GetIdentityProvider(_ context.Context, r *pluginv1.IdentityProviderRequest) (*pluginv1.IdentityProvider, error) {
	return &pluginv1.IdentityProvider{Issuer: "https://id.example.test", AuthorizationEndpoint: "https://id.example.test/auth", ClientId: "test", Scopes: []string{"openid"}, ProviderId: r.ProviderId}, nil
}
func (*server) ExchangeIdentity(_ context.Context, r *pluginv1.IdentityExchange) (*pluginv1.VerifiedIdentity, error) {
	subject := "subject"
	if r.Code == "invalid" {
		subject = ""
	}
	return &pluginv1.VerifiedIdentity{Issuer: r.Issuer, Subject: subject}, nil
}
func main() { pluginv1.Serve(&server{}) }

func (*server) ListIdentityProviders(context.Context, *pluginv1.Empty) (*pluginv1.IdentityProviderList, error) {
	return &pluginv1.IdentityProviderList{Providers: []*pluginv1.IdentityProviderOption{{Id: "", Name: "Legacy"}, {Id: "second", Name: "Second"}}}, nil
}
