package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zeroArtifactFillReader struct{}

func (zeroArtifactFillReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func zeroSizedArtifact(t *testing.T, size int64, body bool) zeroRelease {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "zero", Mode: 0o755, Size: size, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if body {
		if _, err := io.CopyN(tw, zeroArtifactFillReader{}, size); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// Header-only fixtures exercise rejection before allocation or body reads.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "zero.tar.gz")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	return zeroRelease{LocalPath: path, ArtifactSize: int64(archive.Len()), ArtifactSHA256: hex.EncodeToString(digest[:])}
}

func TestDownloadZeroBinaryAcceptsReleaseLargerThan64MiB(t *testing.T) {
	const size = (64 << 20) + 1
	binary, digest, err := downloadZeroBinary(context.Background(), zeroSizedArtifact(t, size, true))
	if err != nil {
		t.Fatal(err)
	}
	if len(binary) != size {
		t.Fatalf("binary size = %d, want %d", len(binary), size)
	}
	want := sha256.Sum256(binary)
	if digest != hex.EncodeToString(want[:]) {
		t.Fatal("binary digest mismatch")
	}
}

func TestDownloadZeroBinaryRejectsInvalidExpandedSize(t *testing.T) {
	for _, size := range []int64{0, zeroBinaryMaxBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			_, _, err := downloadZeroBinary(context.Background(), zeroSizedArtifact(t, size, false))
			if err == nil || !strings.Contains(err.Error(), "invalid size") ||
				!strings.Contains(err.Error(), fmt.Sprintf("%d bytes", size)) {
				t.Fatalf("expected informative size rejection, got %v", err)
			}
		})
	}
}

func TestDownloadZeroBinaryStillRequiresArchiveChecksum(t *testing.T) {
	release := zeroSizedArtifact(t, 1, true)
	release.ArtifactSHA256 = strings.Repeat("0", 64)
	if _, _, err := downloadZeroBinary(context.Background(), release); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected archive checksum rejection, got %v", err)
	}
}
