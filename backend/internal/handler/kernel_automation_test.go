package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseKernelProbe(t *testing.T) {
	probe, err := parseKernelProbe(strings.Join([]string{
		"ZBOARD_OS=debian 12",
		"ZBOARD_ARCH=x86_64",
		"ZBOARD_LIBC=glibc 2.36",
		"ZBOARD_SYSTEMD=1",
		"ZBOARD_INSTALLED=1",
		"ZBOARD_VERSION=0.0.14",
		"ZBOARD_BINARY_SHA=" + strings.Repeat("a", 64),
		"ZBOARD_CONFIG_SHA=" + strings.Repeat("b", 64),
		"ZBOARD_SERVICE=active",
		"ZBOARD_CONTROL=healthy",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Installed || !probe.Systemd || probe.Version != "0.0.14" || probe.ControlStatus != "healthy" {
		t.Fatalf("unexpected probe: %#v", probe)
	}
}

func TestSupportsOfficialZeroArtifact(t *testing.T) {
	if supportsOfficialZeroArtifact(kernelProbe{Libc: "glibc 2.17"}) {
		t.Fatal("glibc 2.17 must not be accepted for the current official artifact")
	}
	if !supportsOfficialZeroArtifact(kernelProbe{Libc: "glibc 2.36"}) {
		t.Fatal("glibc 2.36 should be accepted")
	}
}

func TestClassifyKernelAction(t *testing.T) {
	desiredBinary := strings.Repeat("a", 64)
	desiredConfig := strings.Repeat("b", 64)
	tests := []struct {
		name  string
		probe kernelProbe
		want  string
	}{
		{name: "install", probe: kernelProbe{}, want: "install"},
		{name: "upgrade", probe: kernelProbe{Installed: true, Version: "0.0.13"}, want: "upgrade"},
		{name: "repair binary", probe: kernelProbe{Installed: true, Version: "0.0.14", BinarySHA256: strings.Repeat("c", 64)}, want: "repair"},
		{name: "configure", probe: kernelProbe{Installed: true, Version: "0.0.14", BinarySHA256: desiredBinary, ConfigSHA256: strings.Repeat("c", 64)}, want: "configure"},
		{name: "repair health", probe: kernelProbe{Installed: true, Version: "0.0.14", BinarySHA256: desiredBinary, ConfigSHA256: desiredConfig, ServiceStatus: "failed"}, want: "repair"},
		{name: "none", probe: kernelProbe{Installed: true, Version: "0.0.14", BinarySHA256: desiredBinary, ConfigSHA256: desiredConfig, ServiceStatus: "active", ControlStatus: "healthy"}, want: "none"},
		{name: "manual review", probe: kernelProbe{Installed: true, Version: "0.0.15", BinarySHA256: desiredBinary, ConfigSHA256: desiredConfig}, want: "manual_review"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyKernelAction(test.probe, "0.0.14", desiredBinary, desiredConfig); got != test.want {
				t.Fatalf("classifyKernelAction() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompareZeroVersions(t *testing.T) {
	if compareZeroVersions("0.0.13", "0.0.14") != -1 || compareZeroVersions("v0.1.0", "0.0.14") != 1 || compareZeroVersions("0.0.14", "0.0.14") != 0 {
		t.Fatal("semantic version ordering is incorrect")
	}
}

func TestValidateZeroReleaseURL(t *testing.T) {
	allowed, _ := url.Parse("https://github.com/zerodenet/zero/releases/download/v0.0.14/zero-linux-x86_64.tar.gz")
	if err := validateZeroReleaseURL(allowed); err != nil {
		t.Fatal(err)
	}
	blocked, _ := url.Parse("https://example.com/zero.tar.gz")
	if err := validateZeroReleaseURL(blocked); err == nil {
		t.Fatal("unexpectedly accepted an untrusted release host")
	}
}

func TestZeroConnectorUsesCurrentLocalKernelContract(t *testing.T) {
	if !strings.Contains(zeroReleaseAPI, "/zerodenet/zero/") {
		t.Fatalf("zeroReleaseAPI = %q, want the currently published zerodenet/zero channel", zeroReleaseAPI)
	}
	config := zeroConnectorAPIConfig("http://panel.example.test/", 17, "opaque-secret", true)
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, expected := range []string{
		`"url":"http://panel.example.test/api/zero/events"`,
		`"authorization":"Bearer opaque-secret"`,
		`"source_id":"node-17"`,
		`"outbox_path":"/var/lib/zerodenet/event-outbox.jsonl"`,
		`"engine.started"`,
		`"stats.sampled"`,
		`"flow.completed"`,
		`"allow_insecure":true`,
	} {
		if !strings.Contains(payload, expected) {
			t.Fatalf("connector config %s does not contain %s", payload, expected)
		}
	}
	for _, removed := range []string{`"push"`, `"api_key_env"`, `"/heartbeat"`, `"/commands"`} {
		if strings.Contains(payload, removed) {
			t.Fatalf("connector config retained removed panel contract %s: %s", removed, payload)
		}
	}
}

func TestNativeKernelAccessConfigValidatesWithCurrentZero(t *testing.T) {
	validator := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"))
	if validator == "" {
		t.Skip("ZBOARD_ZERO_VALIDATE_BIN is not configured")
	}
	config := map[string]interface{}{
		"inbounds": []interface{}{
			map[string]interface{}{
				"tag": "managed-vless", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 18443},
				"protocol": map[string]interface{}{
					"type": "vless",
					"users": []interface{}{map[string]interface{}{
						"id": "11111111-2222-3333-4444-555555555555", "principal_key": "subscription:7:endpoint:3",
						"policy_revision": uint64(17), "up_bps": uint64(1_000_000), "down_bps": uint64(1_000_000),
						"device_limit": uint32(2),
					}},
				},
			},
			map[string]interface{}{
				"tag": "managed-shadowsocks", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 18444},
				"protocol": map[string]interface{}{
					"type": "shadowsocks", "cipher": "aes-256-gcm",
					"users": []interface{}{map[string]interface{}{
						"password": "managed-secret", "principal_key": "subscription:8:endpoint:4",
						"policy_revision": uint64(18), "device_limit": uint32(1),
					}},
				},
			},
			map[string]interface{}{
				"tag": "managed-trojan", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 18445},
				"protocol": map[string]interface{}{
					"type": "trojan",
					"users": []interface{}{map[string]interface{}{
						"password": "managed-trojan-secret", "principal_key": "subscription:9:endpoint:5",
						"policy_revision": uint64(19),
					}},
				},
			},
			map[string]interface{}{
				"tag": "managed-hysteria2", "listen": map[string]interface{}{"address": "127.0.0.1", "port": 18446},
				"protocol": map[string]interface{}{
					"type": "hysteria2",
					"users": []interface{}{map[string]interface{}{
						"password": "managed-hysteria-secret", "principal_key": "subscription:10:endpoint:6",
						"policy_revision": uint64(20),
					}},
				},
			},
		},
		"mode":  map[string]interface{}{"type": "rule"},
		"route": map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}},
		"api":   zeroConnectorAPIConfig("http://127.0.0.1:18080", 17, "opaque-secret", true),
	}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "zboard-native-kernel.json")
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(validator, "validate", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("current Zero rejected the Zboard native access contract: %v\n%s", err, output)
	}
}

func TestCurrentZeroDeliversNativeConnectorEvent(t *testing.T) {
	zeroBinary := strings.TrimSpace(os.Getenv("ZBOARD_ZERO_VALIDATE_BIN"))
	if zeroBinary == "" {
		t.Skip("ZBOARD_ZERO_VALIDATE_BIN is not configured")
	}
	received := make(chan zeroEventEnvelope, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer opaque-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var event zeroEventEnvelope
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}
		select {
		case received <- event:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	tempDir := t.TempDir()
	config := map[string]interface{}{
		"inbounds": []interface{}{},
		"mode":     map[string]interface{}{"type": "rule"},
		"route":    map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}},
		"api": map[string]interface{}{
			"event_sinks": []interface{}{map[string]interface{}{
				"tag": "zboard", "type": "webhook", "url": receiver.URL,
				"events": []string{"stats.sampled"}, "source_id": "node-17",
				"headers":        map[string]string{"authorization": "Bearer opaque-secret"},
				"allow_insecure": true,
			}},
			"outbox_path": filepath.Join(tempDir, "event-outbox.jsonl"),
		},
	}
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tempDir, "connector-runtime.json")
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	command := exec.CommandContext(ctx, zeroBinary, "run", configPath)
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		cancel()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Errorf("Zero did not stop after test cancellation")
		}
	}()

	select {
	case event := <-received:
		if event.SchemaID != "zero.event.v1" || event.EventType != "stats.sampled" || event.SourceID != "node-17" {
			t.Fatalf("unexpected connector event: %+v", event)
		}
	case err := <-exited:
		exited <- err
		t.Fatalf("Zero exited before delivering stats.sampled: %v\n%s", err, output.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("Zero did not deliver stats.sampled within 10s\n%s", output.String())
	}
}

func TestResolveManagedZeroRelease(t *testing.T) {
	dir := t.TempDir()
	name := "zero-v0.0.14-linux-x86_64-musl.tar.gz"
	payload := []byte("test archive")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".sha256"), []byte(checksum+"  "+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := resolveManagedZeroRelease(dir, zeroRelease{Version: "0.0.14", Tag: "v0.0.14"})
	if err != nil {
		t.Fatal(err)
	}
	if release.ArtifactURL != "managed://"+name || release.ArtifactSHA256 != checksum || release.ArtifactSize != int64(len(payload)) {
		t.Fatalf("unexpected managed release: %#v", release)
	}
}

func TestResolveLocalNativeZeroReleaseSupportsExplicitPrerelease(t *testing.T) {
	dir := t.TempDir()
	name := "zero-v0.0.15-rc.1-linux-x86_64-musl.tar.gz"
	archive := []byte("local-native-artifact")
	if err := os.WriteFile(filepath.Join(dir, name), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	checksum := hex.EncodeToString(digest[:])
	if err := os.WriteFile(filepath.Join(dir, name+".sha256"), []byte(checksum+"  "+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := resolveLocalNativeZeroRelease(dir, "0.0.15-rc.1")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "0.0.15-rc.1" || release.Tag != "v0.0.15-rc.1" ||
		release.ArtifactSHA256 != checksum || release.LocalPath != filepath.Join(dir, name) {
		t.Fatalf("unexpected local native release: %+v", release)
	}
	if _, err := resolveLocalNativeZeroRelease(dir, "../0.0.15"); err == nil {
		t.Fatal("unsafe local native version was accepted")
	}
}

func TestZeroRollbackUsesAtomicBinaryReplacement(t *testing.T) {
	for name, script := range map[string]string{
		"install failure trap": buildZeroInstallScript("/tmp/stage", strings.Repeat("a", 64), 41),
		"post activation":      buildZeroRollbackScript(41),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(script, `cp -a "$backup/zero" /usr/local/bin/zero`) {
				t.Fatal("rollback overwrites the running executable directly")
			}
			for _, expected := range []string{
				`install -m 0755 "$backup/zero" /usr/local/bin/zero.rollback`,
				`mv -f /usr/local/bin/zero.rollback /usr/local/bin/zero`,
			} {
				if !strings.Contains(script, expected) {
					t.Fatalf("rollback script does not contain %q", expected)
				}
			}
		})
	}
}

func TestManagedZeroReleaseRequiresMatchingChecksumFilename(t *testing.T) {
	dir := t.TempDir()
	name := "zero-v0.0.14-linux-x86_64-musl.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".sha256"), []byte(strings.Repeat("a", 64)+"  other.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveManagedZeroRelease(dir, zeroRelease{Version: "0.0.14", Tag: "v0.0.14"}); err == nil {
		t.Fatal("resolveManagedZeroRelease() error = nil, want checksum filename rejection")
	}
}

func TestZeroInstallScriptsPersistAndConsumeRollbackMetadata(t *testing.T) {
	install := buildZeroInstallScript("/tmp/stage", strings.Repeat("a", 64), 42)
	rollback := buildZeroRollbackScript(42)
	for _, fragment := range []string{"$backup/old_active", "$backup/old_enabled", "systemctl disable zero"} {
		if !strings.Contains(install, fragment) {
			t.Fatalf("install script is missing %q", fragment)
		}
		if !strings.Contains(rollback, fragment) {
			t.Fatalf("rollback script is missing %q", fragment)
		}
	}
}
