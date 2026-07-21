package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
