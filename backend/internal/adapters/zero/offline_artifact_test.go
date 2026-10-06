package zero

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testOfflineELF() []byte {
	data := make([]byte, 128)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], 62)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint64(data[32:], 64)
	binary.LittleEndian.PutUint16(data[52:], 64)
	binary.LittleEndian.PutUint16(data[54:], 56)
	binary.LittleEndian.PutUint16(data[56:], 1)
	binary.LittleEndian.PutUint32(data[64:], 1)
	binary.LittleEndian.PutUint32(data[68:], 5)
	binary.LittleEndian.PutUint64(data[72:], 120)
	binary.LittleEndian.PutUint64(data[96:], 8)
	binary.LittleEndian.PutUint64(data[104:], 8)
	return data
}
func offlineArchive(t *testing.T, entries ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	data := testOfflineELF()
	for _, name := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0700, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func TestOfflineArtifactPersistsDeduplicatesAndDetectsTampering(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := OfflineArtifactStore{Root: root}
	id, size, err := store.Import(ctx, testOfflineELF())
	if err != nil || size != 128 {
		t.Fatalf("size=%d err=%v", size, err)
	}
	second, _, err := store.Import(ctx, offlineArchive(t, "zero"))
	if err != nil || id != second {
		t.Fatalf("dedup=%s err=%v", second, err)
	}
	data, err := (OfflineArtifactStore{Root: root}).Load(ctx, id)
	if err != nil || !bytes.Equal(data, testOfflineELF()) {
		t.Fatalf("restart load: %v", err)
	}
	file := filepath.Join(root, id+".bin")
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("uploaded executable must remain private and non-executable on panel")
	}
	if err := os.WriteFile(file, bytes.Repeat([]byte{'x'}, 128), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, id); err == nil {
		t.Fatal("tampered artifact accepted")
	}
	if _, _, err := store.Import(ctx, testOfflineELF()); err == nil {
		t.Fatal("corrupt existing artifact reused")
	}
}
func TestOfflineArtifactRejectsInvalidExecutablesAndArchives(t *testing.T) {
	arm := testOfflineELF()
	binary.LittleEndian.PutUint16(arm[18:], 183)
	truncated := testOfflineELF()[:125]
	checksum := offlineArchive(t, "zero")
	checksum[len(checksum)-8] ^= 1
	for name, data := range map[string][]byte{"empty": nil, "wrong platform": arm, "truncated ELF": truncated, "text": []byte("zero"), "traversal": offlineArchive(t, "../zero"), "duplicate": offlineArchive(t, "zero", "zero"), "gzip checksum": checksum} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := (OfflineArtifactStore{Root: t.TempDir()}).Import(context.Background(), data); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
}
func TestOfflineArtifactRejectsMissingStorageUnsafeReferencesAndFullStorage(t *testing.T) {
	ctx := context.Background()
	if _, _, err := (OfflineArtifactStore{}).Import(ctx, testOfflineELF()); err == nil {
		t.Fatal("missing storage accepted")
	}
	store := OfflineArtifactStore{Root: t.TempDir()}
	if _, err := store.Load(ctx, "../artifact"); err == nil {
		t.Fatal("unsafe reference accepted")
	}
	file, err := os.Create(filepath.Join(store.Root, "existing.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(offlineStoreMaxBytes); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, _, err := store.Import(ctx, testOfflineELF()); err == nil {
		t.Fatal("storage quota ignored")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.Import(canceled, testOfflineELF()); err == nil {
		t.Fatal("canceled upload accepted")
	}
}

func TestOfflineArtifactConcurrentImportsReuseImmutableFile(t *testing.T) {
	store := OfflineArtifactStore{Root: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := make(chan string, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { id, _, err := store.Import(ctx, testOfflineELF()); results <- id; failures <- err }()
	}
	expected := ""
	for i := 0; i < 4; i++ {
		id := <-results
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		if expected != "" && id != expected {
			t.Fatal("concurrent upload changed identity")
		}
		expected = id
	}
	entries, err := os.ReadDir(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected retained files: %d", len(entries))
	}
}
