package zero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const controlSocket = "/run/zerodenet/control.sock"

type ConfigurationPublishRequest struct {
	Namespace     string
	DeploymentID  uint
	RuntimeConfig []byte
	ConfigSHA256  string
	ConnectorKey  string
}

type ConfigurationPublishResult struct {
	Output         string
	ActivatedAt    time.Time
	Committed      bool
	MayHaveApplied bool
}

type ConfigurationPublisher struct {
	Dialer     KernelRemoteDialer
	NewStageID func() string
	Now        func() time.Time
}

func (p ConfigurationPublisher) Publish(ctx context.Context, request ConfigurationPublishRequest) (ConfigurationPublishResult, error) {
	if p.Dialer == nil || (request.Namespace != "" && request.Namespace != "kernel") || request.DeploymentID == 0 || len(request.RuntimeConfig) == 0 || !kernelSHA256Pattern.MatchString(request.ConfigSHA256) || request.ConnectorKey == "" {
		return ConfigurationPublishResult{}, errors.New("invalid Zero configuration publication request")
	}
	session, err := p.Dialer.Dial(ctx)
	if err != nil {
		return ConfigurationPublishResult{}, err
	}
	defer session.Close()
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()
	info, err := session.Run("/usr/local/bin/zero build-info", true)
	if err != nil {
		if unsupportedConfigurationCapability(info) {
			return ConfigurationPublishResult{Output: info}, fmt.Errorf("%w：%s", ErrConfigurationCapabilityUnavailable, truncateOutput(info))
		}
		return ConfigurationPublishResult{}, operationError(ctx, err)
	}
	capabilities, err := ParseNativeBuildInfo(info, request.RuntimeConfig)
	if err != nil {
		return ConfigurationPublishResult{}, err
	}
	if capabilities.FeaturesDeclared && !capabilities.Features["status-api"] {
		return ConfigurationPublishResult{}, fmt.Errorf("%w：当前内核未编译配置热更新控制接口", ErrConfigurationCapabilityUnavailable)
	}
	if RequiresDirectInboundUDP(request.RuntimeConfig) {
		output := info
		if !SupportsDirectInboundUDP(output) {
			if err := ctx.Err(); err != nil {
				return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, err
			}
			return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, fmt.Errorf("%w：未确认 direct 入站 UDP 能力，未应用入口 UDP 设置", ErrConfigurationCapabilityUnavailable)
		}
	}
	stageID := uuid.NewString()
	if p.NewStageID != nil {
		stageID = p.NewStageID()
	}
	if strings.TrimSpace(stageID) == "" {
		return ConfigurationPublishResult{}, errors.New("empty Zero configuration staging identity")
	}
	stage := "/tmp/zboard-zero-config-" + stageID
	if output, err := session.Run("install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, operationError(ctx, fmt.Errorf("create Zero config staging directory: %w: %s", err, truncateOutput(output)))
	}
	if err := session.Upload(stage+"/runtime.json", "0600", request.RuntimeConfig); err != nil {
		return ConfigurationPublishResult{}, operationError(ctx, err)
	}
	if err := session.Upload(stage+"/zero.env", "0600", []byte("ZERO_PANEL_API_KEY="+request.ConnectorKey+"\n")); err != nil {
		return ConfigurationPublishResult{}, operationError(ctx, err)
	}
	activatedAt := time.Now().UTC()
	if p.Now != nil {
		activatedAt = p.Now().UTC()
	}
	script := BuildConfigurationPublishScript(stage, request.ConfigSHA256, request.DeploymentID)
	if request.Namespace == "kernel" {
		script = buildConfigurationPublishScript(stage, request.ConfigSHA256, request.DeploymentID, "kernel-config")
	}
	// This command owns a separate remote process group and a stdin lifeline.
	// Give its watchdog time to stop CLI children and release flock before SSH
	// closes; closing the transport alone leaves remote commands orphaned.
	stop()
	output, err := session.RunWithLifetime(ctx, script, true)
	if err != nil {
		if configurationConfirmedUnapplied(output) && (strings.Contains(output, "ZBOARD_CONFIG_HOT_APPLY_UNAVAILABLE") || unsupportedConfigurationCapability(output)) {
			return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, fmt.Errorf("%w：%s", ErrConfigurationCapabilityUnavailable, truncateOutput(output))
		}
		return ConfigurationPublishResult{Output: strings.TrimSpace(output), ActivatedAt: activatedAt, Committed: strings.Contains(output, "ZBOARD_CONFIG_COMMITTED="+request.ConfigSHA256), MayHaveApplied: !configurationConfirmedUnapplied(output)}, operationError(ctx, fmt.Errorf("apply Zero config without restarting the service: %w: %s", err, truncateOutput(output)))
	}
	if err := operationError(ctx, nil); err != nil {
		return ConfigurationPublishResult{MayHaveApplied: true}, err
	}
	if !strings.Contains(output, "ZBOARD_CONFIG_APPLIED="+request.ConfigSHA256) {
		return ConfigurationPublishResult{MayHaveApplied: true}, errors.New("Zero configuration apply was not confirmed")
	}
	return ConfigurationPublishResult{Output: strings.TrimSpace(output), ActivatedAt: activatedAt}, nil
}

func unsupportedConfigurationCapability(output string) bool {
	message := strings.ToLower(output)
	for _, fact := range []string{"unknown field", "not compiled", "unsupported config schema", "unrecognized subcommand", "unknown command"} {
		if strings.Contains(message, fact) {
			return true
		}
	}
	return false
}

func configurationConfirmedUnapplied(output string) bool {
	if strings.Contains(output, "ZBOARD_CONFIG_COMMITTED=") {
		return false
	}
	for _, marker := range []string{"ZBOARD_CONFIG_SERVICE_UNAVAILABLE", "ZBOARD_CONFIG_CONTROL_UNAVAILABLE", "ZBOARD_CONFIG_HOT_APPLY_UNAVAILABLE", "ZBOARD_CONFIG_VALIDATION_REJECTED_CURRENT_UNCHANGED"} {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return false
}

func RequiresDirectInboundUDP(raw []byte) bool {
	type policy struct {
		Enabled *bool `json:"enabled"`
	}
	var config struct {
		Runtime struct {
			UDP policy `json:"udp"`
		} `json:"runtime"`
		Inbounds []struct {
			UDP      policy `json:"udp"`
			Protocol struct {
				Type string `json:"type"`
			} `json:"protocol"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return false
	}
	if config.Runtime.UDP.Enabled != nil && !*config.Runtime.UDP.Enabled {
		return false
	}
	for _, inbound := range config.Inbounds {
		if inbound.Protocol.Type == "direct" && (inbound.UDP.Enabled == nil || *inbound.UDP.Enabled) {
			return true
		}
	}
	return false
}

func SupportsDirectInboundUDP(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		raw, found := strings.CutPrefix(strings.TrimSpace(line), "protocol_capabilities:")
		if !found {
			continue
		}
		var protocols []struct {
			Protocol string `json:"protocol"`
			Compiled bool   `json:"compiled"`
			Inbound  struct {
				UDP struct {
					Supported bool `json:"supported"`
				} `json:"udp"`
			} `json:"inbound"`
		}
		if json.Unmarshal([]byte(raw), &protocols) != nil {
			return false
		}
		for _, protocol := range protocols {
			if protocol.Protocol == "direct" {
				return protocol.Compiled && protocol.Inbound.UDP.Supported
			}
		}
	}
	return false
}
