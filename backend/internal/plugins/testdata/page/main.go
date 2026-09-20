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
	return &pluginv1.Info{Id: "example.page", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.ui.page.v1", "zboard.config.v1"}}, nil
}

func (*server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}

func (*server) ValidateConfig(_ context.Context, request *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: request.ConfigJson}, nil
}

func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}

func (*server) HandlePageAction(_ context.Context, request *pluginv1.PageActionRequest) (*pluginv1.PageActionResponse, error) {
	result, _ := json.Marshal(map[string]any{
		"page_id": request.PageId, "surface": request.Surface, "actor_id": request.ActorId,
		"actor_admin": request.ActorAdmin, "action": request.Action, "payload": json.RawMessage(request.PayloadJson),
	})
	return &pluginv1.PageActionResponse{ResultJson: result}, nil
}

func main() { pluginv1.Serve(&server{}) }
