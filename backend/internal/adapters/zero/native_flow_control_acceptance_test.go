package zero

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in public-kernel test. All listeners/configuration are isolated under a
// temporary directory; no installed node, service or external endpoint is used.
func TestNativeFlowControlClosesOnlyRevokedPrincipal(t *testing.T) {
	binary := os.Getenv("ZERO_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set ZERO_NATIVE_TEST_BINARY to an independently verified public binary")
	}
	root, err := os.MkdirTemp("/tmp", "zb-flow-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
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
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ingress := reserve.Addr().(*net.TCPAddr).Port
	reserve.Close()
	revoked, control := uuid.New(), uuid.New()
	config := fmt.Sprintf(`{"inbounds":[{"tag":"scoped","listen":{"address":"127.0.0.1","port":%d},"protocol":{"type":"vless","users":[{"id":"%s","principal_key":"revoked"},{"id":"%s","principal_key":"control"}]}}],"route":{"final":{"type":"direct"}}}`, ingress, revoked, control)
	path, socket := filepath.Join(root, "current.json"), filepath.Join(root, "control.sock")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(root, "kernel.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "run", "--control-socket", socket, path)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); cmd.Wait() }()
	dial := func(id uuid.UUID) (net.Conn, error) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ingress), time.Second)
		if err != nil {
			return nil, err
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		port := echo.Addr().(*net.TCPAddr).Port
		request := append([]byte{0}, id[:]...)
		request = append(request, 0, 1, byte(port>>8), byte(port), 1, 127, 0, 0, 1, 'x')
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			return nil, err
		}
		var reply [3]byte
		if _, err := io.ReadFull(conn, reply[:]); err != nil {
			conn.Close()
			return nil, err
		}
		if reply != [3]byte{0, 0, 'x'} {
			conn.Close()
			return nil, fmt.Errorf("unexpected echo")
		}
		conn.SetDeadline(time.Time{})
		return conn, nil
	}
	var held net.Conn
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		held, err = dial(revoked)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("native VLESS did not start: %v", err)
	}
	defer held.Close()
	unaffected, err := dial(control)
	if err != nil {
		t.Fatal(err)
	}
	defer unaffected.Close()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	result, err := DrainRevokedFlows(ctx, conn, func(context.Context) error {
		actual, e := os.ReadFile(path)
		if e != nil || string(actual) != config {
			return ErrFlowControlEvidence
		}
		return nil
	}, func(_ context.Context, key string, close func() error) (bool, error) {
		if key != "revoked" {
			return false, nil
		}
		return true, close()
	})
	if err != nil || result.ConfirmedClosed != 1 || result.CloseCommands != 1 {
		t.Fatalf("scope drain=%+v err=%v", result, err)
	}
	held.SetDeadline(time.Now().Add(time.Second))
	held.Write([]byte("after"))
	var b [8]byte
	n, readErr := held.Read(b[:])
	if n != 0 || readErr == nil {
		t.Fatal("revoked held flow remained usable")
	}
	if e, ok := readErr.(net.Error); ok && e.Timeout() {
		t.Fatal("read timeout is not closure confirmation")
	}
	unaffected.SetDeadline(time.Now().Add(time.Second))
	if _, err := unaffected.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(unaffected, reply[:]); err != nil || string(reply[:]) != "ok" {
		t.Fatal("other subscription affected", err)
	}
	t.Logf("native flows.close: confirmed=%d; other principal still relays; held revoked connection EOF confirmed", result.ConfirmedClosed)
}
