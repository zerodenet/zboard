package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

const certificateCSRMarker = "ZBOARD_CERT_CSR_BASE64="

type certificateRemote interface {
	Run(command string, privileged bool) (string, error)
	RunWithInput(command string, privileged bool, input string) (string, error)
}

func providerHasCapability(capabilities []string, capability string) bool {
	return slices.Contains(capabilities, capability)
}

func (h *handlers) issueCertificateWithProvider(ctx context.Context, remote certificateRemote, certificate model.ManagedCertificate, operation model.CertificateOperation, providerKey, credential string, domains []string) (metadata issuedCertificateMetadata, failurePhase string, resultErr error) {
	failurePhase = "preparing"
	if h.pluginManager == nil {
		return metadata, failurePhase, errors.New("certificate provider runtime is unavailable")
	}
	stageID := uuid.NewString()
	stageDir := fmt.Sprintf("/etc/zboard/certificates/%d/.staging-%s", certificate.ID, stageID)
	defer func() {
		if resultErr != nil {
			_, _ = remote.Run("rm -rf -- "+shellQuote(stageDir), true)
		}
	}()

	output, err := remote.Run(buildCertificateCSRScript(certificate.ID, stageID, domains), true)
	if err != nil {
		return metadata, failurePhase, fmt.Errorf("generate certificate CSR: %w: %s", err, truncateCertificateError(output))
	}
	csrPEM, csr, err := parseCertificateCSR(output, domains)
	if err != nil {
		return metadata, failurePhase, err
	}
	failurePhase = "requesting"
	fullchainPEM, err := h.pluginManager.IssueCertificate(ctx, plugins.CertificateIssueRequest{
		ProviderKey: providerKey, Credential: credential, ContactEmail: certificate.ContactEmail,
		Environment: certificate.Environment, OperationID: strconv.FormatUint(uint64(operation.ID), 10),
		CSRPEM: csrPEM, Domains: append([]string{}, domains...), Renewal: operation.OperationType == certificateOperationRenew,
	})
	if err != nil {
		return metadata, failurePhase, fmt.Errorf("certificate provider request failed: %w", err)
	}
	failurePhase = "validating"
	metadata, err = validateProviderCertificate(fullchainPEM, csr, domains, time.Now().UTC())
	if err != nil {
		return metadata, failurePhase, err
	}
	failurePhase = "installing"
	encoded := base64.StdEncoding.EncodeToString(fullchainPEM) + "\n"
	output, err = remote.RunWithInput(buildCertificateInstallScript(certificate.ID, stageID), true, encoded)
	if err != nil {
		return metadata, failurePhase, fmt.Errorf("install provider certificate: %w: %s", err, truncateCertificateError(output))
	}
	return metadata, "", nil
}

func buildCertificateCSRScript(certificateID uint, stageID string, domains []string) string {
	baseDir := fmt.Sprintf("/etc/zboard/certificates/%d", certificateID)
	stageDir := baseDir + "/.staging-" + stageID
	var config strings.Builder
	config.WriteString("[req]\nprompt = no\ndistinguished_name = subject\nreq_extensions = extensions\n[subject]\nCN = ")
	config.WriteString(domains[0])
	config.WriteString("\n[extensions]\nsubjectAltName = @alt_names\n[alt_names]\n")
	for index, domain := range domains {
		fmt.Fprintf(&config, "DNS.%d = %s\n", index+1, domain)
	}
	return fmt.Sprintf(`set -eu
test "$(id -u)" = "0"
command -v openssl >/dev/null 2>&1
base_dir=%s
stage_dir=%s
rm -rf -- "$stage_dir"
install -d -m 0700 "$stage_dir" "$base_dir/generations"
printf '%%s' %s > "$stage_dir/openssl.cnf"
openssl req -new -newkey rsa:2048 -nodes -sha256 -keyout "$stage_dir/privkey.pem" -out "$stage_dir/request.pem" -config "$stage_dir/openssl.cnf"
chmod 0600 "$stage_dir/privkey.pem" "$stage_dir/request.pem"
printf '%s%%s\n' "$(base64 < "$stage_dir/request.pem" | tr -d '\r\n')"
`, shellQuote(baseDir), shellQuote(stageDir), shellQuote(config.String()), certificateCSRMarker)
}

func parseCertificateCSR(output string, domains []string) ([]byte, *x509.CertificateRequest, error) {
	index := strings.LastIndex(output, certificateCSRMarker)
	if index < 0 {
		return nil, nil, errors.New("remote CSR result is missing")
	}
	encoded := strings.TrimSpace(strings.SplitN(output[index+len(certificateCSRMarker):], "\n", 2)[0])
	csrPEM, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(csrPEM) == 0 || len(csrPEM) > 32<<10 {
		return nil, nil, errors.New("remote CSR result is invalid")
	}
	block, rest := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil, errors.New("remote CSR PEM is invalid")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, nil, errors.New("remote CSR signature is invalid")
	}
	if !sameDomainSet(csr.DNSNames, domains) {
		return nil, nil, errors.New("remote CSR domains do not match the requested domains")
	}
	return csrPEM, csr, nil
}

func validateProviderCertificate(fullchainPEM []byte, csr *x509.CertificateRequest, domains []string, now time.Time) (issuedCertificateMetadata, error) {
	if len(fullchainPEM) == 0 || len(fullchainPEM) > 96<<10 || csr == nil {
		return issuedCertificateMetadata{}, errors.New("certificate provider returned an invalid certificate chain")
	}
	rest := fullchainPEM
	var leaf *x509.Certificate
	for len(bytes.TrimSpace(rest)) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return issuedCertificateMetadata{}, errors.New("certificate provider returned malformed certificate PEM")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return issuedCertificateMetadata{}, fmt.Errorf("parse provider certificate: %w", err)
		}
		if leaf == nil {
			leaf = certificate
		}
		rest = remaining
	}
	if leaf == nil {
		return issuedCertificateMetadata{}, errors.New("certificate provider returned an empty certificate chain")
	}
	csrKey, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	if err != nil {
		return issuedCertificateMetadata{}, fmt.Errorf("encode CSR public key: %w", err)
	}
	certificateKey, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil || !bytes.Equal(csrKey, certificateKey) {
		return issuedCertificateMetadata{}, errors.New("provider certificate does not match the node private key")
	}
	for _, domain := range domains {
		if err := leaf.VerifyHostname(domain); err != nil {
			return issuedCertificateMetadata{}, fmt.Errorf("provider certificate does not cover %s: %w", domain, err)
		}
	}
	if leaf.NotBefore.After(now.Add(10 * time.Minute)) {
		return issuedCertificateMetadata{}, errors.New("provider certificate is not valid yet")
	}
	if leaf.NotAfter.Before(now.Add(24 * time.Hour)) {
		return issuedCertificateMetadata{}, errors.New("provider certificate expires too soon")
	}
	sum := sha256.Sum256(leaf.Raw)
	return issuedCertificateMetadata{
		SerialNumber: strings.ToUpper(leaf.SerialNumber.Text(16)), FingerprintSHA256: fmt.Sprintf("%x", sum[:]),
		NotBefore: leaf.NotBefore.UTC(), NotAfter: leaf.NotAfter.UTC(),
	}, nil
}

func sameDomainSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	a := append([]string{}, actual...)
	b := append([]string{}, expected...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func buildCertificateInstallScript(certificateID uint, stageID string) string {
	baseDir := fmt.Sprintf("/etc/zboard/certificates/%d", certificateID)
	stageDir := baseDir + "/.staging-" + stageID
	return fmt.Sprintf(`set -eu
test "$(id -u)" = "0"
command -v openssl >/dev/null 2>&1
base_dir=%s
stage_dir=%s
stage_id=%s
test -s "$stage_dir/privkey.pem"
IFS= read -r fullchain_base64
test -n "$fullchain_base64"
if ! printf '%%s' "$fullchain_base64" | base64 -d > "$stage_dir/fullchain.pem" 2>/dev/null; then
  printf '%%s' "$fullchain_base64" | base64 --decode > "$stage_dir/fullchain.pem"
fi
chmod 0644 "$stage_dir/fullchain.pem"
openssl x509 -in "$stage_dir/fullchain.pem" -pubkey -noout > "$stage_dir/cert.pub"
openssl pkey -in "$stage_dir/privkey.pem" -pubout > "$stage_dir/key.pub"
cmp "$stage_dir/cert.pub" "$stage_dir/key.pub"
rm -f "$stage_dir/cert.pub" "$stage_dir/key.pub" "$stage_dir/request.pem" "$stage_dir/openssl.cnf"
serial="$(openssl x509 -in "$stage_dir/fullchain.pem" -serial -noout | cut -d= -f2 | tr -cd 'A-Fa-f0-9')"
test -n "$serial"
generation="$base_dir/generations/$serial-$stage_id"
install -d -m 0700 "$generation"
install -m 0644 "$stage_dir/fullchain.pem" "$generation/fullchain.pem"
install -m 0600 "$stage_dir/privkey.pem" "$generation/privkey.pem"
rm -f "$base_dir/current.next"
ln -s "$generation" "$base_dir/current.next"
mv -Tf "$base_dir/current.next" "$base_dir/current"
rm -rf -- "$stage_dir"
`, shellQuote(baseDir), shellQuote(stageDir), shellQuote(stageID))
}
