package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRealityProbeDomainValidation(t *testing.T) {
	domains, err := normalizeRealityProbeDomains([]string{" WWW.Microsoft.COM. ", "www.microsoft.com", "test.example"})
	if err != nil || strings.Join(domains, ",") != "www.microsoft.com,test.example" {
		t.Fatalf("domains: %v %v", domains, err)
	}
	defaults, err := normalizeRealityProbeDomains(nil)
	if err != nil || len(defaults) < 3 {
		t.Fatalf("defaults: %v %v", defaults, err)
	}
	for _, value := range []string{"$(touch /tmp/injected).example", "valid.example;id", "'bad.example", "https://valid.example", "valid.example:443", "192.0.2.1", "localhost", "bad..example", "-bad.example", strings.Repeat("x", 64) + ".example"} {
		if _, err := normalizeRealityProbeDomains([]string{value}); err == nil {
			t.Fatalf("accepted invalid input %q", value)
		}
	}
	if _, err := normalizeRealityProbeDomains(make([]string, 13)); err == nil {
		t.Fatal("accepted unbounded candidate list")
	}
}

func TestRealityProbeRejectsIncompleteAndUnexpectedResults(t *testing.T) {
	domains := []string{"valid.example", "failed.example"}
	output := "SSH banner\nZBOARD_REALITY|failed.example|certificate|83\nZBOARD_REALITY|valid.example|available|62\n"
	items, err := parseRealityProbeResults(output, domains)
	if err != nil || len(items) != 2 || !items[0].Available || items[1].Available || items[1].Status != "certificate" {
		t.Fatalf("results: %+v %v", items, err)
	}
	for _, output := range []string{
		"ZBOARD_REALITY|valid.example|available|62\n",
		"ZBOARD_REALITY|unknown.example|available|62\n",
		"ZBOARD_REALITY|valid.example|invented|62\n",
		"ZBOARD_REALITY|valid.example|available|-1\n",
		"ZBOARD_REALITY|valid.example|available|x\n",
		"ZBOARD_REALITY|valid.example|available|62\nZBOARD_REALITY|valid.example|available|62\n",
	} {
		if _, err := parseRealityProbeResults(output, domains); err == nil {
			t.Fatalf("accepted invalid output %q", output)
		}
	}
}

func TestRealityProbeCommandClassifiesActualToolOutput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the VPS tools while executing the production command unchanged.
	write("timeout", `test "$1 $2 $3" = '-k 1s 5s' || exit 2
shift 3
exec "$@"`)
	write("date", "printf '1000000000\\n'\n")
	write("openssl", `test "$*" != '' || exit 2
case "$*" in
  *'-tls1_3 -groups X25519 -alpn h2 -verify_hostname '*'-verify_return_error') ;;
  *) exit 2;;
esac
case "$*" in
 *valid.example*) printf 'New, TLSv1.3, Cipher is TLS_AES_128_GCM_SHA256\nALPN protocol: h2\nVerify return code: 0 (ok)\n'; exit 1;;
 *no-h2.example*) printf 'CONNECTED\nNew, TLSv1.3, Cipher is TLS_AES_128_GCM_SHA256\nNo ALPN negotiated\nVerify return code: 0 (ok)\n';;
 *bad-cert.example*) printf 'certificate verify failed\nVerify return code: 62 (hostname mismatch)\n'; exit 1;;
 *timeout.example*) exit 124;;
 *) printf 'getaddrinfo: Name or service not known\n'; exit 1;;
esac`)
	domains := []string{"valid.example", "no-h2.example", "bad-cert.example", "timeout.example", "dns.example"}
	command := exec.Command("sh", "-c", buildRealityProbeCommand(domains))
	command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("probe shell: %v %s", err, output)
	}
	results, err := parseRealityProbeResults(string(output), domains)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"available", "unsupported", "certificate", "timeout", "unreachable"} {
		if results[i].Status != want {
			t.Fatalf("%s = %+v, want %s", domains[i], results[i], want)
		}
	}
}

func TestRealityProbeHTTPRequiresAdminAndVerifiedNode(t *testing.T) {
	h, userToken := newAnnouncementTestHandlers(t)
	path := "/api/v1/admin/protocol-endpoints/reality-probe"
	for _, test := range []struct {
		token string
		want  int
	}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
		response := httptest.NewRecorder()
		h.ProtocolRealityProbeHandler(response, announcementRequest(http.MethodPost, path, test.token, `{"node_id":1}`))
		if response.Code != test.want {
			t.Fatalf("authorization = %d, want %d", response.Code, test.want)
		}
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", 1).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := h.issueToken(authClaims{UserID: 1, Email: "reader@example.test", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "unverified VPS", Address: "192.0.2.1"}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"node_id":0}`, `{"node_id":1,"domains":["$(id).example"]}`, `{"node_id":1}`} {
		response := httptest.NewRecorder()
		h.ProtocolRealityProbeHandler(response, announcementRequest(http.MethodPost, path, adminToken, body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unverified/invalid input = %d %s", response.Code, response.Body.String())
		}
	}
}
