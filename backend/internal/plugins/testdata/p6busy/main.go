// A bounded, signed native capacity fixture. It owns no commercial authority.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type config struct {
	CPUBurners int `json:"cpu_burners"`
	MemoryMiB  int `json:"memory_mib"`
}

type server struct {
	pluginv1.UnimplementedPluginControlServer
	mu     sync.Mutex
	stop   context.CancelFunc
	active config
	memory []byte
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "acceptance.p6busy", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1"}}, nil
}
func (s *server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.active)
	return &pluginv1.HealthResult{Healthy: true, Message: string(raw)}, nil
}
func parse(raw []byte) (config, error) {
	var c config
	if json.Unmarshal(raw, &c) != nil || c.CPUBurners < 0 || c.CPUBurners > 2 || c.MemoryMiB < 0 || c.MemoryMiB > 128 {
		return c, errors.New("capacity fixture permits 0-2 CPU burners and 0-128MiB only")
	}
	return c, nil
}
func (*server) ValidateConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	c, err := parse(r.ConfigJson)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(c)
	return &pluginv1.ConfigResult{NormalizedJson: raw}, nil
}
func (s *server) ApplyConfig(_ context.Context, r *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	c, err := parse(r.ConfigJson)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
	}
	ctx, stop := context.WithCancel(context.Background())
	s.stop, s.active = stop, c
	s.memory = make([]byte, c.MemoryMiB<<20)
	for i := range s.memory {
		s.memory[i] = byte(i)
	}
	for i := 0; i < c.CPUBurners; i++ {
		go func() {
			buffer := make([]byte, 64<<10)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					for n := 0; n < 64; n++ {
						digest := sha256.Sum256(buffer)
						copy(buffer, digest[:])
					}
				}
			}
		}()
	}
	// Touch resident pages periodically; allocated bytes are not an RSS claim.
	go func(memory []byte) {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i := 0; i < len(memory); i += 4096 {
					memory[i]++
				}
			}
		}
	}(s.memory)
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func main() { pluginv1.Serve(&server{}) }
