// Package pluginv1 is the only public server SDK; it has no dependency on host internals.
package pluginv1

import (
	"context"
	hcplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const ProtocolVersion = 1

var Handshake = hcplugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "ZBOARD_PLUGIN", MagicCookieValue: "zboard-plugin-v1"}

type ControlPlugin struct {
	hcplugin.NetRPCUnsupportedPlugin
	Impl PluginControlServer
}

func (p *ControlPlugin) GRPCServer(_ *hcplugin.GRPCBroker, s *grpc.Server) error {
	RegisterPluginControlServer(s, p.Impl)
	return nil
}
func (p *ControlPlugin) GRPCClient(_ context.Context, _ *hcplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return NewPluginControlClient(c), nil
}
func ClientMap() map[string]hcplugin.Plugin {
	return map[string]hcplugin.Plugin{"control": &ControlPlugin{}}
}
func Serve(impl PluginControlServer) {
	hcplugin.Serve(&hcplugin.ServeConfig{HandshakeConfig: Handshake, Plugins: map[string]hcplugin.Plugin{"control": &ControlPlugin{Impl: impl}}, GRPCServer: hcplugin.DefaultGRPCServer})
}
