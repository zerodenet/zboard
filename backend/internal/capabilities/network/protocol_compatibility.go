package network

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrProtocolCompatibilityUnavailable = errors.New("protocol compatibility capability unavailable")

type ProtocolCompatibilityEndpoint struct {
	ID       uint
	Protocol string
}

type ProtocolCompatibilityRepository interface {
	LoadProtocolCompatibilityEndpoints(context.Context, []uint) ([]ProtocolCompatibilityEndpoint, error)
}

type ProtocolCompatibility struct {
	Repository ProtocolCompatibilityRepository
}

func (s ProtocolCompatibility) ValidateEndpoints(ctx context.Context, endpointIDs []uint) error {
	ids := uniqueProtocolCompatibilityIDs(endpointIDs)
	if len(ids) == 0 {
		return nil
	}
	if s.Repository == nil {
		return ErrProtocolCompatibilityUnavailable
	}
	endpoints, err := s.Repository.LoadProtocolCompatibilityEndpoints(ctx, ids)
	if err != nil {
		return err
	}
	if len(endpoints) != len(ids) {
		return errors.New("one or more protocol endpoints do not exist")
	}
	for _, endpoint := range endpoints {
		if !IsRuntimeProtocolSupported(endpoint.Protocol) {
			return fmt.Errorf("protocol endpoint %d cannot be used: 面板无法识别该协议。", endpoint.ID)
		}
	}
	return nil
}

func IsRuntimeProtocolSupported(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vmess", "vless", "trojan", "shadowsocks", "hysteria2", "mieru":
		return true
	default:
		return false
	}
}

func uniqueProtocolCompatibilityIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
