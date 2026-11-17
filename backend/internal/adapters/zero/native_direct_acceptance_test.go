package zero

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestNativeDirectIPv6SourceInterfaceAndListen(t *testing.T) {
	binary := os.Getenv("ZERO_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("explicit verified native binary required")
	}
	for _, scenario := range []struct {
		name, listen, family, iface string
		success                     bool
	}{
		{"IPv4 ingress to IPv6 exit", "127.0.0.1", "only_ipv6", "", true},
		{"IPv6 ingress and bound loopback", "::1", "only_ipv6", func() string {
			if runtime.GOOS == "darwin" {
				return "lo0"
			}
			return "lo"
		}(), true},
		{"wrong destination family", "127.0.0.1", "only_ipv4", "", false},
		{"missing interface cannot fall back", "127.0.0.1", "only_ipv6", "zb-no-device", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			echo, err := net.Listen("tcp6", "[::1]:0")
			if err != nil {
				t.Skipf("IPv6 host unavailable: %v", err)
			}
			defer echo.Close()
			go func() {
				for {
					conn, err := echo.Accept()
					if err != nil {
						return
					}
					go func() { defer conn.Close(); io.Copy(conn, conn) }()
				}
			}()
			udpEcho, err := net.ListenPacket("udp6", echo.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer udpEcho.Close()
			go func() {
				buf := make([]byte, 2048)
				for {
					n, peer, err := udpEcho.ReadFrom(buf)
					if err != nil {
						return
					}
					udpEcho.WriteTo(buf[:n], peer)
				}
			}()
			reservation, err := net.Listen("tcp", net.JoinHostPort(scenario.listen, "0"))
			if err != nil {
				t.Fatal(err)
			}
			port := reservation.Addr().(*net.TCPAddr).Port
			reservation.Close()
			root, err := os.MkdirTemp("/tmp", "zb-direct-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			defer receiver.Close()
			source := ",\"source_ip\":\"::1\""
			if !scenario.success {
				source = ""
			}
			iface := ""
			if scenario.iface != "" {
				iface = ",\"interface\":\"" + scenario.iface + "\""
			}
			path := fmt.Sprintf(`enc:{"outbounds":[{"tag":"direct-exit","protocol":{"type":"direct"},"dial":{"address_family":"%s"%s%s}}],"target":"direct-exit"}`, scenario.family, source, iface)
			data, _, err := (RuntimeConfigurationRenderer{Cipher: runtimeConfigurationCipher{}}).Render(RuntimeConfigurationRenderRequest{NodeID: 1, APIKey: "local-test", Now: time.Now(), Snapshot: network.RuntimeConfigurationSnapshot{SiteURL: receiver.URL, NetworkEntries: []network.RuntimeConfigurationNetworkEntry{{ID: 9, Port: port, ListenAddress: scenario.listen, Network: "tcp_udp", PathConfig: path, Landing: network.RuntimeConfigurationLanding{EndpointExists: true, NodeExists: true, Active: true, NodeEnabled: true, Protocol: "vless", Address: "::1", PublicPort: echo.Addr().(*net.TCPAddr).Port}}}}})
			if err != nil {
				t.Fatal(err)
			}
			// Isolate the renderer's documented node-owned outbox from real state.
			data = bytes.ReplaceAll(data, []byte("/var/lib/zerodenet/event-outbox.jsonl"), []byte(filepath.Join(root, "outbox.jsonl")))
			config := filepath.Join(root, "config.json")
			socket := filepath.Join(root, "control.sock")
			if err := os.WriteFile(config, data, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(binary, "validate", config).CombinedOutput(); err != nil {
				t.Fatalf("native rejected panel configuration: %s %v", output, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			log, err := os.Create(filepath.Join(root, "kernel.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			kernel := exec.CommandContext(ctx, binary, "run", "--control-socket", socket, config)
			kernel.Stdout = log
			kernel.Stderr = log
			if err := kernel.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); kernel.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				connection, err := net.DialTimeout("tcp", net.JoinHostPort(scenario.listen, fmt.Sprint(port)), 100*time.Millisecond)
				if err == nil {
					connection.Close()
					ready = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !ready {
				output, _ := os.ReadFile(filepath.Join(root, "kernel.log"))
				if !scenario.success && scenario.iface != "" && bytes.Contains(bytes.ToLower(output), []byte("interface")) {
					t.Log("missing interface rejected before listener startup")
					return
				}
				t.Fatalf("kernel listener did not start: %s", output)
			}
			for _, transport := range []string{"tcp", "udp"} {
				conn, err := net.Dial(transport, net.JoinHostPort(scenario.listen, fmt.Sprint(port)))
				if err != nil {
					t.Fatal(err)
				}
				conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
				payload := []byte("family-constrained")
				_, writeErr := conn.Write(payload)
				response := make([]byte, len(payload))
				_, readErr := io.ReadFull(conn, response)
				conn.Close()
				if scenario.success && (writeErr != nil || readErr != nil || !bytes.Equal(payload, response)) {
					t.Fatalf("%s constrained echo failed: %v %v", transport, writeErr, readErr)
				}
				if !scenario.success && writeErr == nil && readErr == nil {
					t.Fatalf("%s bypassed hard dial constraint", transport)
				}
			}
		})
	}
}
