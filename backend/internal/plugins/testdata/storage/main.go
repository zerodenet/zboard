package main

import (
	"context"
	"encoding/json"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.storage", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1", "zboard.storage.v1"}}, nil
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
func (*server) TestConfig(ctx context.Context, _ *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	client, err := pluginv1.HostStorageFromEnvironment()
	if err != nil {
		return nil, err
	}
	defer client.Close()
	current, err := client.Get(ctx, "background")
	if err != nil {
		return nil, err
	}
	_, err = client.Put(ctx, "background", current.Revision, json.RawMessage(`{"written_by":"native-plugin"}`))
	return &pluginv1.HealthResult{Healthy: err == nil}, err
}
func main() { pluginv1.Serve(&server{}) }
