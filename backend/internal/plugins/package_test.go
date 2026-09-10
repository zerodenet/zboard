package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"testing"
	"time"
)

func fixturePackage(t testing.TB, edit func(*Manifest, map[string][]byte)) ([]byte, map[string]string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureSignedPackage(t, priv, pub, edit), map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
}
func TestPackageValidatesIdentitySlotEntrypointsAndSurfaces(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.Capabilities = append(m.Capabilities, IdentityCapability)
		executable := "runtimes/" + runtime.GOOS + "-" + runtime.GOARCH + "/oauth"
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: executable}}
		files[executable] = []byte("runtime")
		files["ui/login.html"] = []byte("<button>Login</button>")
		m.Contributions.Slots = []Slot{{ID: "login-methods", Surface: "public", Slot: "auth.login.methods", Title: "Login", Entrypoint: "ui/login.html"}}
	})
	if _, err := ReadPackage(raw, keys); err != nil {
		t.Fatal(err)
	}
	bad, keys := fixturePackage(t, func(m *Manifest, files map[string][]byte) {
		m.Capabilities = append(m.Capabilities, IdentityCapability)
		executable := "runtimes/" + runtime.GOOS + "-" + runtime.GOARCH + "/oauth"
		m.Components.Server = &struct {
			Executables map[string]string `json:"executables"`
		}{Executables: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: executable}}
		files[executable] = []byte("runtime")
		files["ui/login.html"] = []byte("<button>Login</button>")
		m.Contributions.Slots = []Slot{{ID: "login-methods", Surface: "admin", Slot: "auth.login.methods", Title: "Login", Entrypoint: "ui/login.html"}}
	})
	if _, err := ReadPackage(bad, keys); err == nil {
		t.Fatal("slot accepted on the wrong host surface")
	}
}
func fixtureSignedPackage(t testing.TB, priv ed25519.PrivateKey, pub ed25519.PublicKey, edit func(*Manifest, map[string][]byte)) []byte {
	t.Helper()
	m := Manifest{SchemaVersion: 1, ID: "example.welcome", Name: "Welcome", Version: "1.0.0", Requires: Requirements{ZBoard: ">=0.0.1 <0.1.0", Tested: []string{"0.0.1"}, Protocol: 1, Bridge: 1}, Capabilities: []string{"zboard.ui.page.v1", "zboard.config.v1"}, Surfaces: []string{"public", "account", "admin"}, Components: Components{UI: map[string]string{"public": "ui/index.html", "account": "ui/index.html", "admin": "ui/index.html"}}}
	m.Contributions.Pages = []Page{{ID: "home", Surface: "public", Title: "Public"}, {ID: "home", Surface: "account", Title: "Account"}, {ID: "settings", Surface: "admin", Purpose: "configuration", Title: "Settings"}}
	files := map[string][]byte{"ui/index.html": []byte("<h1>Hello plugin</h1>")}
	if edit != nil {
		edit(&m, files)
	}
	m.Files = map[string]string{}
	for p, b := range files {
		sum := sha256.Sum256(b)
		m.Files[p] = hex.EncodeToString(sum[:])
	}
	raw, _ := json.Marshal(m)
	sig, _ := json.Marshal(Signature{PublicKey: base64.StdEncoding.EncodeToString(pub), Algorithm: "ed25519", KeyID: "test.publisher", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw))})
	files["manifest.json"] = raw
	files["signature.json"] = sig
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for p, b := range files {
		w, err := z.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestPackageRejectsUntrustedTamperedAndCoreCapabilities(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	if _, err := ReadPackage(raw, keys); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackage(raw, nil); err == nil {
		t.Fatal("untrusted publisher accepted")
	}
	for _, edit := range []func(*Manifest, map[string][]byte){
		func(m *Manifest, _ map[string][]byte) { m.Capabilities = []string{"zboard.core.sql.v1"} },
		func(_ *Manifest, f map[string][]byte) { f["../core.db"] = []byte("overwrite") },
		func(m *Manifest, _ map[string][]byte) { m.Contributions.Pages[0].Purpose = "configuration" },
	} {
		raw, keys := fixturePackage(t, edit)
		if _, err := ReadPackage(raw, keys); err == nil {
			t.Fatal("unsafe package accepted")
		}
	}
	z, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	var changed bytes.Buffer
	w := zip.NewWriter(&changed)
	for _, f := range z.File {
		r, _ := f.Open()
		b := new(bytes.Buffer)
		b.ReadFrom(r)
		r.Close()
		entry, _ := w.Create(f.Name)
		if f.Name == "ui/index.html" {
			entry.Write([]byte("tampered"))
		} else {
			entry.Write(b.Bytes())
		}
	}
	w.Close()
	if _, err := ReadPackage(changed.Bytes(), keys); err == nil {
		t.Fatal("tampered resource accepted")
	}
}
func TestPackageCompatibilityAndFileBoundaries(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	p, err := ReadPackage(raw, keys)
	if err != nil {
		t.Fatal(err)
	}
	if c := p.Manifest.Compatibility("v0.0.1"); !c.Compatible || !c.Tested {
		t.Fatal(c)
	}
	if c := p.Manifest.Compatibility("v1.0.0"); !c.Compatible || c.Warning == "" {
		t.Fatal(c)
	}
	for _, path := range []string{"/etc/passwd", "a/../../b", "a\\b", "a/./b", "a:b", "a\x00b"} {
		if SafePath(path) {
			t.Fatal(path)
		}
	}
}
func TestMarketRejectsExpiredOrModifiedSignedCatalog(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	now := time.Now()
	payload, _ := json.Marshal(marketPayload{SchemaVersion: 1, ExpiresAt: now.Add(time.Hour), Entries: []MarketEntry{}})
	sig := Signature{PublicKey: base64.StdEncoding.EncodeToString(pub), Algorithm: "ed25519", KeyID: "test.publisher", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	raw, _ := json.Marshal(signedCatalog{Payload: payload, Signature: sig})
	if _, err := parseMarket(raw, keys, now); err != nil {
		t.Fatal(err)
	}
	if _, err := parseMarket(raw, keys, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired index accepted")
	}
	sig.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	raw, _ = json.Marshal(signedCatalog{Payload: payload, Signature: sig})
	if _, err := parseMarket(raw, keys, now); err == nil {
		t.Fatal("bad signature accepted")
	}
}
func TestMarketRejectsPrivateURLs(t *testing.T) {
	for _, s := range []string{"http://example.com/index.json", "https://127.0.0.1/a", "https://[::1]/a", "https://169.254.169.254/a", "https://10.0.0.1/a", "https://100.64.0.1/a", "https://u:p@example.com/a", "https://example.com:8443/a"} {
		if _, err := safeRemoteURL(s); err == nil {
			t.Fatal(s)
		}
	}
}
