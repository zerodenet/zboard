package handler

import (
	"os"
	"strings"
	"testing"
)

func TestZeroBootstrapControlInboundIsLoopbackEphemeralDirect(t *testing.T) {
	inbound := zeroBootstrapControlInbound()
	if inbound["tag"] != zeroBootstrapInboundTag {
		t.Fatalf("unexpected bootstrap tag: %#v", inbound["tag"])
	}
	listen, ok := inbound["listen"].(map[string]interface{})
	if !ok {
		t.Fatalf("bootstrap listen config has unexpected type: %T", inbound["listen"])
	}
	if listen["address"] != "127.0.0.1" {
		t.Fatalf("bootstrap inbound must be loopback-only: %#v", listen["address"])
	}
	if listen["port"] != 0 {
		t.Fatalf("bootstrap inbound must request an ephemeral port: %#v", listen["port"])
	}
	protocol, ok := inbound["protocol"].(map[string]interface{})
	if !ok {
		t.Fatalf("bootstrap protocol config has unexpected type: %T", inbound["protocol"])
	}
	if protocol["type"] != "direct" {
		t.Fatalf("bootstrap inbound must use direct: %#v", protocol["type"])
	}
	if _, exists := protocol["target"]; exists {
		t.Fatal("bootstrap direct inbound must not define a forwarding target")
	}
}

func TestRuntimeCompilerUsesBootstrapOnlyForEmptyInboundSet(t *testing.T) {
	payload, err := os.ReadFile("kernel_automation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	guard := "if len(inbounds) == 0 {\n\t\tinbounds = append(inbounds, zeroBootstrapControlInbound())\n\t}"
	if !strings.Contains(source, guard) {
		t.Fatal("runtime compiler must inject the bootstrap inbound only when no real inbound was compiled")
	}
}
