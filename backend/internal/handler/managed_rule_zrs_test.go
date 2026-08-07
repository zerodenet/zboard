package handler

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func init() {
	managedRuleZRSCompiler = func(source []byte) ([]byte, error) {
		digest := sha256.Sum256(source)
		return append([]byte("ZRS-TEST\n"), digest[:]...), nil
	}
}

func TestWriteManagedRuleSourcePublishesZRSBeforeSource(t *testing.T) {
	h := &handlers{zeroArtifactDir: t.TempDir()}
	source := []byte("{\"version\":1,\"rules\":[{\"type\":\"domain_exact\",\"value\":\"example.com\"}]}\n")
	if err := h.writeManagedRuleSource("example", source); err != nil {
		t.Fatalf("writeManagedRuleSource() error = %v", err)
	}

	digest := sha256.Sum256(source)
	artifactPath, err := h.managedRuleArtifactPath("example", digest, managedRuleArtifactZRS)
	if err != nil {
		t.Fatalf("managedRuleArtifactPath() error = %v", err)
	}
	artifact, err := readFile(artifactPath)
	if err != nil {
		t.Fatalf("read ZRS artifact: %v", err)
	}
	if !bytes.HasPrefix(artifact, []byte("ZRS-TEST\n")) {
		t.Fatalf("unexpected ZRS artifact %q", artifact)
	}
	stored, err := h.readManagedRuleSource("example")
	if err != nil {
		t.Fatalf("readManagedRuleSource() error = %v", err)
	}
	if !bytes.Equal(stored, source) {
		t.Fatalf("stored source = %q, want %q", stored, source)
	}
}
