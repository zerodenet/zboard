package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/plugins"
	pluginv1 "github.com/zerodenet/zboard/backend/pkg/pluginapi/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func newPublicRouteHTTPFixture(t *testing.T) (*handlers, *plugins.Manager, plugins.Installation) {
	t.Helper()
	h, _ := newAnnouncementTestHandlers(t)
	binary := filepath.Join(t.TempDir(), "route-plugin")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "./testdata/route")
	command.Dir = "../plugins"
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build route plugin: %v %s", err, output)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile("../../../examples/plugins/welcome/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest plugins.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ID = "example.route"
	manifest.Capabilities = []string{plugins.ConfigCapability, plugins.HTTPRouteCapability}
	manifest.Surfaces = nil
	manifest.Components.UI = nil
	manifest.Contributions.Pages = nil
	manifest.Components.Server = &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "runtimes/host/plugin"}}
	manifest.Contributions.HTTPRoutes = []plugins.HTTPRoute{
		{ID: "notification", Method: "POST", Path: "/.well-known/example/v1/notification"},
		{ID: "discovery", Method: "GET", Path: "/.well-known/example/v1/discovery"},
	}
	digest := sha256.Sum256(payload)
	manifest.Files = map[string]string{"runtimes/host/plugin": hex.EncodeToString(digest[:])}
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	signature, err := json.Marshal(plugins.Signature{Algorithm: "ed25519", KeyID: "test.route", PublicKey: base64.StdEncoding.EncodeToString(public), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifestBytes))})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, data := range map[string][]byte{"manifest.json": manifestBytes, "signature.json": signature, "runtimes/host/plugin": payload} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.NewManager(h.db, newTestCredentialCipher(t), plugins.Options{Directory: t.TempDir(), TrustedPublishers: map[string]string{"test.route": base64.StdEncoding.EncodeToString(public)}}, "v0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	h.SetPluginManager(manager)
	installation, err := manager.Import(archive.Bytes(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	installation, err = manager.Action(context.Background(), installation.ID, "enable", "admin", installation.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return h, manager, installation
}

func signRouteNotification(uri string, body []byte) string {
	mac := hmac.New(sha256.New, []byte("fixture-route-secret"))
	mac.Write([]byte(uri + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestPublicRouteHTTPDeliversCompleteRequestToSignedPlugin(t *testing.T) {
	h, manager, installation := newPublicRouteHTTPFixture(t)
	body := []byte("{ \"event\":\"notify\", \"text\":\"付款\" }\r\n")
	uri := "/.well-known/example/v1/%6Eotification?nonce=first&nonce=second&empty=&encoded=%2f+%20"
	request := httptest.NewRequest("POST", uri, bytes.NewReader(body))
	request.Host = "callback.example:8443"
	request.RemoteAddr = "192.0.2.2:1234"
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Authorization", "Bearer plugin-route-credential")
	request.Header.Set("X-Webhook-Signature", signRouteNotification(request.RequestURI, body))
	request.Header["X-Multi"] = []string{"first", "", "third"}
	request.Header["X-Opaque"] = []string{string([]byte{0xff, 0x80})}
	request.Header["Cookie"] = []string{"a=1", "b=2"}
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Trailer = http.Header{"X-Trailer": {"end-signature"}}
	want := pluginv1.NewHTTPRequest(request, body)
	want.RouteId = "notification"
	response := httptest.NewRecorder()
	h.PluginPublicRouteHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("callback = %d %s", response.Code, response.Body.String())
	}
	got := new(pluginv1.HTTPRequest)
	if err := protojson.Unmarshal(response.Body.Bytes(), got); err != nil || !proto.Equal(got, want) {
		t.Fatalf("process did not receive original request: %v\ngot %v\nwant %v", err, got, want)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response was not relayed: %v", response.Header())
	}
	for _, test := range []struct {
		name, uri, signature string
		status               int
		message              string
	}{
		{"invalid signature", uri, "invalid", 401, "invalid signature"},
		{"body tampering", uri, signRouteNotification(uri, []byte("different bytes")), 401, "invalid signature"},
		{"raw query tampering", uri + "&nonce=third", signRouteNotification(uri, body), 401, "invalid signature"},
		{"plugin query parsing", uri + "&invalid=%ZZ", signRouteNotification(uri+"&invalid=%ZZ", body), 400, "invalid query"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", test.uri, bytes.NewReader(body))
			r.Header.Set("X-Webhook-Signature", test.signature)
			w := httptest.NewRecorder()
			h.PluginPublicRouteHandler(w, r)
			if w.Code != test.status || strings.TrimSpace(w.Body.String()) != test.message {
				t.Fatalf("plugin decision lost: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, test := range []struct{ method, path string }{{"GET", "/.well-known/example/v1/notification?valid=1"}, {"POST", "/.well-known/example/v1/other?valid=1"}} {
		w := httptest.NewRecorder()
		h.PluginPublicRouteHandler(w, httptest.NewRequest(test.method, test.path, nil))
		if w.Code != 404 {
			t.Fatalf("unregistered method/path was dispatched: %d", w.Code)
		}
	}
	// A supplied route ID cannot override the host's signed route match.
	input := &pluginv1.HTTPRequest{RouteId: "notification", Method: "GET", Path: "/.well-known/example/v1/discovery"}
	if result, err := manager.HandlePublicRoute(context.Background(), input); err != nil || string(result.Body) != `{"available":true}` || input.RouteId != "notification" {
		t.Fatalf("route ID selection or snapshot failed: %+v %v", result, err)
	}
	if _, err := manager.Action(context.Background(), installation.ID, "disable", "admin", installation.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.PluginPublicRouteHandler(w, httptest.NewRequest("POST", uri, bytes.NewReader(body)))
	if w.Code != 404 {
		t.Fatalf("disabled route remained reachable: %d", w.Code)
	}
}

func TestPublicRouteHTTPBoundsRequestWithoutTruncatingAcceptedBodies(t *testing.T) {
	h, _, _ := newPublicRouteHTTPFixture(t)
	body := bytes.Repeat([]byte{'x'}, plugins.MaxPublicRouteBodyBytes)
	digest := sha256.Sum256(body)
	r := httptest.NewRequest("GET", "/.well-known/example/v1/discovery", bytes.NewReader(body))
	r.Header.Set("X-Test-Body-Sha256", hex.EncodeToString(digest[:]))
	w := httptest.NewRecorder()
	h.PluginPublicRouteHandler(w, r)
	if w.Code != 200 || w.Body.String() != "body verified" {
		t.Fatalf("accepted 8 MiB body failed process hop: %d %s", w.Code, w.Body.String())
	}
	for _, r := range []*http.Request{
		httptest.NewRequest("GET", "/.well-known/example/v1/discovery", bytes.NewReader(append(body, 'x'))),
		httptest.NewRequest("GET", "/.well-known/example/v1/discovery?value="+strings.Repeat("q", pluginv1.MaxHTTPMetadataBytes), nil),
	} {
		w := httptest.NewRecorder()
		h.PluginPublicRouteHandler(w, r)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("unbounded request accepted: %d %s", w.Code, w.Body.String())
		}
	}
}
