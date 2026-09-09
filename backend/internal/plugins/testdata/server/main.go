package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
	mu    sync.Mutex
	value map[string]any
}

func (s *server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.server", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1"}}, nil
}
func (s *server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (s *server) ValidateConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	var obj map[string]any
	if json.Unmarshal(r.ConfigJson, &obj) != nil || obj == nil {
		return nil, errors.New("invalid")
	}
	if obj["reject"] == true {
		return nil, errors.New("rejected")
	}
	return &pluginv1.ConfigResult{NormalizedJson: r.ConfigJson}, nil
}
func (s *server) ApplyConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.Unmarshal(r.ConfigJson, &s.value); err != nil {
		return nil, err
	}
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (s *server) TestConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &pluginv1.HealthResult{Healthy: s.value["healthy"] != false}, nil
}
func main() { pluginv1.Serve(&server{}) }
