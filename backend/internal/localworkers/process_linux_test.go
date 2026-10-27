//go:build linux

package localworkers

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/zerodenet/zboard/backend/internal/testsupport/localworkerlock"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"
	"time"
)

var helperConfig = flag.String("f", "", "local process fixture configuration")
var helperWorker = flag.Bool("projection-worker", false, "local process fixture worker")
var helperBackground = flag.Bool("projection-worker-background", false, "local process fixture priority")
var helperConnections = flag.Int("projection-worker-database-connections", 0, "local process fixture connection cap")
var helperDrain = flag.Duration("projection-worker-drain-timeout", 0, "local process fixture drain")

func TestMain(m *testing.M) {
	if role := os.Getenv("ZBOARD_TEST_LOCAL_WORKER_ROLE"); role != "" {
		flag.Parse()
		if role == "worker" {
			processFixtureWorker()
		} else if role == "owner" {
			processFixtureOwner()
		}
		os.Exit(97)
	}
	os.Exit(m.Run())
}

type fixtureProcessStatus struct {
	PID         int
	GOMAXPROCS  int
	MemoryLimit int64
}

func writeProcessFixture(dir, name string, value any) {
	raw, err := json.Marshal(value)
	if err != nil || os.WriteFile(filepath.Join(dir, name+".tmp"), raw, 0600) != nil || os.Rename(filepath.Join(dir, name+".tmp"), filepath.Join(dir, name)) != nil {
		os.Exit(96)
	}
}

func processFixtureWorker() {
	dir := os.Getenv("ZBOARD_TEST_LOCAL_WORKER_DIR")
	if !*helperWorker || !*helperBackground || *helperConnections != 2 || *helperDrain <= 0 || *helperConfig == "" {
		os.Exit(95)
	}
	if _, err := os.Stat(*helperConfig); err != nil || BackgroundPriority() != nil {
		os.Exit(94)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(stop)
	writeProcessFixture(dir, "worker.ready", fixtureProcessStatus{PID: os.Getpid(), GOMAXPROCS: runtime.GOMAXPROCS(0), MemoryLimit: debug.SetMemoryLimit(-1)})
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			writeProcessFixture(dir, "parent.signal", true)
			if os.Getenv("ZBOARD_TEST_LOCAL_WORKER_HOLD") != "1" {
				os.Exit(0)
			}
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				os.Exit(0)
			}
		case <-deadline.C:
			os.Exit(93) // Every crash fixture remains bounded if its test fails.
		}
	}
}

func processFixtureOwner() {
	dir := os.Getenv("ZBOARD_TEST_LOCAL_WORKER_DIR")
	ownership, err := AcquireLock(filepath.Join(dir, "owner.lock"))
	if err != nil {
		os.Exit(92)
	}
	defer ownership.Close()
	launcher, err := NewCommandLauncher(os.Args[0], filepath.Join(dir, "config"), time.Second, 128<<20)
	if err != nil {
		os.Exit(91)
	}
	launcher.Ownership = ownership
	os.Setenv("ZBOARD_TEST_LOCAL_WORKER_ROLE", "worker")
	process, err := launcher.Start(context.Background())
	if err != nil {
		os.Exit(90)
	}
	writeProcessFixture(dir, "owner.ready", fixtureProcessStatus{PID: process.PID()})
	select {
	case <-process.Done():
		os.Exit(89)
	case <-time.After(12 * time.Second):
		process.Drain()
		select {
		case <-process.Done():
		case <-time.After(2 * time.Second):
			process.Kill()
		}
		os.Exit(88)
	}
}

func waitProcessFixture(t *testing.T, path string, value any) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, value) == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("process fixture did not reach expected boundary", filepath.Base(path))
		case <-tick.C:
		}
	}
}

func processFixtureEnvironment(role, dir string) []string {
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "ZBOARD_TEST_LOCAL_WORKER_") {
			env = append(env, value)
		}
	}
	return append(env, "ZBOARD_TEST_LOCAL_WORKER_ROLE="+role, "ZBOARD_TEST_LOCAL_WORKER_DIR="+dir)
}

func TestLocalWorkerCommandLaunchUsesRealProcessLimitsAndGracefulDrain(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte("owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZBOARD_TEST_LOCAL_WORKER_ROLE", "worker")
	t.Setenv("ZBOARD_TEST_LOCAL_WORKER_DIR", dir)
	launcher, err := NewCommandLauncher(os.Args[0], config, time.Second, 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	launcher.Stdout, launcher.Stderr = nil, nil
	process, err := launcher.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Kill() })
	var status fixtureProcessStatus
	waitProcessFixture(t, filepath.Join(dir, "worker.ready"), &status)
	if status.PID != process.PID() || status.GOMAXPROCS != 1 || status.MemoryLimit != 128<<20 {
		t.Fatal("actual child ignored process-local resource controls", status)
	}
	if err := process.Drain(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-process.Done():
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("actual child did not drain")
	}
}

func TestLocalWorkerOwnerCrashDoesNotReleaseAnAliveChildOwnershipLock(t *testing.T) {
	localworkerlock.Hold(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	owner.Env = append(processFixtureEnvironment("owner", dir), "ZBOARD_TEST_LOCAL_WORKER_HOLD=1")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- owner.Wait() }()
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(dir, "release"), []byte("release owned fixture"), 0600)
		owner.Process.Kill()
	})
	var child fixtureProcessStatus
	waitProcessFixture(t, filepath.Join(dir, "owner.ready"), &child)
	var ready fixtureProcessStatus
	waitProcessFixture(t, filepath.Join(dir, "worker.ready"), &ready)
	if child.PID != ready.PID {
		t.Fatal("owner did not report its actual child")
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("fixture owner did not crash")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fixture owner did not exit")
	}
	var signaled bool
	waitProcessFixture(t, filepath.Join(dir, "parent.signal"), &signaled)
	if !signaled {
		t.Fatal("parent death did not signal its child")
	}
	if second, err := AcquireLock(filepath.Join(dir, "different-owner.lock")); err == nil {
		second.Close()
		t.Fatal("new supervisor bypassed an alive orphan's ownership")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), []byte("release owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if next, err := AcquireLock(filepath.Join(dir, "different-owner.lock")); err == nil {
			next.Close()
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("confirmed child exit did not release ownership")
		case <-tick.C:
		}
	}
}

// Different private paths must not multiply the namespace process budget.
func TestLocalWorkerNamespaceOwnershipRejectsDifferentPathsAndReleasesNormally(t *testing.T) {
	localworkerlock.Hold(t)
	dir := t.TempDir()
	first, err := AcquireLock(filepath.Join(dir, "first.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := AcquireLock(filepath.Join(dir, "second.lock")); err == nil {
		second.Close()
		t.Fatal("different private path bypassed namespace ownership")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := AcquireLock(filepath.Join(dir, "second.lock"))
	if err != nil {
		t.Fatal("normal close did not release namespace ownership", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	// Repeated Close must not close a later owner's reused descriptor.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkerOwnershipRefusesSymlinksAndExecutableReplacement(t *testing.T) {
	localworkerlock.Hold(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "preserved")
	if err := os.WriteFile(file, []byte("preserve ownership evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if owned, err := AcquireLock(link); err == nil {
		owned.Close()
		t.Fatal("symbolic ownership path accepted")
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "preserve ownership evidence" {
		t.Fatal("refused ownership was overwritten")
	}
	binary := filepath.Join(dir, "binary")
	if err := os.WriteFile(binary, []byte("original binary fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCommandLauncher(binary, file, time.Second, 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("changed binary fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if child, err := launcher.Start(t.Context()); err == nil {
		child.Kill()
		t.Fatal("replaced deployment launched a child")
	}
}
