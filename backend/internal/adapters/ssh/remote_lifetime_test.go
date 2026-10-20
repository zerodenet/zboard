package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// Execute the real command wrapper over TCP/SSH and a real Linux shell. All
// processes and lock files belong to this fixture; no system service is used.
func lifetimeSession(t *testing.T) (*RemoteSession, <-chan struct{}) {
	return lifetimeSessionWithPTY(t, false)
}

func lifetimeSessionWithPTY(t *testing.T, stallPTY bool) (*RemoteSession, <-chan struct{}) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("remote process groups use Linux util-linux/coreutils, verified in CI")
	}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := cryptossh.NewSignerFromKey(private)
	config := &cryptossh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		conn, channels, requests, err := cryptossh.NewServerConn(raw, config)
		if err != nil {
			return
		}
		defer conn.Close()
		go cryptossh.DiscardRequests(requests)
		incoming, ok := <-channels
		if !ok {
			return
		}
		channel, requests, err := incoming.Accept()
		if err != nil {
			return
		}
		defer channel.Close()
		for request := range requests {
			if request.Type == "pty-req" && stallPTY {
				continue // Wait for cancellation to close the client transport.
			}
			if request.Type != "exec" {
				_ = request.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			if cryptossh.Unmarshal(request.Payload, &payload) != nil {
				return
			}
			_ = request.Reply(true, nil)
			command := exec.Command("sh", "-c", payload.Command)
			command.Stdout, command.Stderr = channel, channel.Stderr()
			stdin, err := command.StdinPipe()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(stdin, channel); _ = stdin.Close() }()
			err = command.Run()
			status := uint32(0)
			if err != nil {
				status = 1
			}
			_, _ = channel.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{status}))
			return
		}
	}()
	client, err := cryptossh.Dial("tcp", listener.Addr().String(), &cryptossh.ClientConfig{User: "fixture", HostKeyCallback: cryptossh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteSession(client, func(command string, _ bool) (string, string, bool, error) { return command, "", false, nil })
	t.Cleanup(func() {
		_ = remote.Close()
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("remote fixture shell survived SSH close")
		}
	})
	return remote, done
}

func TestRemoteLifetimeCancelsStalledPTYBeforeCommandStarts(t *testing.T) {
	remote, done := lifetimeSessionWithPTY(t, true)
	remote.prepare = func(command string, _ bool) (string, string, bool, error) {
		return command, "", true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := remote.RunWithLifetime(ctx, "printf must-not-run", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PTY cancellation=%v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PTY setup ignored cancellation")
	}
}

func TestRemoteLifetimeNormalExitStopsWatchdog(t *testing.T) {
	remote, done := lifetimeSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := remote.RunWithLifetime(ctx, "printf ready", true)
	if err != nil || output != "ready" {
		t.Fatalf("output=%q error=%v", output, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("successful command left watchdogs attached")
	}
}

func TestRemoteLifetimeStopsOnlyOwnedChildrenAndReleasesLock(t *testing.T) {
	for _, cause := range []string{"cancellation", "transport loss", "node deadline"} {
		t.Run(cause, func(t *testing.T) {
			remote, serverDone := lifetimeSession(t)
			root := t.TempDir()
			pidPath, lockPath := filepath.Join(root, "child.pid"), filepath.Join(root, "publication.lock")
			unrelated := exec.Command("sleep", "60")
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
			// A nested CLI deliberately ignores TERM and inherits the publication
			// lock. Cleanup must escalate within this group, leaving peers alive.
			command := "exec 9>" + shellQuote(lockPath) + "; flock -x 9; sh -c " + shellQuote("trap '' TERM; echo $$ > "+shellQuote(pidPath)+"; exec sleep 60") + " & wait"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if cause == "node deadline" {
					_, err = remote.runWithLifetime(ctx, command, false, 600*time.Millisecond)
				} else {
					_, err = remote.RunWithLifetime(ctx, command, false)
				}
				done <- err
			}()
			var pid int
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if data, err := os.ReadFile(pidPath); err == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					if pid != 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("nested command did not start")
			}
			// The command exit, SSH EOF and SIGKILL delivery to grandchildren
			// are independent observations. Keep one existing nine-second cleanup
			// budget for all of them; an EOF alone does not prove child cleanup.
			cleanupDeadline := time.Now().Add(9 * time.Second)
			if cause == "cancellation" {
				cancel()
			} else if cause == "transport loss" {
				_ = remote.Close()
			}
			select {
			case err := <-done:
				if err == nil || (cause == "cancellation" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("termination error=%v", err)
				}
			case <-time.After(time.Until(cleanupDeadline)):
				t.Fatal("command did not terminate within cleanup budget")
			}
			select {
			case <-serverDone:
			case <-time.After(time.Until(cleanupDeadline)):
				t.Fatal("remote command group survived transport/deadline")
			}
			for {
				state, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
				if errors.Is(err, os.ErrNotExist) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				fields := strings.Fields(string(state)[strings.LastIndex(string(state), ")")+1:])
				if len(fields) > 0 && fields[0] == "Z" {
					break
				}
				if len(fields) == 0 {
					t.Fatalf("unrecognized child state: %s", state)
				}
				if !time.Now().Before(cleanupDeadline) {
					t.Fatalf("nested CLI still alive: %s", fields[0])
				}
				time.Sleep(10 * time.Millisecond)
			}
			lock, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatalf("publication lock remained held: %v", err)
			}
			if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("cleanup terminated an unrelated runtime: %v", err)
			}
		})
	}
}
