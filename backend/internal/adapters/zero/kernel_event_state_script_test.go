package zero

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type eventStateFixture struct {
	t                  *testing.T
	root, stage, state string
	sha                string
}

func newEventStateFixture(t *testing.T) *eventStateFixture {
	t.Helper()
	f := &eventStateFixture{t: t, root: t.TempDir()}
	f.stage, f.state = filepath.Join(f.root, "stage"), filepath.Join(f.root, "state")
	newBinary := "#!/bin/sh\nexit 0\n"
	digest := sha256.Sum256([]byte(newBinary))
	f.sha = hex.EncodeToString(digest[:])
	files := map[string]string{
		"stage/zero": newBinary, "stage/runtime.json": "{}", "stage/zero.env": "ZERO_PANEL_API_KEY=new\n",
		"stage/zero.service": "managed", "stage/cleanup-zero-node.sh": "#!/bin/sh\nsystemctl stop zero\n",
		"bin/id": "#!/bin/sh\necho 0\n", "bin/uname": "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo x86_64;; esac\n",
		"bin/flock": "#!/bin/sh\nexit 0\n", "bin/sha256sum": "#!/bin/sh\nexec shasum -a 256 \"$@\"\n",
		"bin/systemctl": `#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE_ROOT/calls"
case "$1" in
 is-active|is-enabled) exit 0;;
 stop) test ! -f "$FIXTURE_ROOT/stop-fails";;
 restart)
  if [ -f "$FIXTURE_ROOT/restart-fails" ]; then
   rm "$FIXTURE_ROOT/restart-fails"
   printf 'new node event\n' > "$FIXTURE_ROOT/state/event-outbox.jsonl"
   exit 1
  fi;;
esac
`,
		"usr/local/bin/zero": "old binary", "etc/zerodenet/zero.env": "ZERO_PANEL_API_KEY=old\n",
		"etc/systemd/system/zero.service": "old service", "etc/zerodenet/generations/old.json": "old config",
	}
	for name, body := range files {
		f.write(name, body)
	}
	if err := os.Symlink(filepath.Join(f.root, "etc/zerodenet/generations/old.json"), filepath.Join(f.root, "etc/zerodenet/current.json")); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *eventStateFixture) write(name, body string) {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		f.t.Fatal(err)
	}
}

func (f *eventStateFixture) read(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *eventStateFixture) run(script string) (string, error) {
	f.t.Helper()
	script = strings.NewReplacer(
		"in /var /var/lib ", "in ", "mv -Tf", "mv -f",
		"/var/lib/zerodenet", f.state, "/etc/zerodenet", filepath.Join(f.root, "etc/zerodenet"),
		"/etc/systemd/system", filepath.Join(f.root, "etc/systemd/system"),
		"/usr/local/bin", filepath.Join(f.root, "usr/local/bin"),
		"/usr/local/sbin", filepath.Join(f.root, "usr/local/sbin"),
	).Replace(script)
	cmd := exec.Command("sh")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "FIXTURE_ROOT="+f.root, "PATH="+filepath.Join(f.root, "bin")+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestKernelInitializationIsolatesForeignAndUnknownQueues(t *testing.T) {
	for _, previousOwner := range []string{"", strings.Repeat("b", 64)} {
		t.Run(previousOwner, func(t *testing.T) {
			f := newEventStateFixture(t)
			f.write("state/event-outbox.jsonl", "old node event\n")
			if previousOwner != "" {
				f.write("state/zboard-event-state.owner", previousOwner+"\n")
			}
			f.write("stage/event-state.plan", strings.Repeat("a", 64)+" 0 reset\n")
			if out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42)); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if f.read("state/backups/42/event-state.outbox") != "old node event\n" {
				t.Fatal("old accounting events were lost")
			}
			if _, err := os.Stat(filepath.Join(f.state, "event-outbox.jsonl")); !os.IsNotExist(err) {
				t.Fatal("old queue remains in the live delivery path")
			}
			calls := f.read("calls")
			if strings.Index(calls, "stop zero") > strings.Index(calls, "restart zero") || !strings.Contains(calls, "stop zero") {
				t.Fatal("queue was not isolated while its writer was stopped")
			}
		})
	}
}

func TestSameNodeUpgradePreservesQueueAndLegacyUpgradeCanAdoptIt(t *testing.T) {
	for _, marked := range []bool{true, false} {
		t.Run(map[bool]string{true: "owned", false: "legacy"}[marked], func(t *testing.T) {
			f := newEventStateFixture(t)
			f.write("state/event-outbox.jsonl", "unacknowledged events\n")
			if marked {
				f.write("state/zboard-event-state.owner", strings.Repeat("a", 64)+"\n")
			}
			f.write("stage/event-state.plan", strings.Repeat("a", 64)+" 1 preserve\n")
			if out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42)); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if f.read("state/event-outbox.jsonl") != "unacknowledged events\n" || strings.Contains(f.read("calls"), "stop zero") {
				t.Fatal("same-node upgrade reset pending delivery state")
			}
		})
	}
}

func TestKernelInitializationRollbackRestoresPreviousQueue(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		t.Run(map[bool]string{true: "activation fails", false: "explicit rollback"}[automatic], func(t *testing.T) {
			f := newEventStateFixture(t)
			f.write("state/event-outbox.jsonl", "old node event\n")
			f.write("state/zboard-event-state.owner", strings.Repeat("b", 64)+"\n")
			f.write("stage/event-state.plan", strings.Repeat("a", 64)+" 0 reset\n")
			if automatic {
				f.write("restart-fails", "")
			}
			out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42))
			if automatic == (err == nil) {
				t.Fatalf("%v: %s", err, out)
			}
			if !automatic {
				f.write("state/event-outbox.jsonl", "new node event\n")
				for attempt := 0; attempt < 2; attempt++ {
					if out, err := f.run(BuildKernelRollbackScript(42)); err != nil {
						t.Fatalf("%v: %s", err, out)
					}
				}
			}
			if f.read("state/event-outbox.jsonl") != "old node event\n" || f.read("state/zboard-event-state.owner") != strings.Repeat("b", 64)+"\n" {
				t.Fatal("rollback changed old node event ownership")
			}
			if f.read("state/backups/42/event-state.failed") != "new node event\n" {
				t.Fatal("new node events were mixed into the old queue")
			}
		})
	}
}

func TestPublicationCannotAdoptUninitializedOrForeignEventState(t *testing.T) {
	for _, previous := range []string{"", strings.Repeat("b", 64)} {
		f := newEventStateFixture(t)
		f.write("state/event-outbox.jsonl", "old node event\n")
		f.write("stage/event-state.guard", strings.Repeat("a", 64)+" 0\n")
		if previous != "" {
			f.write("state/zboard-event-state.owner", previous+"\n")
		}
		out, err := f.run(BuildConfigurationPublishScript(f.stage, f.sha, 42))
		if err == nil || !configurationConfirmedUnapplied(out) || !strings.Contains(out, "initialize this node") {
			t.Fatalf("publication bypassed initialization: %v %s", err, out)
		}
		if f.read("state/event-outbox.jsonl") != "old node event\n" {
			t.Fatal("rejected publication changed old events")
		}
		if _, err := os.Stat(filepath.Join(f.root, "calls")); !os.IsNotExist(err) {
			t.Fatal("rejected publication changed service state")
		}
	}
}

func TestKernelInitializationRefusesOwnershipChangesAndSymlinksBeforeStopping(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		f := newEventStateFixture(t)
		f.write("state/event-outbox.jsonl", "pending events\n")
		f.write("stage/event-state.plan", strings.Repeat("a", 64)+" 0 reset\n")
		if symlink {
			if err := os.Symlink(filepath.Join(f.root, "outside"), filepath.Join(f.state, "zboard-event-state.owner")); err != nil {
				t.Fatal(err)
			}
		} else {
			// Another initialization established this owner after our preflight.
			f.write("state/zboard-event-state.owner", strings.Repeat("a", 64)+"\n")
		}
		if out, err := f.run(BuildKernelInstallScript(f.stage, f.sha, 42)); err == nil {
			t.Fatalf("stale/unsafe ownership plan was applied: %s", out)
		}
		if f.read("state/event-outbox.jsonl") != "pending events\n" {
			t.Fatal("unsafe initialization changed events")
		}
		if _, err := os.Stat(filepath.Join(f.root, "calls")); !os.IsNotExist(err) {
			t.Fatal("ownership rejection changed service state")
		}
	}
}
