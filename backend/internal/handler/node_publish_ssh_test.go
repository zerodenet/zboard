package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"golang.org/x/crypto/ssh"
)

// Exercise real TCP/SSH stalls through the production publication executor.
// This server deliberately never executes a shell or contacts a deployed node.
func TestPublishCancellationInterruptsSSHAndRetainsRetry(t *testing.T) {
	for _, stage := range []string{"handshake", "session", "command", "deadline"} {
		t.Run(stage, func(t *testing.T) {
			f := newOrderFixture(t)
			endpoint := attachOrderPublishEndpoint(t, f)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(key)
			if err != nil {
				t.Fatal(err)
			}
			config := &ssh.ServerConfig{NoClientAuth: true}
			config.AddHostKey(signer)
			var executorDone <-chan struct{}
			stalled := make(chan struct{})
			serverDone := make(chan struct{})
			release := make(chan struct{})
			go func() {
				defer close(serverDone)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				if stage == "handshake" || stage == "deadline" {
					close(stalled)
					<-release
					return
				}
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				channel, ok := <-channels
				if !ok {
					return
				}
				if stage == "session" {
					close(stalled)
					<-release
					return
				}
				stream, reqs, err := channel.Accept()
				if err != nil {
					return
				}
				defer stream.Close()
				for request := range reqs {
					if request.Type == "exec" {
						_ = request.Reply(true, nil)
						close(stalled)
						<-release
						return
					}
					_ = request.Reply(false, nil)
				}
			}()
			t.Cleanup(func() {
				close(release)
				_ = listener.Close()
				<-serverDone
				if executorDone != nil {
					<-executorDone
				}
			})
			host, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			password, err := f.h.credentialCipher.Encrypt("test-password")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.h.db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Updates(map[string]interface{}{
				"ssh_host": host, "ssh_port": port, "ssh_user": "root", "ssh_pwd": password,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := enqueueNodeConfigPublish(f.h.db, endpoint.NodeID, endpoint.ID, 0); err != nil {
				t.Fatal(err)
			}
			item := mustClaimPublish(t, f.h, time.Now().UTC().Add(time.Second))
			ctx, cancel := context.WithCancel(context.Background())
			if stage == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
			}
			defer cancel()
			done := make(chan struct{})
			executorDone = done
			go func() {
				defer close(done)
				f.h.executeNodePublish(ctx, item, func(ctx context.Context) error { return f.h.publishQueuedNode(ctx, item) })
			}()
			select {
			case <-stalled:
			case <-done:
				t.Fatal("publication failed before reaching SSH stall")
			case <-time.After(5 * time.Second):
				t.Fatal("SSH stall was not reached")
			}
			if stage != "deadline" {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				// Unblock the deliberately broken peer before reporting a regression.
				_ = listener.Close()
				t.Error("publication ignored cancellation while SSH was stalled")
				return
			}
			var pending model.NodeConfigPublish
			if err := f.h.db.First(&pending, endpoint.NodeID).Error; err != nil || pending.Attempts != 1 || pending.LeaseToken != "" {
				t.Fatalf("canceled publication lost retry: %+v %v", pending, err)
			}
		})
	}
}
