package handler

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func paymentPluginPackage(t *testing.T, id string, pub ed25519.PublicKey, key ed25519.PrivateKey) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plugin")
	command := exec.Command("go", "build", "-ldflags", "-X main.pluginID="+id, "-o", binary, "../plugins/testdata/payment")
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build payment plugin: %v %s", err, raw)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	runtimePath := "runtimes/host/plugin"
	m := plugins.Manifest{SchemaVersion: 1, ID: id, Name: id, Version: "1.0.0", Requires: plugins.Requirements{ZBoard: ">=0.0.1", Tested: []string{"0.0.1"}, Protocol: 1, Bridge: 1}, Capabilities: []string{plugins.CommerceOrdersReadCapability, plugins.CommercePaymentsWriteCapability, plugins.PageCapability, plugins.UISlotCapability, plugins.ConfigCapability, plugins.StorageCapability, plugins.HTTPRouteCapability}, Surfaces: []string{"account"}, Components: plugins.Components{UI: map[string]string{"account": "ui/payment.html"}, Server: &struct {
		Executables map[string]string `json:"executables"`
	}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: runtimePath}}}}
	m.Data = &plugins.DataManifest{Version: 1, MinCompatibleVersion: 1, Migrations: []plugins.DataMigration{{Version: 1}}}
	m.Contributions.Slots = []plugins.Slot{{ID: "checkout", Surface: "account", Slot: plugins.CheckoutPaymentSlot, Title: id, Entrypoint: "ui/payment.html"}, {ID: "order", Surface: "account", Slot: plugins.OrderPaymentSlot, Title: id, Entrypoint: "ui/payment.html"}}
	m.Contributions.HTTPRoutes = []plugins.HTTPRoute{{ID: "notification", Method: "POST", Path: "/.well-known/" + id + "/notification"}}
	files := map[string][]byte{runtimePath: payload, "ui/payment.html": []byte(`<!doctype html><script>const token=new URLSearchParams(location.hash.slice(1)).get('bridge_token');parent.postMessage({source:'zboard-plugin-ui',bridge_token:token,type:'checkout.options',options:[{id:'test',title:'模拟支付',prepare_action:'mock.prepare',reconcile_action:'mock.reconcile'}]},'*');parent.postMessage({source:'zboard-plugin-ui',bridge_token:token,type:'ui.resize',height:100},'*')</script>`)}
	m.Files = map[string]string{}
	for name, data := range files {
		digest := sha256.Sum256(data)
		m.Files[name] = hex.EncodeToString(digest[:])
	}
	manifest, _ := json.Marshal(m)
	files["manifest.json"] = manifest
	files["signature.json"], _ = json.Marshal(plugins.Signature{PublicKey: base64.StdEncoding.EncodeToString(pub), Algorithm: "ed25519", KeyID: "payment.tests", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest))})
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
