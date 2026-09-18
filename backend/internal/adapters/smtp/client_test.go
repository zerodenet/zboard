package smtpadapter

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

func TestSMTPConnectionCancellationInterruptsHandshake(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					accepted <- conn
				}
			}()
			host, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- (Channel{Settings: platform.SMTPSettings{Host: host, Port: port, From: "sender@example.test", TLSMode: mode}}).Check(ctx)
			}()
			select {
			case conn := <-accepted:
				defer conn.Close()
			case <-time.After(3 * time.Second):
				t.Fatal("connection not established")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled handshake succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation left SMTP handshake blocked")
			}
		})
	}
}
