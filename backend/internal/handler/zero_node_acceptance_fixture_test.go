package handler

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
	"golang.org/x/crypto/ssh"
)

func newRealZeroNodeFixture(t *testing.T) (orderFixture, model.ProtocolEndpoint) {
	t.Helper()
	f, endpoint, _ := newRealZeroNodeControlledFixture(t)
	return f, endpoint
}

func newRealZeroNodeControlledFixture(t *testing.T) (orderFixture, model.ProtocolEndpoint, func(string)) {
	t.Helper()
	sshAddress, _ := realZeroAddresses(t)
	key, err := os.ReadFile(os.Getenv("ZBOARD_TEST_ZERO_SSH_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := os.Getenv("ZBOARD_TEST_ZERO_HOST_KEY")
	if fingerprint == "" {
		t.Fatal("runner must pin the SSH host key")
	}
	client, err := ssh.Dial("tcp", sshAddress, &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 5 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != fingerprint {
				return fmt.Errorf("test node SSH host key changed")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	run := func(command string) {
		t.Helper()
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		output, err := session.CombinedOutput(command)
		if err != nil {
			t.Fatalf("test node setup failed: %v: %s", err, output)
		}
	}
	run("systemctl stop zero.service zboard-acceptance-echo.service >/dev/null 2>&1 || true; systemd-run --collect --unit=zboard-acceptance-echo /usr/bin/systemd-socket-activate --accept --inetd --listen=127.0.0.1:18080 /bin/cat")
	t.Cleanup(func() {
		session, err := client.NewSession()
		if err == nil {
			defer session.Close()
			_ = session.Run("systemctl stop zero.service zboard-acceptance-echo.service")
		}
	})
	f := newOrderFixture(t)
	if err := f.h.db.Model(&f.planRecord).Updates(map[string]any{"traffic_bytes": 100 << 20, "device_limit": 64}).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := attachOrderPublishEndpoint(t, f)
	secret, err := f.h.credentialCipher.Encrypt(string(key))
	if err != nil {
		t.Fatal(err)
	}
	host, portRaw, _ := net.SplitHostPort(sshAddress)
	port, _ := strconv.Atoi(portRaw)
	if err := f.h.db.Model(&model.Node{}).Where("id = ?", endpoint.NodeID).Updates(map[string]any{
		"ssh_host": host, "ssh_port": port, "ssh_user": "root", "ssh_auth_method": sshAuthPrivateKey, "ssh_pwd": secret,
		"ssh_host_key_fingerprint": fingerprint, "ssh_privilege_mode": "none",
	}).Error; err != nil {
		t.Fatal(err)
	}
	protocol, err := f.h.credentialCipher.Encrypt(`{"type":"vless","users":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Port = 8443
	if err := f.h.db.Model(&endpoint).Updates(map[string]any{"port": 8443, "server_config": protocol}).Error; err != nil {
		t.Fatal(err)
	}
	listener, err := client.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		t.Fatal(err)
	}
	var failures atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed := &zeroNodeResponseStatus{ResponseWriter: w}
		f.h.ZeroEventHandler(observed, r)
		if observed.status >= 400 {
			failures.Add(1)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		if n := failures.Load(); n != 0 {
			t.Errorf("real Zero event receiver returned %d errors", n)
		}
	})
	installation := model.Installation{ID: 1, SiteName: "Isolated acceptance", SiteURL: "http://127.0.0.1:18081", InstalledAt: time.Now().UTC()}
	if err := f.h.db.Create(&installation).Error; err != nil {
		t.Fatal(err)
	}
	cfg := zeroevent.DefaultConfig()
	cfg.Directory = t.TempDir()
	cfg.Storage.MinFreeSpace = 0
	cfg.Storage.EmergencyReserve = 0
	if err := f.h.ConfigureZeroEventSpool(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.h.CloseZeroEventSpool(); err != nil {
			t.Error(err)
		}
	})
	return f, endpoint, run
}

type zeroNodeResponseStatus struct {
	http.ResponseWriter
	status int
}

func (w *zeroNodeResponseStatus) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
