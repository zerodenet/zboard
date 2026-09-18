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
	return &pluginv1.Info{Id: "example.tasks", Version: pluginVersion, Protocol: 1, Capabilities: []string{"zboard.config.v1", "zboard.task.v1"}}, nil
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
func (*server) RunTask(ctx context.Context, r *pluginv1.TaskRunRequest) (*pluginv1.TaskRunResult, error) {
	if r.TaskId == "host" {
		host, err := pluginv1.HostTasksFromEnvironment()
		if err != nil {
			return nil, err
		}
		defer host.Close()
		first, err := host.Submit(ctx, "success", "native-host-fixture")
		if err != nil {
			return nil, err
		}
		again, err := host.Submit(ctx, "success", "native-host-fixture")
		if err != nil {
			return nil, err
		}
		if _, err := host.Submit(ctx, "undeclared", "forbidden"); err == nil {
			return &pluginv1.TaskRunResult{Succeeded: false}, nil
		}
		page, err := host.List(ctx, 100, 0)
		if err != nil {
			return nil, err
		}
		found := false
		for _, item := range page.Items {
			if item.ID == first.ID {
				found = true
			}
		}
		return &pluginv1.TaskRunResult{Succeeded: found && first.ID != "" && first.ID == again.ID}, nil
	}
	if r.TaskId == "slow" || r.TaskId == "slow-disable" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &pluginv1.TaskRunResult{Succeeded: r.TaskId == "success" && r.RunId != "" && r.Generation > 0}, nil
}
func main() { pluginv1.Serve(&server{}) }
