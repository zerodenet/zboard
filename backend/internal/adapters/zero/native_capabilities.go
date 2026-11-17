package zero

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

// Default artifact reference and reproducible test baseline, never a minimum
// version required for a node or operation.
const NativeBaselineVersion = "0.0.3-dev.202610102233"

// This means one requested operation cannot be applied; it does not disable a
// node or invalidate its existing kernel/configuration.
var ErrConfigurationCapabilityUnavailable = network.ErrConfigurationCapabilityUnavailable

type NativeBuildInfo struct {
	Version           string
	Prerelease        bool
	Features          map[string]bool
	Protocols         map[string]bool
	FeaturesDeclared  bool
	ProtocolsDeclared bool
}

// Capability facts come from the installed binary, not a host default or an
// administrator-selected version. Version names are diagnostic metadata, not
// capability gates. Unknown declarations are resolved by native validation.
func ParseNativeBuildInfo(output string, config []byte) (NativeBuildInfo, error) {
	info := NativeBuildInfo{Features: map[string]bool{}, Protocols: map[string]bool{}}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "build_id":
			info.Version = strings.TrimPrefix(value, "v")
		case "features":
			info.FeaturesDeclared = true
			for _, feature := range strings.Split(value, ",") {
				info.Features[strings.TrimSpace(feature)] = true
			}
		case "protocol_capabilities":
			info.ProtocolsDeclared = true
			var protocols []struct {
				Protocol string `json:"protocol"`
				Compiled bool   `json:"compiled"`
			}
			if err := json.Unmarshal([]byte(value), &protocols); err != nil {
				return info, fmt.Errorf("invalid Zero protocol capability declaration")
			}
			for _, protocol := range protocols {
				info.Protocols[protocol.Protocol] = protocol.Compiled
			}
		}
	}
	info.Prerelease = strings.Contains(info.Version, "-")
	var document struct {
		API struct {
			EventSinks []json.RawMessage `json:"event_sinks"`
		} `json:"api"`
		Inbounds []struct {
			Protocol struct {
				Type string `json:"type"`
			} `json:"protocol"`
		} `json:"inbounds"`
		Outbounds []struct {
			Protocol struct {
				Type string `json:"type"`
			} `json:"protocol"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(config, &document); err != nil {
		return info, fmt.Errorf("invalid runtime configuration")
	}
	if len(document.API.EventSinks) > 0 && info.FeaturesDeclared && !info.Features["connector"] {
		return info, fmt.Errorf("%w：未编译 connector，未应用事件投递设置", ErrConfigurationCapabilityUnavailable)
	}
	for _, inbound := range document.Inbounds {
		if info.ProtocolsDeclared && !info.Protocols[inbound.Protocol.Type] {
			return info, fmt.Errorf("%w：未编译入站协议 %s", ErrConfigurationCapabilityUnavailable, inbound.Protocol.Type)
		}
	}
	for _, outbound := range document.Outbounds {
		if info.ProtocolsDeclared && !info.Protocols[outbound.Protocol.Type] {
			return info, fmt.Errorf("%w：未编译出站协议 %s", ErrConfigurationCapabilityUnavailable, outbound.Protocol.Type)
		}
	}
	return info, nil
}
