package localworkers

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CommandLauncher launches only the administrator's running ZBoard executable.
// It has no shell or HTTP input. A replaced/changed binary refuses new launches
// until the main service is restarted against that deployment.
type CommandLauncher struct {
	Executable   string
	ConfigFile   string
	DrainTimeout time.Duration
	MemoryBytes  uint64
	Stdout       io.Writer
	Stderr       io.Writer
	Ownership    *Ownership
	identity     os.FileInfo
	digest       [32]byte
}

func NewCommandLauncher(executable, configFile string, drain time.Duration, memory uint64) (*CommandLauncher, error) {
	if executable == "" || configFile == "" || drain < time.Second || drain > time.Minute || memory < 64<<20 || memory > 1<<40 {
		return nil, errors.New("invalid local projection worker command")
	}
	info, digest, err := executableDigest(executable)
	if err != nil {
		return nil, err
	}
	return &CommandLauncher{Executable: executable, ConfigFile: configFile, DrainTimeout: drain, MemoryBytes: memory, Stdout: os.Stdout, Stderr: os.Stderr, identity: info, digest: digest}, nil
}

func executableDigest(path string) (os.FileInfo, [32]byte, error) {
	var digest [32]byte
	file, err := os.Open(path)
	if err != nil {
		return nil, digest, errors.New("local projection executable unavailable")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 256<<20 {
		return nil, digest, errors.New("invalid local projection executable")
	}
	hash := sha256.New()
	if n, err := io.Copy(hash, io.LimitReader(file, 256<<20+1)); err != nil || n != before.Size() {
		return nil, digest, errors.New("local projection executable read failed")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, digest, errors.New("local projection executable changed")
	}
	copy(digest[:], hash.Sum(nil))
	return after, digest, nil
}

func (l *CommandLauncher) Start(ctx context.Context) (Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, digest, err := executableDigest(l.Executable)
	if err != nil || !os.SameFile(l.identity, info) || digest != l.digest {
		return nil, errors.New("local projection executable no longer matches the parent deployment")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(l.Executable, "-f", l.ConfigFile, "-projection-worker", "-projection-worker-background", "-projection-worker-database-connections", "2", "-projection-worker-drain-timeout", l.DrainTimeout.String())
	cmd.Stdout, cmd.Stderr = l.Stdout, l.Stderr
	if l.Ownership != nil {
		// The flock remains owned until the last supervised descendant exits,
		// even if the parent crashes before it can drain those processes.
		cmd.ExtraFiles = []*os.File{l.Ownership.file}
	}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GOMAXPROCS=") && !strings.HasPrefix(value, "GOMEMLIMIT=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	// Conservative process-local controls complement observed host reserves.
	// GOMEMLIMIT is a GC target, not a claim of a hard RSS limit.
	cmd.Env = append(cmd.Env, "GOMAXPROCS=1", "GOMEMLIMIT="+strconv.FormatUint(l.MemoryBytes, 10))
	configureChild(cmd)
	p := &commandProcess{command: cmd, done: make(chan error, 1)}
	started := make(chan error, 1)
	go func() {
		// Linux parent-death delivery is tied to the creating OS thread. Keep
		// that bounded supervisor thread alive until this exact child is reaped.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := cmd.Start()
		started <- err
		if err == nil {
			p.done <- cmd.Wait()
		}
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, errors.New("local projection worker failed to start")
	}
	return p, nil
}

type commandProcess struct {
	command *exec.Cmd
	done    chan error
}

func (p *commandProcess) PID() int           { return p.command.Process.Pid }
func (p *commandProcess) Done() <-chan error { return p.done }
func (p *commandProcess) Drain() error       { return drainProcess(p.command.Process) }
func (p *commandProcess) Kill() error        { return p.command.Process.Kill() }
