package handler

import (
	"encoding/json"
	"strings"
)

// Read the kernel's registered protocol facts, not a guessed version threshold.
func zeroBuildSupportsDirectUDP(output string) bool {
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

func requiresDirectInboundUDP(raw []byte) bool {
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
