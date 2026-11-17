package localfiles

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestStoragePersistsAcrossInstancesAndRejectsTraversalAndReplacement(t *testing.T) {
	directory := t.TempDir()
	first := &Store{Directory: directory}
	id := uuid.NewString()
	if err := first.Put(context.Background(), id, []byte("retained")); err != nil {
		t.Fatal(err)
	}
	if err := first.Put(context.Background(), id, []byte("replacement")); err == nil {
		t.Fatal("immutable file replaced")
	}
	second := &Store{Directory: directory}
	file, err := second.Open(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(data) != "retained" {
		t.Fatal(string(data), err)
	}
	if err := first.Put(context.Background(), "../escape", []byte("bad")); err == nil {
		t.Fatal("traversal accepted")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	linkID := uuid.NewString()
	if err := os.Symlink(outside, filepath.Join(directory, linkID)); err != nil {
		t.Fatal(err)
	}
	if file, err := second.Open(context.Background(), linkID); err == nil {
		file.Close()
		t.Fatal("symlink escaped storage")
	}
	if err := second.Remove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := second.Remove(context.Background(), id); err != nil {
		t.Fatal("delete retry failed", err)
	}
}
