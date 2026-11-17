//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package zeroevent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func ownershipSpool(t *testing.T, cfg Config) *FileSpool {
	t.Helper()
	s, err := NewFileSpool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func ownershipWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for spool operation")
	}
}

func TestFileSpoolOwnershipSameProcessAndPermanentInode(t *testing.T) {
	cfg := testFileConfig(t)
	first := ownershipSpool(t, cfg)
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatal("same object Start must remain idempotent:", err)
	}
	lockPath := filepath.Join(cfg.Directory, spoolOwnershipFileName)
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	second := ownershipSpool(t, cfg)
	if err := second.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("second owner: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(cfg.Directory, alias); err != nil {
		t.Fatal(err)
	}
	aliasCfg := cfg
	aliasCfg.Directory = alias
	aliased := ownershipSpool(t, aliasCfg)
	if err := aliased.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("aliased owner: %v", err)
	}
	if err := first.Append(context.Background(), testEnvelope("owned", 1)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third := ownershipSpool(t, cfg)
	if err := third.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("old Close released new ownership: %v", err)
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("ownership lock inode was replaced")
	}
	batch, err := second.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].ID != "owned" {
		t.Fatalf("replay: %+v, %v", batch, err)
	}
}

func TestFileSpoolOwnershipBeforeRecoveryAndReserve(t *testing.T) {
	cfg := testFileConfig(t)
	owner, err := acquireSpoolOwnership(cfg.Directory)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSpoolOwnership(owner)
	// These inputs would fail checkpoint parsing and truncate recovery's active
	// tail. A rejected owner must not reach either, or allocate its reserve.
	checkpoint := []byte("invalid-checkpoint")
	partial := []byte("partial-active-record")
	activePath := segmentPath(cfg.Directory, 1, segmentActiveSuffix)
	if err := os.WriteFile(filepath.Join(cfg.Directory, checkpointFileName), checkpoint, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, partial, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg.Storage.EmergencyReserve = 4096
	s := ownershipSpool(t, cfg)
	if err := s.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("ownership must precede recovery: %v", err)
	}
	for path, expected := range map[string]string{activePath: string(partial), filepath.Join(cfg.Directory, checkpointFileName): string(checkpoint)} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != expected {
			t.Fatalf("rejected startup changed %s: %q %v", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.Directory, emergencyReserveFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserve created before ownership: %v", err)
	}
}

func TestFileSpoolOwnershipFailedStartReleasesAndRetries(t *testing.T) {
	cfg := testFileConfig(t)
	path := filepath.Join(cfg.Directory, checkpointFileName)
	if err := os.WriteFile(path, []byte("broken"), 0o640); err != nil {
		t.Fatal(err)
	}
	s := ownershipSpool(t, cfg)
	if err := s.Start(context.Background()); err == nil || errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("expected checkpoint failure: %v", err)
	}
	probe, err := acquireSpoolOwnership(cfg.Directory)
	if err != nil {
		t.Fatalf("failed startup leaked ownership: %v", err)
	}
	if err := releaseSpoolOwnership(probe); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("same object retry: %v", err)
	}
}

func TestFileSpoolOwnershipParentCancellationRetainsCommit(t *testing.T) {
	cfg := testFileConfig(t)
	s := ownershipSpool(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 2; i++ {
		if err := s.Append(context.Background(), testEnvelope(fmt.Sprint(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := s.ReadBatch(context.Background(), 1)
	if err != nil || len(batch.Events) != 1 {
		t.Fatalf("batch: %+v %v", batch, err)
	}
	cancel()
	ownershipWait(t, s.writerDone)
	ownershipWait(t, s.compressionDone)
	ownershipWait(t, s.storageDone)
	next := ownershipSpool(t, cfg)
	if err := next.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("parent cancellation released ownership: %v", err)
	}
	if err := s.Commit(context.Background(), batch.Next); err != nil {
		t.Fatalf("commit after parent cancellation: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := next.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	replay, err := next.ReadBatch(context.Background(), 10)
	if err != nil || len(replay.Events) != 1 || replay.Events[0].Sequence != 2 {
		t.Fatalf("checkpoint replay: %+v %v", replay, err)
	}
}

// Block Err at a real operation boundary after its lifecycle check while that
// operation owns readMu. No production hooks or timing assumptions are needed.
type ownershipGateContext struct {
	context.Context
	call    atomic.Int32
	gateAt  int32
	entered chan struct{}
	release chan struct{}
}

func (c *ownershipGateContext) Err() error {
	if c.call.Add(1) == c.gateAt {
		close(c.entered)
		<-c.release
	}
	return c.Context.Err()
}

func TestFileSpoolOwnershipCloseWaitsForReadAndCompact(t *testing.T) {
	for _, operation := range []string{"read", "compact"} {
		t.Run(operation, func(t *testing.T) {
			cfg := testFileConfig(t)
			cfg.Compaction.Enabled = true
			writeRawTestSegment(t, cfg.Directory, 1, []Envelope{
				semanticTestEvent("old", "flow.updated", "core", "flow", 1),
				semanticTestEvent("new", "flow.updated", "core", "flow", 2),
			})
			s := ownershipSpool(t, cfg)
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			gate := &ownershipGateContext{Context: context.Background(), gateAt: 1, entered: make(chan struct{}), release: make(chan struct{})}
			if operation == "compact" {
				gate.gateAt = 2
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(gate.release) }) }
			defer release()
			operationDone := make(chan struct{})
			var operationErr error
			go func() {
				defer close(operationDone)
				if operation == "read" {
					_, operationErr = s.ReadBatch(gate, 10)
				} else {
					_, operationErr = s.Compact(gate)
				}
			}()
			ownershipWait(t, gate.entered)
			closed := make(chan error, 2)
			go func() { closed <- s.Close() }()
			// Observe the actual closing state before starting the second Close.
			deadline := time.After(10 * time.Second)
			for {
				s.mu.RLock()
				closing := s.closed
				s.mu.RUnlock()
				if closing {
					break
				}
				select {
				case <-deadline:
					t.Fatal("Close did not start")
				case <-time.After(time.Millisecond):
				}
			}
			go func() { closed <- s.Close() }()
			select {
			case err := <-closed:
				t.Fatalf("Close returned before in-flight %s: %v", operation, err)
			case <-time.After(25 * time.Millisecond):
			}
			probe := ownershipSpool(t, cfg)
			if err := probe.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
				t.Fatalf("ownership released before %s drained: %v", operation, err)
			}
			release()
			ownershipWait(t, operationDone)
			if operationErr != nil {
				t.Fatal(operationErr)
			}
			for i := 0; i < 2; i++ {
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("concurrent Close did not complete")
				}
			}
			if err := probe.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			batch, err := probe.ReadBatch(context.Background(), 10)
			want := 2
			if operation == "compact" {
				want = 1
			}
			if err != nil || len(batch.Events) != want {
				t.Fatalf("reopened %s result: %+v %v", operation, batch, err)
			}
		})
	}
}

func TestFileSpoolOwnershipCloseBeforeStartIsConcurrentSafe(t *testing.T) {
	for _, s := range []*FileSpool{ownershipSpool(t, testFileConfig(t)), {}} {
		var wait sync.WaitGroup
		for i := 0; i < 8; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			}()
		}
		wait.Wait()
		if err := s.Start(context.Background()); !errors.Is(err, ErrSpoolClosed) {
			t.Fatalf("Start after Close: %v", err)
		}
	}
}

func TestFileSpoolOwnershipCloseWaitsForCommit(t *testing.T) {
	cfg := testFileConfig(t)
	s := ownershipSpool(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(context.Background(), testEnvelope("committing", 1)); err != nil {
		t.Fatal(err)
	}
	batch, err := s.ReadBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Drain background users first, so the only possible readMu holder below is
	// this real Commit. Hold representation validation until Close has begun.
	cancel()
	ownershipWait(t, s.writerDone)
	ownershipWait(t, s.compressionDone)
	ownershipWait(t, s.storageDone)
	s.segmentMu.Lock()
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(s.segmentMu.Unlock) }
	defer unlock()
	committed := make(chan error, 1)
	go func() { committed <- s.Commit(context.Background(), batch.Next) }()
	deadline := time.After(10 * time.Second)
	for {
		if !s.readMu.TryLock() {
			break
		}
		s.readMu.Unlock()
		select {
		case <-deadline:
			t.Fatal("Commit did not enter readMu")
		case <-time.After(time.Millisecond):
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while Commit owns readMu: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	probe := ownershipSpool(t, cfg)
	if err := probe.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("ownership released during Commit: %v", err)
	}
	unlock()
	select {
	case err := <-committed:
		// A call already past admission may commit; one still acquiring mu may
		// observe Close and reject. Both must finish before ownership transfers.
		if err != nil && !errors.Is(err, ErrSpoolClosed) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Commit did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not finish")
	}
	if err := probe.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFileSpoolOwnershipProcessHelper(t *testing.T) {
	mode := os.Getenv("ZEROEVENT_OWNERSHIP_HELPER")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	cfg := testFileConfig(t)
	cfg.Directory = os.Getenv("ZEROEVENT_OWNERSHIP_DIRECTORY")
	s := ownershipSpool(t, cfg)
	err := s.Start(context.Background())
	if mode == "reject" {
		if !errors.Is(err, ErrSpoolInUse) {
			t.Fatalf("cross-process contender: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 2; i++ {
		if err := s.Append(context.Background(), testEnvelope(fmt.Sprint(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := s.ReadBatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(context.Background(), batch.Next); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ownership-ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestFileSpoolOwnershipCrossProcessKillReleases(t *testing.T) {
	cfg := testFileConfig(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := func(mode string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestFileSpoolOwnershipProcessHelper$")
		cmd.Env = append(os.Environ(), "ZEROEVENT_OWNERSHIP_HELPER="+mode, "ZEROEVENT_OWNERSHIP_DIRECTORY="+cfg.Directory)
		return cmd
	}
	holder := command("hold")
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()
	ready := make(chan struct{})
	var readyLine string
	var readyErr error
	go func() { readyLine, readyErr = bufio.NewReader(stdout).ReadString('\n'); close(ready) }()
	ownershipWait(t, ready)
	if readyErr != nil || readyLine != "ownership-ready\n" {
		t.Fatalf("holder startup: %q %v", readyLine, readyErr)
	}
	before, err := os.Stat(filepath.Join(cfg.Directory, spoolOwnershipFileName))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := command("reject").CombinedOutput(); err != nil {
		t.Fatalf("cross-process rejection: %v %s", err, output)
	}
	probe := ownershipSpool(t, cfg)
	if err := probe.Start(context.Background()); !errors.Is(err, ErrSpoolInUse) {
		t.Fatalf("parent contender: %v", err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = holder.Wait()
	waited = true
	if err == nil {
		t.Fatal("holder unexpectedly exited successfully after kill")
	}
	if err := probe.Start(context.Background()); err != nil {
		t.Fatalf("dead process retained ownership: %v", err)
	}
	after, err := os.Stat(filepath.Join(cfg.Directory, spoolOwnershipFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("dead process recovery replaced lock inode")
	}
	batch, err := probe.ReadBatch(context.Background(), 10)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Sequence != 2 {
		t.Fatalf("killed owner checkpoint replay: %+v %v", batch, err)
	}
}
