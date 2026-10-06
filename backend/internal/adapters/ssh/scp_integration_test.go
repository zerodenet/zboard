package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	cryptossh "golang.org/x/crypto/ssh"
)

// Exercise a real SCP sink through an SSH session, including receiver exit
// status, rather than treating an SSH stdin upload as SCP coverage.
func TestRemoteSessionSCPTransfersToRealReceiver(t *testing.T) {
	scp, err := exec.LookPath("scp")
	if err != nil {
		t.Skip("SCP executable unavailable")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := cryptossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &cryptossh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	destination := filepath.Join(t.TempDir(), "zero'kernel")
	commandSeen := make(chan string, 1)
	serverDone := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer raw.Close()
		conn, channels, requests, err := cryptossh.NewServerConn(raw, config)
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		go cryptossh.DiscardRequests(requests)
		incoming, ok := <-channels
		if !ok {
			serverDone <- nil
			return
		}
		channel, requests, err := incoming.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer channel.Close()
		for request := range requests {
			if request.Type != "exec" {
				request.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			if err := cryptossh.Unmarshal(request.Payload, &payload); err != nil {
				serverDone <- err
				return
			}
			commandSeen <- payload.Command
			request.Reply(true, nil)
			process := exec.Command(scp, "-t", destination)
			process.Stdin = channel
			process.Stdout = channel
			process.Stderr = channel.Stderr()
			err := process.Run()
			status := uint32(0)
			if err != nil {
				status = 1
			}
			channel.SendRequest("exit-status", false, cryptossh.Marshal(struct{ Status uint32 }{status}))
			serverDone <- err
			return
		}
	}()
	client, err := cryptossh.Dial("tcp", listener.Addr().String(), &cryptossh.ClientConfig{User: "test", HostKeyCallback: cryptossh.FixedHostKey(signer.PublicKey()), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteSession(client, nil)
	defer remote.Close()
	payload := bytes.Repeat([]byte("offline kernel\x00"), 65536)
	if err := remote.UploadSCP(destination, "0700", payload); err != nil {
		t.Fatal(err)
	}
	if command := <-commandSeen; command != "umask 077; scp -t "+shellQuote(destination) {
		t.Fatalf("not an SCP receiver: %q", command)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("SCP payload mismatch: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("SCP mode incorrect: %v", err)
	}
}
