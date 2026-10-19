package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	var failureCodes [600]atomic.Int64
	var firstFailureAt atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed := &zeroNodeResponseStatus{ResponseWriter: w}
		f.h.ZeroEventHandler(observed, r)
		if observed.status >= 400 {
			failures.Add(1)
			if observed.status < len(failureCodes) {
				failureCodes[observed.status].Add(1)
			}
			firstFailureAt.CompareAndSwap(0, time.Now().UnixMilli())
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Close() })
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
		// Stop admitting new requests and drain accepted durable writes before
		// stopping the producer. Force-closing its active HTTP request cancels
		// the receive transaction and manufactures a teardown-only 500.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Error(err)
		}
		cancel()
		session, err := client.NewSession()
		if err != nil {
			t.Error(err)
		} else {
			if err := session.Run("systemctl stop zero.service zboard-acceptance-echo.service"); err != nil {
				t.Error(err)
			}
			session.Close()
		}
		server.Close()
		if n := failures.Load(); n != 0 {
			codes := map[int]int64{}
			for status := 400; status < len(failureCodes); status++ {
				if count := failureCodes[status].Load(); count > 0 {
					codes[status] = count
				}
			}
			t.Errorf("real Zero event receiver returned %d errors: statuses=%v first_at=%s", n, codes, time.UnixMilli(firstFailureAt.Load()).UTC())
		}
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

// Provision the isolated node through the actual installer before exercising
// everyday publication. A preloaded executable alone has no managed event-state
// owner; pretending it was observed would bypass the production safety boundary.
func initializeRealZeroNode(t *testing.T, f orderFixture, endpoint model.ProtocolEndpoint) {
	t.Helper()
	binary, err := os.ReadFile("/usr/local/bin/zero")
	if err != nil {
		t.Fatal(err)
	}
	verificationRaw, err := os.ReadFile("/evidence/zero-verification.json")
	if err != nil {
		t.Fatal(err)
	}
	var verification struct {
		BinarySHA string `json:"binary_sha256"`
	}
	if err := json.Unmarshal(verificationRaw, &verification); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	binarySHA := hex.EncodeToString(digest[:])
	if binarySHA != verification.BinarySHA {
		t.Fatal("isolated installer binary differs from verified public artifact")
	}
	node, err := f.h.loadNode(endpoint.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := f.h.nodeConnectorCredential(node)
	if err != nil {
		t.Fatal(err)
	}
	if credential.IsNew {
		if err := f.h.db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]any{"node_credential": credential.Encrypted, "node_credential_prefix": credential.Prefix, "node_credential_revoked_at": nil}).Error; err != nil {
			t.Fatal(err)
		}
		f.h.invalidateZeroEventCredential(node.ID)
	}
	if err := f.h.services.NodeCredentialReconciliation(f.h.credentialCipher, true).ReconcileNode(t.Context(), node.ID); err != nil {
		t.Fatal(err)
	}
	config, _, err := f.h.compileNodeRuntimeConfigContext(t.Context(), node, credential.Raw, "")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	operation := model.NodeOperation{NodeID: node.ID, OperationType: "initialize", Status: "running", Phase: "installing", RequestedBy: 1, DesiredSHA256: binarySHA, StartedAt: &started}
	if err := f.h.db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.installNodeKernel(t.Context(), node, operation.ID, binary, binarySHA, config, credential.Raw, ""); err != nil {
		t.Fatalf("real isolated initialization failed: %v", err)
	}
	if err := f.h.db.Model(&operation).Updates(map[string]any{"status": "succeeded", "phase": "completed", "finished_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
}
