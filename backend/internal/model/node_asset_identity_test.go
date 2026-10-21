package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNodeAssetLabelsAreServerGeneratedAndHidden(t *testing.T) {
	a, b := Node{AssetTag: "wire-choice"}, Node{AssetTag: "wire-choice"}
	if err := a.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if err := b.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if len(a.AssetTag) != 64 || a.AssetTag == b.AssetTag || a.AssetTag == "wire-choice" {
		t.Fatal("new assets did not get distinct server labels")
	}
	data, err := json.Marshal(a)
	if err != nil || bytes.Contains(data, []byte("asset_tag")) || bytes.Contains(data, []byte(a.AssetTag)) {
		t.Fatal("internal asset label exposed", err)
	}
	var inbound Node
	if err := json.Unmarshal([]byte(`{"asset_tag":"wire-choice","AssetTag":"wire-choice"}`), &inbound); err != nil || inbound.AssetTag != "" {
		t.Fatal("JSON chose an internal asset label", err)
	}
}

func TestNodeAssetEntropyFailureIsNotAnIdentity(t *testing.T) {
	if tag, err := nodeAssetTag(strings.NewReader("short")); !errors.Is(err, io.ErrUnexpectedEOF) || tag != "" {
		t.Fatal("short entropy accepted as identity", err)
	}
}
