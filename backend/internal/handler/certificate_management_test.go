package handler

import (
	"strings"
	"testing"

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
	for _, forbidden := range []string{"--standalone", "--http-01-port"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("DNS-01 script unexpectedly contains %q", forbidden)
		}
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

	for _, expected := range []string{"--webroot", "--webroot-path", "/var/www/acme"} {
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
