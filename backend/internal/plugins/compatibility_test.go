package plugins

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestProductVersionsDoNotGatePluginAdmission(t *testing.T) {
	for _, host := range []string{"v0.0.2-dev.202609091428", "v1.0.0", "dev", "v0.0.1-rc.1"} {
		t.Run(host, func(t *testing.T) {
			raw, keys := fixturePackage(t, nil)
			m, _, _ := testManager(t, keys)
			m.host = host
			installed, err := importFixture(t, m, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !installed.Compatibility.Compatible || installed.Compatibility.Tested {
				t.Fatal(installed.Compatibility)
			}
			installed, err = m.Action(context.Background(), installed.ID, "enable", "admin", installed.Generation, false, "")
			if err != nil || !installed.Enabled {
				t.Fatal(installed, err)
			}
		})
	}
}

func TestUntestedHostAllowsActivePluginUpgrade(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]string{"test.publisher": base64.StdEncoding.EncodeToString(pub)}
	m, _, _ := testManager(t, keys)
	m.host = "v0.0.2-dev.202609091428"
	old, err := importFixture(t, m, fixtureSignedPackage(t, priv, pub, nil))
	if err != nil {
		t.Fatal(err)
	}
	old, err = m.Action(context.Background(), old.ID, "enable", "admin", old.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := m.Import(fixtureSignedPackage(t, priv, pub, func(m *Manifest, _ map[string][]byte) { m.Version = "1.0.1" }), "admin")
	if err != nil || !upgraded.Enabled || upgraded.Version != "1.0.1" {
		t.Fatal(upgraded, err)
	}
}

func TestPluginAPIAndRuntimeRemainRequiredOnEveryHostVersion(t *testing.T) {
	for _, edit := range []func(*Manifest, map[string][]byte){
		func(m *Manifest, _ map[string][]byte) { m.Requires.Protocol = 2 },
		func(m *Manifest, _ map[string][]byte) { m.Requires.Bridge = 2 },
		func(m *Manifest, files map[string][]byte) {
			m.Components.Server = &struct {
				Executables map[string]string `json:"executables"`
			}{Executables: map[string]string{"unsupported-platform": "runtimes/service"}}
			files["runtimes/service"] = []byte("not executed")
		},
	} {
		raw, keys := fixturePackage(t, edit)
		m, _, _ := testManager(t, keys)
		m.host = "v0.0.2-dev.202609091428"
		if _, err := m.Import(raw, "admin"); err == nil {
			t.Fatal("unsupported API or runtime accepted")
		}
	}
}

func TestHostVersionMetadataIsOptionalAndTestedVersionsNormalizePrefix(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) {
		m.Requires.ZBoard = ""
		m.Requires.Tested = []string{"v0.0.1"}
	})
	p, err := ReadPackage(raw, keys)
	if err != nil {
		t.Fatal(err)
	}
	if c := p.Manifest.Compatibility("v0.0.1"); !c.Compatible || !c.Tested {
		t.Fatal(c)
	}
	if c := p.Manifest.Compatibility("v0.0.1-dev"); !c.Compatible || c.Tested {
		t.Fatal(c)
	}
}
