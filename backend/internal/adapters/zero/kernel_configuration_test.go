package zero

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKernelRollbackRestoresConfigurationAfterLiveApply(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "missing", "dangling symlink"} {
		for _, automatic := range []bool{false, true} {
			name := kind + "/explicit"
			if automatic {
				name = kind + "/activation failure"
			}
			t.Run(name, func(t *testing.T) {
				f := newEventStateFixture(t)
				current := filepath.Join(f.root, "etc/zerodenet/current.json")
				oldLink, err := os.Readlink(current)
				if err != nil {
					t.Fatal(err)
				}
				if kind != "symlink" {
					if err := os.Remove(current); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "file" {
					f.write("etc/zerodenet/current.json", "previous persisted config\n")
					if err := os.Chmod(current, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "dangling symlink" {
					oldLink = filepath.Join(f.root, "missing-generation.json")
					if err := os.Symlink(oldLink, current); err != nil {
						t.Fatal(err)
					}
				}
				f.write("state/event-outbox.jsonl", "pending events\n")
				if automatic {
					f.write("restart-fails", "")
				}
				out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42))
				if (err != nil) != automatic {
					t.Fatalf("install: %v: %s", err, out)
				}
				if !automatic {
					// A successful config.apply replaces the installation symlink.
					if err := os.Remove(current); err != nil {
						t.Fatal(err)
					}
					f.write("etc/zerodenet/current.json", "new persisted config\n")
					for pass := 0; pass < 2; pass++ {
						if out, err := f.run(BuildKernelRollbackScript(42)); err != nil {
							t.Fatalf("rollback: %v: %s", err, out)
						}
					}
				}
				switch kind {
				case "file":
					if got := f.read("etc/zerodenet/current.json"); got != "previous persisted config\n" {
						t.Fatalf("restored config = %q", got)
					}
					info, err := os.Lstat(current)
					if err != nil || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
						t.Fatalf("file mode changed: %v, %v", info, err)
					}
				case "missing":
					if _, err := os.Lstat(current); !os.IsNotExist(err) {
						t.Fatalf("unexpected configuration after rollback: %v", err)
					}
				default:
					if got, err := os.Readlink(current); err != nil || got != oldLink {
						t.Fatalf("link = %q, %v; want %q", got, err, oldLink)
					}
				}
			})
		}
	}
}

func TestKernelRollbackSupportsLegacySymlinkMetadata(t *testing.T) {
	f := newEventStateFixture(t)
	if out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42)); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := os.Remove(filepath.Join(f.state, "backups/42/old_config_kind")); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run(BuildKernelRollbackScript(42)); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := f.read("etc/zerodenet/current.json"); got != "old config" {
		t.Fatalf("restored config = %q", got)
	}
}

func TestKernelRollbackRejectsAmbiguousLegacyBackupBeforeChangingRuntime(t *testing.T) {
	f := newEventStateFixture(t)
	if out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42)); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := os.Remove(filepath.Join(f.state, "backups/42/old_config_kind")); err != nil {
		t.Fatal(err)
	}
	f.write("state/backups/42/old_link", "\n")
	beforeBinary := f.read("usr/local/bin/zero")
	beforeCalls := f.read("calls")
	if out, err := f.run(BuildKernelRollbackScript(42)); err == nil || !strings.Contains(out, "manual recovery required") {
		t.Fatalf("rollback = %v: %s", err, out)
	}
	if f.read("etc/zerodenet/current.json") != "{}" || f.read("usr/local/bin/zero") != beforeBinary || f.read("calls") != beforeCalls {
		t.Fatal("ambiguous legacy rollback modified the current runtime")
	}
}
