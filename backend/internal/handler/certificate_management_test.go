package handler

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestBuildCertbotCertificateScriptUsesDNS01WithoutStandalonePort(t *testing.T) {
	script := buildCertbotCertificateScript(model.ManagedCertificate{
		ID:            7,
		ContactEmail:  "admin@example.com",
		Environment:   certificateEnvironmentProduction,
		ChallengeType: certificateChallengeDNS01,
	}, []string{"edge.example.com"}, false, "stage-id")

	for _, expected := range []string{"--dns-cloudflare", "cloudflare.ini", "IFS= read -r cloudflare_token"} {
		if !strings.Contains(script, expected) {
			t.Fatalf("DNS-01 script does not contain %q", expected)
		}
	}
	for _, expected := range []string{
		"python3-certbot-dns-cloudflare", "plugins 2>/dev/null | grep -q 'dns-cloudflare'", "python3 -m venv",
		"python3 -m ensurepip", "python3-pip", "--target", "certbot-dns-cloudflare", `"$certbot_bin"`,
		"ZBOARD_CERT_ERROR=unable to install Certbot Cloudflare DNS plugin",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("DNS-01 fallback script does not contain %q", expected)
		}
	}
	for _, forbidden := range []string{"--standalone", "--http-01-port"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("DNS-01 script unexpectedly contains %q", forbidden)
		}
	}
}

func TestHTTP01PreflightDoesNotTreatMissingLocalIPv6RouteAsRemoteFailure(t *testing.T) {
	previousLookup, previousDial := http01LookupIPAddrs, http01DialTimeout
	http01LookupIPAddrs = func(context.Context, *net.Resolver, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("2001:db8::10")}}, nil
	}
	http01DialTimeout = func(string, string, time.Duration) (net.Conn, error) {
		return nil, errors.New("dial tcp [2001:db8::10]:80: connect: network is unreachable")
	}
	defer func() { http01LookupIPAddrs, http01DialTimeout = previousLookup, previousDial }()

	if err := preflightHTTP01Domains([]string{"edge.example.com"}); err != nil {
		t.Fatalf("preflightHTTP01Domains() error = %v", err)
	}
}

func TestHTTP01PreflightStillRejectsConcretePortFailure(t *testing.T) {
	previousLookup, previousDial := http01LookupIPAddrs, http01DialTimeout
	http01LookupIPAddrs = func(context.Context, *net.Resolver, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	http01DialTimeout = func(string, string, time.Duration) (net.Conn, error) {
		return nil, errors.New("connect: connection refused")
	}
	defer func() { http01LookupIPAddrs, http01DialTimeout = previousLookup, previousDial }()

	if err := preflightHTTP01Domains([]string{"edge.example.com"}); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("preflightHTTP01Domains() error = %v", err)
	}
}

func TestBuildCertbotCertificateScriptUsesWebrootWithoutBindingPort(t *testing.T) {
	script := buildCertbotCertificateScript(model.ManagedCertificate{
		ID:            8,
		ContactEmail:  "admin@example.com",
		Environment:   certificateEnvironmentProduction,
		ChallengeType: certificateChallengeHTTP01Webroot,
		WebrootPath:   "/var/www/acme",
	}, []string{"edge.example.com"}, false, "stage-id")

	for _, expected := range []string{
		"--webroot", "--webroot-path", "/var/www/acme", "zboard-http01-preflight-stage-id",
		".well-known/acme-challenge", "curl --fail", "wget -qO-", "edge.example.com",
		"ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("webroot script does not contain %q", expected)
		}
	}
	for _, forbidden := range []string{"--standalone", "--http-01-port"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("webroot script unexpectedly contains %q", forbidden)
		}
	}
}

func TestValidNodeWebrootUsesRemotePOSIXSemantics(t *testing.T) {
	if !validNodeWebroot("/var/www/acme") {
		t.Fatal("canonical remote POSIX webroot was rejected")
	}
	for _, invalid := range []string{"", "/", "var/www/acme", "/var/www/../acme", `C:\www\acme`} {
		if validNodeWebroot(invalid) {
			t.Fatalf("invalid remote webroot %q was accepted", invalid)
		}
	}
}

func TestBuildCertbotCertificateScriptsHaveValidShellSyntax(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell is not available")
	}
	certificates := []model.ManagedCertificate{
		{ID: 9, ContactEmail: "admin@example.com", Environment: certificateEnvironmentProduction, ChallengeType: certificateChallengeDNS01},
		{ID: 10, ContactEmail: "admin@example.com", Environment: certificateEnvironmentProduction, ChallengeType: certificateChallengeHTTP01Webroot, WebrootPath: "/var/www/acme"},
	}
	for _, certificate := range certificates {
		script := buildCertbotCertificateScript(certificate, []string{"edge.example.com"}, false, "stage-id")
		command := exec.Command(sh, "-n")
		command.Stdin = strings.NewReader(script)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("challenge %s generated invalid shell: %v: %s", certificate.ChallengeType, err, output)
		}
	}
}
