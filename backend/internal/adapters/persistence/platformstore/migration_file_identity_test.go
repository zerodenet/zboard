package platformstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationFileIdentitySurvivesWritesAndDetectsReplacement(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "database.db")
	if err := os.WriteFile(filename, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := migrationFileIdentity(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("same physical file, new contents"), 0600); err != nil {
		t.Fatal(err)
	}
	same, err := migrationFileIdentity(filename)
	if err != nil || same != first {
		t.Fatal("file identity depended on contents or modification time", err)
	}
	if err := os.Rename(filename, filename+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("same physical file, new contents"), 0600); err != nil {
		t.Fatal(err)
	}
	replacement, err := migrationFileIdentity(filename)
	if err != nil || replacement == first {
		t.Fatal("a replacement at the same path reused database identity", err)
	}
}
