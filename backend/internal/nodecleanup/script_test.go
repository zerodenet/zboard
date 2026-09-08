package nodecleanup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the embedded shell with real files under a temp root and mocked
// systemd/signals. The production script exposes no alternate-root option.
func runFixture(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	source := string(Script)
	const entry = "\nmain \"$@\"\n"
	if !strings.HasSuffix(source, entry) {
		t.Fatal("shell entrypoint changed")
	}
	body := strings.TrimSuffix(source, entry) + `
cleanup_root=$FIXTURE_ROOT
id() { printf '%s\n' "${FIXTURE_UID:-0}"; }
systemctl() { :; }
timeout() { :; }
sleep() { :; }
kill() {
 printf 'signal %s\n' "$*" >> "$FIXTURE_ROOT/calls"
 rm -rf -- "$FIXTURE_ROOT/proc/$2"
}
ctl() {
 printf '%s\n' "$*" >> "$FIXTURE_ROOT/calls"
 case "$*" in
  *--property=ExecStart*)
   if [ -f "$FIXTURE_ROOT/foreign" ]; then printf '/other/zero run /other.json\n'
   else printf '/usr/local/bin/zero run /etc/zerodenet/current.json\n'; fi;;
  *--property=LoadState*) printf 'loaded\n';;
  *--property=ActiveState*)
   if [ -f "$FIXTURE_ROOT/active" ]; then cat "$FIXTURE_ROOT/active"; else printf 'inactive\n'; fi;;
  stop*)
   if [ -f "$FIXTURE_ROOT/hung" ]; then rm "$FIXTURE_ROOT/hung"; return 124; fi
   printf 'inactive\n' > "$FIXTURE_ROOT/active";;
  kill*) printf 'inactive\n' > "$FIXTURE_ROOT/active";;
 esac
}
main "$@"
`
	script := filepath.Join(t.TempDir(), "fixture.sh")
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), "FIXTURE_ROOT="+root)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
func fixtureFile(t *testing.T, root, name, value string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func exists(path string) bool { _, err := os.Lstat(path); return err == nil }
func calls(t *testing.T, root string) string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(root, "calls"))
	return string(raw)
}

func TestShellStopDisablesBeforeKillingHungServiceAndRetainsFiles(t *testing.T) {
	root := t.TempDir()
	config := fixtureFile(t, root, "etc/zerodenet/current.json", "{}")
	fixtureFile(t, root, "active", "active\n")
	fixtureFile(t, root, "hung", "")
	if output, err := runFixture(t, root, "stop", "--yes"); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	log := calls(t, root)
	disabled, killed := strings.Index(log, "disable zero.service"), strings.Index(log, "kill --signal=KILL zero.service")
	if disabled < 0 || killed < disabled || !exists(config) {
		t.Fatalf("stop contract: %s", log)
	}
}
func TestShellUninstallScopesFilesAndRequiresCertificateOptIn(t *testing.T) {
	root := t.TempDir()
	config := fixtureFile(t, root, "etc/zerodenet/current.json", "{}")
	queue := fixtureFile(t, root, "var/lib/zerodenet/event-outbox.jsonl", "pending")
	cert := fixtureFile(t, root, "etc/zboard/certificates/1/key.pem", "key")
	owned := fixtureFile(t, root, "etc/letsencrypt/renewal/zboard-1.conf", "renewal")
	other := fixtureFile(t, root, "etc/letsencrypt/renewal/other.conf", "other")
	similar := fixtureFile(t, root, "etc/letsencrypt/renewal/zboard-other.conf", "other")
	if output, err := runFixture(t, root, "uninstall", "--yes"); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if exists(config) || exists(queue) || !exists(cert) || !exists(owned) {
		t.Fatal("wrong default removal scope")
	}
	for i := 0; i < 2; i++ {
		if output, err := runFixture(t, root, "uninstall", "--yes", "--certificates"); err != nil {
			t.Fatalf("%v: %s", err, output)
		}
	}
	if exists(cert) || exists(owned) || !exists(other) || !exists(similar) {
		t.Fatal("wrong certificate removal scope")
	}
}
func TestShellRefusesSymlinksAndForeignServiceBeforeRemoval(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	sentinel := fixtureFile(t, outside, "keep", "keep")
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "etc/zerodenet")); err != nil {
		t.Fatal(err)
	}
	if output, err := runFixture(t, root, "uninstall", "--yes"); err == nil || !strings.Contains(output, "symlink") {
		t.Fatalf("%v: %s", err, output)
	}
	if !exists(sentinel) || strings.Contains(calls(t, root), "disable") {
		t.Fatal("symlink check happened after mutation")
	}
	if err := os.Remove(filepath.Join(root, "etc/zerodenet")); err != nil {
		t.Fatal(err)
	}
	config := fixtureFile(t, root, "etc/zerodenet/current.json", "{}")
	fixtureFile(t, root, "foreign", "")
	if output, err := runFixture(t, root, "uninstall", "--yes"); err == nil || !strings.Contains(output, "another binary") {
		t.Fatalf("%v: %s", err, output)
	}
	if !exists(config) {
		t.Fatal("removed foreign configuration")
	}
}
func TestShellSignalsOnlyExactManagedBinaryAndConfig(t *testing.T) {
	root := t.TempDir()
	for _, proc := range []struct{ pid, binary, config string }{{"111", "/usr/local/bin/zero (deleted)", "/etc/zerodenet/current.json"}, {"222", "/usr/local/bin/zero", "/other.json"}, {"333", "/other/zero", "/etc/zerodenet/current.json"}} {
		fixtureFile(t, root, "proc/"+proc.pid+"/cmdline", proc.binary+"\x00run\x00"+proc.config+"\x00")
		if err := os.Symlink(proc.binary, filepath.Join(root, "proc", proc.pid, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runFixture(t, root, "stop", "--yes"); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	log := calls(t, root)
	if !strings.Contains(log, "signal -TERM 111") || strings.Contains(log, "signal -TERM 222") || strings.Contains(log, "signal -TERM 333") {
		t.Fatalf("signals: %s", log)
	}
}
func TestShellDefaultStatusAndMissingConfirmationNeverMutate(t *testing.T) {
	root := t.TempDir()
	config := fixtureFile(t, root, "etc/zerodenet/current.json", "{}")
	if output, err := runFixture(t, root); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if output, err := runFixture(t, root, "uninstall"); err == nil || !strings.Contains(output, "--yes") {
		t.Fatalf("%v: %s", err, output)
	}
	if !exists(config) || strings.Contains(calls(t, root), "disable") {
		t.Fatal("unconfirmed mutation")
	}
}
func TestShellSyntax(t *testing.T) {
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(string(Script))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}
