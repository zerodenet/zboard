package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"time"

	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
)

type server struct {
	pluginv1.UnimplementedPluginControlServer
}

func (*server) GetInfo(context.Context, *pluginv1.Empty) (*pluginv1.Info, error) {
	return &pluginv1.Info{Id: "example.dns", Version: "1.0.0", Protocol: 1, Capabilities: []string{"zboard.config.v1", "zboard.dns.provider.v1", "zboard.certificate.provider.v1"}}, nil
}
func (*server) Health(context.Context, *pluginv1.Empty) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) ValidateConfig(_ context.Context, request *pluginv1.ConfigRequest) (*pluginv1.ConfigResult, error) {
	return &pluginv1.ConfigResult{NormalizedJson: request.ConfigJson}, nil
}
func (*server) ApplyConfig(context.Context, *pluginv1.ConfigRequest) (*pluginv1.HealthResult, error) {
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) VerifyDNSCredential(_ context.Context, request *pluginv1.DNSCredentialRequest) (*pluginv1.HealthResult, error) {
	if request.ProviderKey != "edge-dns" || string(request.Credential) != "valid-provider-token" || request.Generation == 0 || request.ConfigRevision == 0 {
		return &pluginv1.HealthResult{Healthy: false, Message: "credential rejected"}, nil
	}
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) VerifyCertificateCredential(_ context.Context, request *pluginv1.CertificateCredentialRequest) (*pluginv1.HealthResult, error) {
	if request.ProviderKey != "edge-dns" || string(request.Credential) != "valid-provider-token" || request.Generation == 0 || request.ConfigRevision == 0 {
		return &pluginv1.HealthResult{Healthy: false, Message: "credential rejected"}, nil
	}
	return &pluginv1.HealthResult{Healthy: true}, nil
}
func (*server) IssueCertificate(ctx context.Context, request *pluginv1.CertificateIssueRequest) (*pluginv1.CertificateIssueResult, error) {
	if request.ProviderKey != "edge-dns" || string(request.Credential) != "valid-provider-token" || request.Generation == 0 || request.ConfigRevision == 0 || request.OperationId == "" {
		return nil, errors.New("invalid request")
	}
	if request.OperationId == "slow" {
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	block, rest := pem.Decode(request.CsrPem)
	if block == nil || len(rest) != 0 {
		return nil, errors.New("invalid CSR")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, errors.New("invalid CSR signature")
	}
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	issuer := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuer, issuer, &issuerKey.PublicKey, issuerKey)
	if err != nil {
		return nil, err
	}
	issuer, err = x509.ParseCertificate(issuerDER)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, err
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: csr.Subject, DNSNames: append([]string{}, csr.DNSNames...), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, issuer, csr.PublicKey, issuerKey)
	if err != nil {
		return nil, err
	}
	return &pluginv1.CertificateIssueResult{FullchainPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})}, nil
}
func (*server) ApplyDNSRecord(ctx context.Context, request *pluginv1.DNSApplyRequest) (*pluginv1.DNSApplyResult, error) {
	if request.ProviderKey != "edge-dns" || string(request.Credential) != "valid-provider-token" || request.Generation == 0 || request.ConfigRevision == 0 {
		return nil, errors.New("invalid request")
	}
	if request.Name == "slow.example.test" {
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &pluginv1.DNSApplyResult{
		ZoneId: "zone-1", RecordId: "record-1", RecordType: request.RecordType,
		Name: request.Name, Value: request.Value, Ttl: request.Ttl, Proxied: request.Proxied,
	}, nil
}

func main() { pluginv1.Serve(&server{}) }
