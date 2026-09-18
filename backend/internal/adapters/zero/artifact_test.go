package zero

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zeroArtifactFixture(t *testing.T, binary []byte) ([]byte, string) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "README", Mode: 0o600, Size: 4, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("skip")); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "zero", Mode: 0o700, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	return archive.Bytes(), hex.EncodeToString(digest[:])
}

func TestArtifactLoaderVerifiesAndExtractsLocalBinary(t *testing.T) {
	binary := []byte("zero-binary")
	archive, checksum := zeroArtifactFixture(t, binary)
	path := filepath.Join(t.TempDir(), "zero.tar.gz")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	actual, binarySHA, err := (ArtifactLoader{}).LoadBinary(context.Background(), ArtifactRelease{LocalPath: path, Size: int64(len(archive)), SHA256: checksum})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	if !bytes.Equal(actual, binary) || binarySHA != hex.EncodeToString(digest[:]) {
		t.Fatalf("binary=%q sha=%s", actual, binarySHA)
	}
}

func TestArtifactLoaderRejectsUntrustedMetadata(t *testing.T) {
	archive, checksum := zeroArtifactFixture(t, []byte("zero-binary"))
	path := filepath.Join(t.TempDir(), "zero.tar.gz")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	loader := ArtifactLoader{}
	if _, _, err := loader.LoadBinary(context.Background(), ArtifactRelease{LocalPath: path, Size: int64(len(archive)), SHA256: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("checksum error=%v", err)
	}
	if _, _, err := loader.LoadBinary(context.Background(), ArtifactRelease{LocalPath: path, Size: int64(len(archive)) + 1, SHA256: checksum}); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("size error=%v", err)
	}
}

func TestArtifactLoaderRejectsUntrustedRemoteURLBeforeRequest(t *testing.T) {
	for _, rawURL := range []string{"http://github.com/zero.tar.gz", "https://example.test/zero.tar.gz"} {
		if _, _, err := (ArtifactLoader{}).LoadBinary(context.Background(), ArtifactRelease{URL: rawURL, Size: 1, SHA256: strings.Repeat("0", 64)}); err == nil {
			t.Fatalf("URL %q was accepted", rawURL)
		}
	}
}
