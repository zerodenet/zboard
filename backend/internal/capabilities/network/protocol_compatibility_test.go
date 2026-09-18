package network

import (
	"context"
	"strings"
	"testing"
)

type protocolCompatibilityRepositoryStub struct {
	endpoints []ProtocolCompatibilityEndpoint
	ids       []uint
}

func (s *protocolCompatibilityRepositoryStub) LoadProtocolCompatibilityEndpoints(_ context.Context, ids []uint) ([]ProtocolCompatibilityEndpoint, error) {
	s.ids = append([]uint(nil), ids...)
	return s.endpoints, nil
}

func TestProtocolCompatibilityValidatesPersistedEndpoints(t *testing.T) {
	repository := &protocolCompatibilityRepositoryStub{endpoints: []ProtocolCompatibilityEndpoint{{ID: 2, Protocol: "VLESS"}, {ID: 7, Protocol: "mieru"}}}
	service := ProtocolCompatibility{Repository: repository}
	if err := service.ValidateEndpoints(context.Background(), []uint{7, 2, 7}); err != nil {
		t.Fatal(err)
	}
	if len(repository.ids) != 2 || repository.ids[0] != 2 || repository.ids[1] != 7 {
		t.Fatalf("normalized ids = %v", repository.ids)
	}
	repository.endpoints = []ProtocolCompatibilityEndpoint{{ID: 2, Protocol: "unknown"}, {ID: 7, Protocol: "mieru"}}
	if err := service.ValidateEndpoints(context.Background(), []uint{2, 7}); err == nil || !strings.Contains(err.Error(), "endpoint 2") {
		t.Fatalf("unsupported protocol error = %v", err)
	}
	repository.endpoints = repository.endpoints[:1]
	if err := service.ValidateEndpoints(context.Background(), []uint{2, 7}); err == nil || !strings.Contains(err.Error(), "do not exist") {
		t.Fatalf("missing endpoint error = %v", err)
	}
}

func TestRuntimeProtocolSupportIsCaseInsensitive(t *testing.T) {
	for _, protocol := range []string{"vmess", " VLESS ", "Trojan", "SHADOWSOCKS", "hysteria2", "Mieru"} {
		if !IsRuntimeProtocolSupported(protocol) {
			t.Fatalf("protocol %q rejected", protocol)
		}
	}
	if IsRuntimeProtocolSupported("unknown") {
		t.Fatal("unknown protocol accepted")
	}
}
