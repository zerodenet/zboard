package zero

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests execute only an explicitly supplied local kernel binary. They
// neither contact deployed nodes nor manage an existing system service.
func TestNativePublicationPreservesTrafficAcrossPublisherProcesses(t *testing.T) {
	binary := os.Getenv("ZERO_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set ZERO_NATIVE_TEST_BINARY to a verified v0.0.3 binary")
	}
	root, err := os.MkdirTemp("/tmp", "zb-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	for _, dir := range []string{"bin", "etc", "var"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"id":        "#!/bin/sh\necho 0\n",
		"flock":     "#!/bin/sh\nexit 0\n",
		"sha256sum": "#!/bin/sh\nexec shasum -a 256 \"$@\"\n",
		"systemctl": "#!/bin/sh\nif [ \"$1\" = is-active ]; then exit 0; fi\necho service-mutated >> " + shellQuote(filepath.Join(root, "mutations")) + "\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
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
	udpEcho, err := net.ListenPacket("udp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, peer, err := udpEcho.ReadFrom(buffer)
			if err != nil {
				return
			}
			udpEcho.WriteTo(buffer[:n], peer)
		}
	}()
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ingress := reservation.Addr().(*net.TCPAddr).Port
	reservation.Close()
	config := fmt.Sprintf(`{"inbounds":[{"tag":"echo","listen":{"address":"127.0.0.1","port":%d},"protocol":{"type":"direct","target":"127.0.0.1","port":%d}}],"route":{"final":{"type":"direct"}}}`, ingress, echo.Addr().(*net.TCPAddr).Port)
	current := filepath.Join(root, "etc", "current.json")
	candidate := filepath.Join(root, "desired.json")
	for _, path := range []string{current, candidate} {
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	socket := filepath.Join(root, "control.sock")
	log, err := os.Create(filepath.Join(root, "kernel.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kernel := exec.CommandContext(ctx, binary, "run", "--control-socket", socket, current)
	kernel.Stdout = log
	kernel.Stderr = log
	if err := kernel.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); kernel.Wait() })
	identity := func() (string, uint64, error) {
		b, e := exec.Command(binary, "status", "--json", "--socket", socket).Output()
		if e != nil {
			return "", 0, e
		}
		var v struct {
			Core     string `json:"core_instance_id"`
			Revision uint64 `json:"config_revision"`
		}
		var wrapped struct {
			Runtime json.RawMessage `json:"runtime"`
		}
		e = json.Unmarshal(b, &wrapped)
		if e != nil {
			return "", 0, e
		}
		e = json.Unmarshal(wrapped.Runtime, &v)
		return v.Core, v.Revision, e
	}
	var core string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		core, _, err = identity()
		if err == nil && core != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || core == "" {
		t.Fatalf("kernel did not start: %v", err)
	}
	tcp, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ingress))
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", ingress))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	probe := func(conn net.Conn) error {
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		payload := []byte("existing-flow")
		if _, e := conn.Write(payload); e != nil {
			return e
		}
		b := make([]byte, len(payload))
		_, e := io.ReadFull(conn, b)
		if e == nil && string(b) != string(payload) {
			return fmt.Errorf("echo differs")
		}
		return e
	}
	for _, conn := range []net.Conn{tcp, udp} {
		if err := probe(conn); err != nil {
			t.Fatal(err)
		}
	}
	self, _ := os.Executable()
	var lastRevision uint64
	for cycle := 0; cycle < 3; cycle++ {
		child := exec.Command(self, "-test.run=^TestNativePublisherChild$", "-test.v")
		child.Env = append(os.Environ(), "ZERO_NATIVE_CHILD_ROOT="+root, "ZERO_NATIVE_TEST_BINARY="+binary)
		if cycle == 0 {
			child.Env = append(child.Env, "ZERO_NATIVE_LOST_ACK=1")
		}
		output, childErr := child.CombinedOutput()
		if cycle == 0 {
			if childErr == nil || !strings.Contains(string(output), "test lost acknowledgement") {
				t.Fatalf("lost ack was not exercised: %v %s", childErr, output)
			}
		} else if childErr != nil {
			t.Fatalf("publisher child: %v %s", childErr, output)
		}
		for _, conn := range []net.Conn{tcp, udp} {
			if err := probe(conn); err != nil {
				t.Fatalf("cycle %d existing flow interrupted: %v", cycle, err)
			}
		}
		observed, revision, err := identity()
		if err != nil || observed != core {
			t.Fatalf("kernel instance changed: %s -> %s %v", core, observed, err)
		}
		if cycle > 0 && revision != lastRevision {
			t.Fatalf("duplicate publication advanced revision: %d -> %d", lastRevision, revision)
		}
		lastRevision = revision
	}
	// An external port conflict must roll back the candidate, preserving the
	// same process, committed revision and unrelated established traffic.
	var invalid map[string]any
	json.Unmarshal([]byte(config), &invalid)
	invalid["inbounds"] = append(invalid["inbounds"].([]any), map[string]any{"tag": "occupied", "listen": map[string]any{"address": "127.0.0.1", "port": echo.Addr().(*net.TCPAddr).Port}, "protocol": map[string]any{"type": "socks5"}})
	bad, _ := json.Marshal(invalid)
	if err := os.WriteFile(candidate, bad, 0600); err != nil {
		t.Fatal(err)
	}
	failed := exec.Command(self, "-test.run=^TestNativePublisherChild$", "-test.v")
	failed.Env = append(os.Environ(), "ZERO_NATIVE_CHILD_ROOT="+root, "ZERO_NATIVE_TEST_BINARY="+binary)
	if output, err := failed.CombinedOutput(); err == nil {
		t.Fatalf("occupied listener was accepted: %s", output)
	}
	for _, conn := range []net.Conn{tcp, udp} {
		if err := probe(conn); err != nil {
			t.Fatalf("failed candidate interrupted previous traffic: %v", err)
		}
	}
	observed, revision, err := identity()
	if err != nil || observed != core || revision != lastRevision {
		t.Fatalf("failed candidate committed: core=%s revision=%d err=%v", observed, revision, err)
	}
	if _, err := os.Stat(filepath.Join(root, "mutations")); err == nil {
		t.Fatal("publication invoked service control")
	}
	t.Logf("verified native PID=%d core=%s revision=%d, existing TCP/UDP survive three publisher processes", kernel.Process.Pid, core, lastRevision)
}

func TestNativePublisherChild(t *testing.T) {
	root := os.Getenv("ZERO_NATIVE_CHILD_ROOT")
	if root == "" {
		t.Skip("isolated child only")
	}
	data, err := os.ReadFile(filepath.Join(root, "desired.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	dialer := localNativeDialer{root: root, binary: os.Getenv("ZERO_NATIVE_TEST_BINARY")}
	result, err := (ConfigurationPublisher{Dialer: dialer}).Publish(context.Background(), ConfigurationPublishRequest{DeploymentID: 1, RuntimeConfig: data, ConfigSHA256: hex.EncodeToString(sum[:]), ConnectorKey: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result.Output)
}

type localNativeDialer struct{ root, binary string }

func (d localNativeDialer) Dial(context.Context) (KernelRemoteSession, error) { return d, nil }
func (d localNativeDialer) replace(value string) string {
	return strings.NewReplacer("/usr/local/bin/zero", d.binary, "/etc/zerodenet", filepath.Join(d.root, "etc"), "/var/lib/zerodenet", filepath.Join(d.root, "var"), controlSocket, filepath.Join(d.root, "control.sock"), "/tmp/zboard-zero-config-", filepath.Join(d.root, "stage-")).Replace(value)
}
func (d localNativeDialer) Run(script string, _ bool) (string, error) {
	c := exec.Command("bash", "-c", d.replace(script))
	c.Env = append(os.Environ(), "PATH="+filepath.Join(d.root, "bin")+":"+os.Getenv("PATH"))
	b, e := c.CombinedOutput()
	if e == nil && os.Getenv("ZERO_NATIVE_LOST_ACK") == "1" && strings.Contains(script, "ZBOARD_CONFIG_APPLIED") {
		return "", fmt.Errorf("test lost acknowledgement")
	}
	return string(b), e
}
func (d localNativeDialer) Upload(path, mode string, data []byte) error {
	return os.WriteFile(d.replace(path), data, 0600)
}
func (d localNativeDialer) Close() error { return nil }
