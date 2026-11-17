package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the remote command against real local TLS servers without contacting
// any deployed node or public domain. Only DNS/port selection is redirected.
func TestRealityProbeRealTLSHandshake(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("OpenSSL is unavailable")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.com"}, DNSNames: []string{"example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, domain, alpn, want string
		version                  uint16
	}{
		{"compatible", "example.com", "h2", "available", tls.VersionTLS13},
		{"wrong hostname", "wrong.example", "h2", "certificate", tls.VersionTLS13},
		{"no HTTP2", "example.com", "http/1.1", "unsupported", tls.VersionTLS13},
		{"TLS12 only", "example.com", "h2", "unsupported", tls.VersionTLS12},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: test.version, MaxVersion: test.version, NextProtos: []string{test.alpn}, CurvePreferences: []tls.CurveID{tls.X25519}})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_ = conn.(*tls.Conn).Handshake()
			}()
			dir := t.TempDir()
			write := func(name, script string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write("openssl", "#!/bin/sh\ntest \"$1 $2\" = 's_client -connect' || exit 2\nshift 3\nexec "+shellQuote(openssl)+" s_client -connect "+shellQuote(listener.Addr().String())+" \"$@\"\n")
			write("timeout", "#!/bin/sh\nshift 3\nexec \"$@\"\n")
			write("date", "#!/bin/sh\nprintf '1000000000\\n'\n")
			caPath := filepath.Join(dir, "ca.pem")
			if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "sh", "-c", buildRealityProbeCommand([]string{test.domain}))
			command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "SSL_CERT_FILE="+caPath)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("probe command: %v %s", err, output)
			}
			results, err := parseRealityProbeResults(string(output), []string{test.domain})
			if err != nil || results[0].Status != test.want {
				t.Fatalf("TLS probe: %+v %v, output %s", results, err, output)
			}
			_ = listener.Close()
			select {
			case <-serverDone:
			case <-time.After(time.Second):
				t.Fatal("local TLS server did not close")
			}
		})
	}
}
