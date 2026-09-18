package plugins

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func TestDNSProviderCapabilityRequiresServerAndDeclarations(t *testing.T) {
	raw, keys := fixturePackage(t, func(manifest *Manifest, _ map[string][]byte) {
		manifest.Capabilities = append(manifest.Capabilities, DNSProviderCapability)
	})
	if _, err := ReadPackage(raw, keys); err == nil {
		t.Fatal("DNS provider without server and declarations accepted")
	}
}

func TestSharedDNSAndCertificateProviderRequiresStableName(t *testing.T) {
	raw, keys := fixturePackage(t, func(manifest *Manifest, files map[string][]byte) {
		manifest.Capabilities = append(manifest.Capabilities, DNSProviderCapability, CertificateProviderCapability)
		manifest.Contributions.DNSProviders = []DNSProviderDefinition{{Key: "edge-dns", Name: "Edge DNS"}}
		manifest.Contributions.CertificateProviders = []DNSProviderDefinition{{Key: "edge-dns", Name: "Different CA"}}
		manifest.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = []byte("fixture")
	})
	if _, err := ReadPackage(raw, keys); err == nil || !strings.Contains(err.Error(), "same name") {
		t.Fatalf("shared provider with inconsistent names error=%v", err)
	}
}

func TestRealDNSProviderExecutesAndFencesReconfiguredGeneration(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, "./testdata/dns").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	raw, keys := fixturePackage(t, func(manifest *Manifest, files map[string][]byte) {
		manifest.ID = "example.dns"
		manifest.Capabilities = []string{ConfigCapability, DNSProviderCapability, CertificateProviderCapability}
		manifest.Surfaces = nil
		manifest.Contributions.Pages = nil
		manifest.Contributions.DNSProviders = []DNSProviderDefinition{{Key: "edge-dns", Name: "Edge DNS"}}
		manifest.Contributions.CertificateProviders = []DNSProviderDefinition{{Key: "edge-dns", Name: "Edge DNS"}}
		manifest.Components.UI = nil
		manifest.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
		files["runtimes/host/plugin"] = payload
	})
	manager, _, _ := testManager(t, keys)
	ctx := context.Background()
	installation, err := importFixture(t, manager, raw)
	if err != nil {
		t.Fatal(err)
	}
	installation, err = manager.Action(ctx, installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SaveConfig(ctx, installation.ID, "admin", 0, []byte(`{"endpoint":"test"}`)); err != nil {
		t.Fatal(err)
	}
	providers, err := manager.DNSProviders(ctx)
	if err != nil || len(providers) != 1 || providers[0].Key != "edge-dns" || providers[0].PluginID != installation.ID {
		t.Fatalf("providers=%+v error=%v", providers, err)
	}
	if err := manager.VerifyDNSProviderCredential(ctx, "edge-dns", "invalid"); err == nil {
		t.Fatal("invalid credential accepted")
	}
	if err := manager.VerifyDNSProviderCredential(ctx, "edge-dns", "valid-provider-token"); err != nil {
		t.Fatal(err)
	}
	definitions, err := manager.ProviderDefinitions(ctx)
	if err != nil || len(definitions) != 1 || definitions[0].Key != "edge-dns" || len(definitions[0].Capabilities) != 2 {
		t.Fatalf("definitions=%+v error=%v", definitions, err)
	}
	if err := manager.VerifyCertificateProviderCredential(ctx, "edge-dns", "invalid"); err == nil {
		t.Fatal("invalid certificate credential accepted")
	}
	if err := manager.VerifyCertificateProviderCredential(ctx, "edge-dns", "valid-provider-token"); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "edge.example.test"}, DNSNames: []string{"edge.example.test"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	fullchain, err := manager.IssueCertificate(ctx, CertificateIssueRequest{ProviderKey: "edge-dns", Credential: "valid-provider-token", CSRPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), Domains: []string{"edge.example.test"}, ContactEmail: "admin@example.test", Environment: "staging", OperationID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(fullchain)
	issued, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr != nil || issued.VerifyHostname("edge.example.test") != nil {
		t.Fatalf("issued certificate error=%v certificate=%+v", parseErr, issued)
	}
	certificateErrors := make(chan error, 1)
	go func() {
		_, issueErr := manager.IssueCertificate(ctx, CertificateIssueRequest{ProviderKey: "edge-dns", Credential: "valid-provider-token", CSRPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), Domains: []string{"edge.example.test"}, ContactEmail: "admin@example.test", Environment: "staging", OperationID: "slow"})
		certificateErrors <- issueErr
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := manager.SaveConfig(ctx, installation.ID, "admin", 1, []byte(`{"endpoint":"certificate-change"}`)); err != nil {
		t.Fatal(err)
	}
	if err := <-certificateErrors; !errors.Is(err, ErrConflict) {
		t.Fatalf("stale certificate provider result error=%v", err)
	}
	record := network.ManagedDNSRecord{DomainName: "edge.example.test", RecordType: "A", RecordValue: "203.0.113.9", TTL: 300, Proxied: false}
	result, err := manager.ApplyDNSProvider(ctx, "edge-dns", "valid-provider-token", record, false)
	if err != nil || result.ZoneID != "zone-1" || result.RecordID != "record-1" || result.Name != record.DomainName || result.Value != record.RecordValue {
		t.Fatalf("result=%+v error=%v", result, err)
	}

	staleRecord := record
	staleRecord.DomainName = "slow.example.test"
	resultErrors := make(chan error, 1)
	go func() {
		_, applyErr := manager.ApplyDNSProvider(ctx, "edge-dns", "valid-provider-token", staleRecord, false)
		resultErrors <- applyErr
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := manager.SaveConfig(ctx, installation.ID, "admin", 2, []byte(`{"endpoint":"changed"}`)); err != nil {
		t.Fatal(err)
	}
	if err := <-resultErrors; !errors.Is(err, ErrConflict) {
		t.Fatalf("stale provider result error=%v", err)
	}
}
