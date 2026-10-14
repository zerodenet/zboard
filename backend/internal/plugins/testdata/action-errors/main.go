package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.action-errors", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.ui.page.v1", "zboard.ui.slot.v1", "zboard.config.v1"}}, nil
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
func (*server) HandlePageAction(_ context.Context, r *pluginv1.PageActionRequest) (*pluginv1.PageActionResponse, error) {
	switch r.Action {
	case "reject.retry":
		return nil, pluginv1.NewActionError("resource_busy", "Resource is busy. Please try later.", true)
	case "reject.permanent":
		return nil, pluginv1.NewActionError("operation_closed", "此操作已关闭，请保留输入并联系管理员。", false)
	case "reject.html":
		return nil, pluginv1.NewActionError("public_text", `<img src=x onerror="alert('unsafe')"> is plain text.`, false)
	case "reject.namespace":
		return nil, pluginv1.NewActionError("session_expired", "Plugin-specific reason, not host authentication.", true)
	case "reject.long":
		return nil, pluginv1.NewActionError("too_long", strings.Repeat("private", 200), true)
	case "reject.malformed":
		s, _ := status.New(codes.FailedPrecondition, "internal-secret").WithDetails(&errdetails.ErrorInfo{Domain: pluginv1.ActionErrorDomain, Reason: "bad code", Metadata: map[string]string{"public_message": "internal-secret", "retryable": "yes"}})
		return nil, s.Err()
	case "reject.legacy":
		return nil, status.Error(codes.FailedPrecondition, "internal-secret stack token=password")
	case "reject.timeout":
		return nil, status.Error(codes.DeadlineExceeded, "internal-secret timeout")
	case "reject.slow":
		time.Sleep(200 * time.Millisecond)
		return nil, pluginv1.NewActionError("late_result", "Must not survive session revocation.", true)
	case "reject.exit":
		os.Exit(2)
	}
	raw, _ := json.Marshal(map[string]any{"action": r.Action, "payload": json.RawMessage(r.PayloadJson), "slot": r.Slot, "actor_id": r.ActorId})
	return &pluginv1.PageActionResponse{ResultJson: raw}, nil
}
func main() { pluginv1.Serve(&server{}) }
