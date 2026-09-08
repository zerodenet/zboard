package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

// Exercise the real core: A must forward opaque bytes for both transports,
// including a two-hop SOCKS5 to Shadowsocks proxy chain, without receiving B's credentials.
func TestNetworkEntryRealTCPUDPForwarding(t *testing.T) {
	binary := os.Getenv("ZBOARD_ZERO_VALIDATE_BIN")
	if binary == "" {
		t.Skip("set ZBOARD_ZERO_VALIDATE_BIN for real Zero forwarding")
	}
	landing, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Close()
	targetPort := landing.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", targetPort))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		for {
			conn, err := landing.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, peer, err := udp.ReadFrom(buffer)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(buffer[:n], peer)
		}
	}()
	freePort := func() int {
		socket, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := socket.Addr().(*net.TCPAddr).Port
		socket.Close()
		return port
	}
	start := func(t *testing.T, config map[string]interface{}, port int) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "runtime.json")
		raw, _ := json.Marshal(config)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		socketDir, err := os.MkdirTemp("/tmp", "zboard-ipc-")
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
		cmd := exec.CommandContext(ctx, binary, "run", path, "--control-socket", filepath.Join(socketDir, "control.sock"))
		logFile, err := os.Create(path + ".log")
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.Env = append(os.Environ(), "RUST_LOG=debug")
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cancel()
			_ = cmd.Wait()
			logFile.Close()
			if t.Failed() {
				raw, _ := os.ReadFile(path + ".log")
				t.Logf("Zero: %s", raw)
			}
		})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("Zero listener did not become ready")
	}
	for _, mode := range []string{"direct", "chain", "shared_pool"} {
		t.Run(mode, func(t *testing.T) {
			f, a, b := networkEntryFixture(t)
			entryPort := freePort()
			b.Address = "127.0.0.1"
			b.Port = targetPort
			b.PublicPort = targetPort
			if err := f.h.db.Save(&b).Error; err != nil {
				t.Fatal(err)
			}
			entry := model.NetworkEntry{Name: "opaque", NodeID: a.ID, EndpointID: b.ID, Address: "127.0.0.1", Port: entryPort, PublicPort: entryPort, Enabled: true}
			if mode != "direct" {
				first, second := freePort(), freePort()
				for _, port := range []int{first, second} {
					protocol := map[string]interface{}{"type": "socks5"}
					if port == second {
						protocol = map[string]interface{}{"type": "shadowsocks", "cipher": "aes-128-gcm", "password": "path-fixture"}
					}
					start(t, map[string]interface{}{"inbounds": []interface{}{map[string]interface{}{"tag": "hop", "listen": map[string]interface{}{"address": "127.0.0.1", "port": port}, "protocol": protocol}}, "route": map[string]interface{}{"final": map[string]interface{}{"type": "direct"}}}, port)
				}
				raw := fmt.Sprintf(`{"outbounds":[{"tag":"one","protocol":{"type":"socks5","server":"127.0.0.1","port":%d}},{"tag":"two","protocol":{"type":"shadowsocks","cipher":"aes-128-gcm","password":"path-fixture","server":"127.0.0.1","port":%d}}],"outbound_groups":[{"tag":"chain","type":"relay","proxies":["one","two"]}],"target":"chain"}`, first, second)
				if mode == "shared_pool" {
					var path networkEntryPath
					json.Unmarshal([]byte(raw), &path)
					path.Groups = append(path.Groups, map[string]interface{}{"tag": "shared", "type": "url_test", "outbounds": []string{"chain", "two"}})
					path.Target = "shared"
					payload, _ := json.Marshal(path)
					encrypted, err := f.h.credentialCipher.Encrypt(string(payload))
					if err != nil {
						t.Fatal(err)
					}
					pool := model.NodeProxyPool{NodeID: a.ID, Name: "shared", Config: encrypted}
					if err := f.h.db.Create(&pool).Error; err != nil {
						t.Fatal(err)
					}
					entry.ProxyPoolID = &pool.ID
				}
				entry.PathConfig, err = f.h.credentialCipher.Encrypt(raw)
				if err != nil {
					t.Fatal(err)
				}
			}
			if entry.ProxyPoolID != nil {
				entry.PathConfig = ""
			}
			if err := f.h.db.Create(&entry).Error; err != nil {
				t.Fatal(err)
			}
			testPorts := []int{entryPort}
			if mode == "shared_pool" {
				for i := 0; i < 2; i++ {
					extra := entry
					extra.ID = 0
					extra.Name = fmt.Sprintf("extra-%d", i)
					extra.Port = freePort()
					extra.PublicPort = extra.Port
					if i == 1 {
						extra.ProxyPoolID = nil
					}
					if err := f.h.db.Create(&extra).Error; err != nil {
						t.Fatal(err)
					}
					testPorts = append(testPorts, extra.Port)
				}
			}
			config := map[string]interface{}{"inbounds": []map[string]interface{}{}, "mode": map[string]interface{}{"type": "rule"}, "route": map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}}}
			if err := f.h.appendNetworkEntryRuntime(config, a.ID); err != nil {
				t.Fatal(err)
			}
			start(t, config, entryPort)
			for _, entryPort := range testPorts {
				// Deliberately not an HTTP request or an authenticated protocol handshake.
				message := []byte{0, 255, 17, 23, 42, 0, 91, 88}
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", entryPort), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := conn.Write(message); err != nil {
					t.Fatal(err)
				}
				reply := make([]byte, len(message))
				if _, err := io.ReadFull(conn, reply); err != nil || !bytes.Equal(reply, message) {
					t.Fatalf("opaque TCP relay: %v %v", reply, err)
				}
				datagram, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", entryPort))
				if err != nil {
					t.Fatal(err)
				}
				defer datagram.Close()
				_ = datagram.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := datagram.Write(message); err != nil {
					t.Fatal(err)
				}
				reply = make([]byte, 2048)
				n, err := datagram.Read(reply)
				if err != nil || !bytes.Equal(reply[:n], message) {
					t.Fatalf("opaque UDP relay: %v %v", reply[:n], err)
				}
			}
		})
	}
	for _, mode := range []string{"direct", "shared_pool"} {
		t.Run("B_authentication_"+mode, func(t *testing.T) {
			f, a, b := networkEntryFixture(t)
			bPort, aPort := freePort(), freePort()
			const validID = "11111111-2222-3333-4444-555555555555"
			start(t, map[string]interface{}{
				"inbounds": []interface{}{map[string]interface{}{"tag": "B", "listen": map[string]interface{}{"address": "127.0.0.1", "port": bPort}, "protocol": map[string]interface{}{"type": "vless", "users": []interface{}{map[string]interface{}{"id": validID}}}}},
				"route":    map[string]interface{}{"final": map[string]interface{}{"type": "direct"}},
			}, bPort)
			b.Address, b.Port, b.PublicPort, b.Protocol = "127.0.0.1", bPort, bPort, "vless"
			if err := f.h.db.Save(&b).Error; err != nil {
				t.Fatal(err)
			}
			entry := model.NetworkEntry{Name: "B authentication", NodeID: a.ID, EndpointID: b.ID, Address: "127.0.0.1", Port: aPort, PublicPort: aPort, Network: "tcp", Enabled: true}
			if mode == "shared_pool" {
				hopPort := freePort()
				start(t, map[string]interface{}{"inbounds": []interface{}{map[string]interface{}{"tag": "hop", "listen": map[string]interface{}{"address": "127.0.0.1", "port": hopPort}, "protocol": map[string]interface{}{"type": "socks5"}}}, "route": map[string]interface{}{"final": map[string]interface{}{"type": "direct"}}}, hopPort)
				raw := fmt.Sprintf(`{"outbounds":[{"tag":"hop","protocol":{"type":"socks5","server":"127.0.0.1","port":%d}}],"outbound_groups":[{"tag":"pool","type":"url_test","outbounds":["hop"]}],"target":"pool"}`, hopPort)
				encrypted, err := f.h.credentialCipher.Encrypt(raw)
				if err != nil {
					t.Fatal(err)
				}
				pool := model.NodeProxyPool{NodeID: a.ID, Name: "optional pool", Config: encrypted}
				if err := f.h.db.Create(&pool).Error; err != nil {
					t.Fatal(err)
				}
				entry.ProxyPoolID = &pool.ID
			}
			if err := f.h.db.Create(&entry).Error; err != nil {
				t.Fatal(err)
			}
			aConfig := map[string]interface{}{"inbounds": []map[string]interface{}{}, "mode": map[string]interface{}{"type": "rule"}, "route": map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}}}
			if err := f.h.appendNetworkEntryRuntime(aConfig, a.ID); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(aConfig)
			if bytes.Contains(raw, []byte(validID)) || bytes.Contains(raw, []byte("vless")) {
				t.Fatal("A must not receive B authentication or a VLESS listener")
			}
			start(t, aConfig, aPort)
			for _, id := range []string{validID, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"} {
				clientPort := freePort()
				start(t, map[string]interface{}{
					"inbounds":  []interface{}{map[string]interface{}{"tag": "client", "listen": map[string]interface{}{"address": "127.0.0.1", "port": clientPort}, "protocol": map[string]interface{}{"type": "direct", "target": "127.0.0.1", "port": targetPort}}},
					"outbounds": []interface{}{map[string]interface{}{"tag": "B-via-A", "protocol": map[string]interface{}{"type": "vless", "server": "127.0.0.1", "port": aPort, "id": id}}},
					"route":     map[string]interface{}{"final": map[string]interface{}{"type": "route", "outbound": "B-via-A"}},
				}, clientPort)
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", clientPort), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				message := []byte("authenticated at B through A")
				_, _ = conn.Write(message)
				reply := make([]byte, len(message))
				_, err = io.ReadFull(conn, reply)
				if id == validID && (err != nil || !bytes.Equal(reply, message)) {
					t.Fatalf("B rejected valid client through %s: %v", mode, err)
				}
				if id != validID && err == nil {
					t.Fatal("B accepted an invalid client identity")
				}
			}
		})
	}

}
