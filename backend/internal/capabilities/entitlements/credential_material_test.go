package entitlements

import (
	"encoding/base64"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSubscriptionMaterialProfilesAndAttribution(t *testing.T) {
	seed, err := NewCredentialMaterial()
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewCredentialMaterial()
	if seed == other {
		t.Fatal("reused seed")
	}
	values := map[string]string{}
	for _, c := range []struct {
		protocol, method string
		size             int
	}{
		{"vless", "", 16}, {"vmess", "", 16}, {"trojan", "", 32}, {"hysteria2", "", 32}, {"mieru", "", 32},
		{"shadowsocks", "aes-128-gcm", 32}, {"shadowsocks", "2022-blake3-aes-128-gcm", 16},
		{"shadowsocks", "2022-blake3-aes-256-gcm", 32}, {"shadowsocks", "2022-blake3-chacha20-poly1305", 32},
	} {
		got, e := RenderCredentialMaterial(seed, c.protocol, c.method)
		if e != nil {
			t.Fatal(e)
		}
		repeat, _ := RenderCredentialMaterial(seed, c.protocol, c.method)
		if got != repeat {
			t.Fatal("not deterministic")
		}
		different, _ := RenderCredentialMaterial(other, c.protocol, c.method)
		if got == different {
			t.Fatal("different subscription shared material")
		}
		values[c.protocol+c.method] = got
		if c.protocol == "vless" || c.protocol == "vmess" {
			id, e := uuid.Parse(got)
			if e != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
				t.Fatal("invalid UUID", e)
			}
			continue
		}
		encoding := base64.RawURLEncoding
		if strings.HasPrefix(c.method, "2022-") {
			encoding = base64.StdEncoding
		}
		raw, e := encoding.DecodeString(got)
		if e != nil || len(raw) != c.size {
			t.Fatal(c.protocol, c.method, len(raw), e)
		}
	}
	if values["vless"] != values["vmess"] || values["trojan"] != values["mieru"] || values["trojan"] != values["shadowsocksaes-128-gcm"] {
		t.Fatal("profiles were needlessly split by protocol")
	}
	if values["trojan"] == values["shadowsocks2022-blake3-aes-256-gcm"] {
		t.Fatal("missing domain separation")
	}
	verifier, e := CredentialMaterialVerifier(seed)
	if e != nil {
		t.Fatal(e)
	}
	binding := CredentialBinding{CredentialID: 11, NodeID: 21, EndpointID: 31}
	principal, e := CredentialBindingPrincipal(seed, binding)
	if e != nil || len(principal) > 128 {
		t.Fatal(len(principal), e)
	}
	// Verify needs no usable seed, including after secret erasure.
	seed = ""
	got, e := VerifyCredentialBinding(verifier, principal)
	if e != nil || got != binding {
		t.Fatal(got, e)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(principal, "sp1:"))
	for _, position := range []int{7, 15, 23, 24, len(raw) - 1} {
		tampered := append([]byte{}, raw...)
		tampered[position] ^= 1
		if _, e := VerifyCredentialBinding(verifier, "sp1:"+base64.RawURLEncoding.EncodeToString(tampered)); e == nil {
			t.Fatal("accepted tampered binding", position)
		}
	}
	wrong, _ := CredentialMaterialVerifier(other)
	if _, e := VerifyCredentialBinding(wrong, principal); e == nil {
		t.Fatal("wrong public verifier")
	}
	for _, value := range []string{"", "sp1:garbage", principal + "=", strings.Replace(principal, "sp1:", "sp2:", 1)} {
		if _, e := VerifyCredentialBinding(verifier, value); e == nil {
			t.Fatal("accepted malformed principal")
		}
	}
}

func TestSubscriptionMaterialRejectsInvalidProfiles(t *testing.T) {
	seed, _ := NewCredentialMaterial()
	for _, c := range []struct{ seed, protocol, method string }{{"bad", "vless", ""}, {seed, "unsupported", ""}, {seed, "shadowsocks", "2022-unknown"}} {
		if _, e := RenderCredentialMaterial(c.seed, c.protocol, c.method); e == nil {
			t.Fatal("invalid profile accepted")
		}
	}
	if _, e := CredentialBindingPrincipal(seed, CredentialBinding{}); e == nil {
		t.Fatal("zero binding accepted")
	}
}
