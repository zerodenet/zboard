//go:build linux

package localworkers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalWorkerChangedConfigurationNeverLaunchesNextChild(t *testing.T) {
	for _, change := range []string{"same-size-content", "same-content-new-inode", "missing", "directory"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "config")
			original := []byte("database: ORIGINAL_SECRET_A")
			if err := os.WriteFile(config, original, 0600); err != nil {
				t.Fatal(err)
			}
			launcher, err := NewCommandLauncher(os.Args[0], config, time.Second, 128<<20)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(config)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "same-size-content":
				modified := []byte("database: MODIFIED_SECRET_B")
				if len(modified) != len(original) {
					t.Fatal("same-size fixture required")
				}
				if err := os.WriteFile(config, modified, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(config, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "same-content-new-inode":
				replacement := filepath.Join(dir, "replacement")
				if err := os.WriteFile(replacement, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, config); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(config); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(config); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(config, 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ZBOARD_TEST_LOCAL_WORKER_ROLE", "worker")
			t.Setenv("ZBOARD_TEST_LOCAL_WORKER_DIR", dir)
			var output bytes.Buffer
			launcher.Stdout = &output
			launcher.Stderr = &output
			child, err := launcher.Start(t.Context())
			if err == nil || child != nil {
				if child != nil {
					_ = child.Kill()
				}
				t.Fatal("changed configuration launched a child")
			}
			if strings.Contains(err.Error(), "SECRET") || output.Len() != 0 {
				t.Fatal("configuration refusal exposed source values or ran the child")
			}
		})
	}
}
func TestLocalWorkerConfigurationMustBeBoundedRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oversize")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(4<<20 + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if launcher, err := NewCommandLauncher(os.Args[0], path, time.Second, 128<<20); err == nil || launcher != nil {
		t.Fatal("unbounded configuration accepted")
	}
}
