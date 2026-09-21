package main

import (
	"context"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

var pluginVersion = "1.0.0"

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{
		Id: "example.route", Version: pluginVersion, Protocol: 1,
		Capabilities: []string{"zboard.config.v1", "zboard.http.route.v1"},
	}, nil
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

func (*server) HandleHTTP(_ context.Context, request *pluginv1.HTTPRequest) (*pluginv1.HTTPResponse, error) {
	valid := request.Method == "GET" && ((request.RouteId == "discovery" && request.Path == "/.well-known/example/v1/discovery") ||
		(request.RouteId == "replacement" && request.Path == "/.well-known/example/v2/discovery"))
	if !valid {
		return &pluginv1.HTTPResponse{Status: 404, ContentType: "application/json", Body: []byte(`{"error":"not_found"}`)}, nil
	}
	return &pluginv1.HTTPResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"available":true}`)}, nil
}

func main() { pluginv1.Serve(&server{}) }
