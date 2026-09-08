package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/grpc"
)

type process struct {
	client    *hcplugin.Client
	api       pluginv1.PluginControlClient
	directory string
}

func startProcess(ctx context.Context, root string, p *Package) (*process, error) {
	rel := p.Manifest.Components.Server.Executables[runtime.GOOS+"-"+runtime.GOARCH]
	checksum, _ := hex.DecodeString(p.Manifest.Files[rel])
	directory, err := os.MkdirTemp(filepath.Join(root, "sockets"), "run-")
	if err != nil {
		return nil, err
	}
	binary := filepath.Join(directory, "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(binary, p.Files[rel], 0500); err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(directory)
		}
	}()
	cmd := exec.Command(binary)
	cmd.Dir = directory
	client := hcplugin.NewClient(&hcplugin.ClientConfig{
		HandshakeConfig: pluginv1.Handshake, Plugins: pluginv1.ClientMap(), Cmd: cmd,
		AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolGRPC}, AutoMTLS: true, SkipHostEnv: true,
		SecureConfig: &hcplugin.SecureConfig{Checksum: checksum, Hash: sha256.New()}, StartTimeout: 10 * time.Second,
		Logger: hclog.NewNullLogger(), SyncStdout: io.Discard, SyncStderr: io.Discard,
		UnixSocketConfig: &hcplugin.UnixSocketConfig{TempDir: filepath.Join(root, "sockets")},
		GRPCDialOptions:  []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(128<<10), grpc.MaxCallSendMsgSize(256<<10))},
	})
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, errors.New("plugin process failed to start")
	}
	raw, err := rpc.Dispense("control")
	if err != nil {
		client.Kill()
		return nil, errors.New("plugin protocol negotiation failed")
	}
	proc := &process{client: client, api: raw.(pluginv1.PluginControlClient), directory: directory}
	info, err := proc.api.GetInfo(ctx, &pluginv1.Empty{})
	if err != nil || info.Id != p.Manifest.ID || info.Version != p.Manifest.Version || info.Protocol != 1 || len(info.Capabilities) != len(p.Manifest.Capabilities) {
		proc.close()
		return nil, errors.New("plugin identity does not match signed manifest")
	}
	for _, c := range info.Capabilities {
		if !slices.Contains(p.Manifest.Capabilities, c) {
			proc.close()
			return nil, errors.New("runtime requests unsupported capability")
		}
	}
	health, err := proc.api.Health(ctx, &pluginv1.Empty{})
	if err != nil || !health.Healthy {
		proc.close()
		return nil, errors.New("plugin health check failed")
	}
	ok = true
	return proc, nil
}
func (p *process) close() {
	if p != nil {
		p.client.Kill()
		os.RemoveAll(p.directory)
	}
}
func (p *process) apply(ctx context.Context, raw []byte, rev uint64, previous ...[]byte) ([]byte, error) {
	request := &pluginv1.ConfigRequest{ConfigJson: raw, Revision: rev}
	if len(previous) > 0 {
		request.PreviousConfigJson = previous[0]
	}
	normalized, err := p.api.ValidateConfig(ctx, request)
	if err != nil || normalized == nil || validConfig(normalized.NormalizedJson) != nil {
		return nil, errors.New("plugin rejected configuration")
	}
	res, err := p.api.ApplyConfig(ctx, &pluginv1.ConfigRequest{ConfigJson: normalized.NormalizedJson, Revision: rev})
	if err != nil || !res.Healthy {
		return nil, errors.New("plugin could not apply configuration")
	}
	return normalized.NormalizedJson, nil
}
