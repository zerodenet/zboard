//go:build linux

package localworkers

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func configureChild(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
func drainProcess(process *os.Process) error { return process.Signal(syscall.SIGTERM) }

// BackgroundPriority affects only an explicitly supervised projection child.
// It neither changes cgroups/security settings nor lowers the main service.
func BackgroundPriority() error {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, 19); err != nil {
		return errors.New("cannot set projection child background priority")
	}
	return nil
}

type Ownership struct {
	file   *os.File
	anchor *os.File
	once   sync.Once
	err    error
}

func (o *Ownership) Close() error {
	o.once.Do(func() { o.err = errors.Join(o.file.Close(), o.anchor.Close()) })
	return o.err
}

func AcquireLock(path string) (*Ownership, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("local projection supervisor lock unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return nil, errors.New("local projection supervisor lock must be a private regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, errors.New("local projection supervisor lock belongs to another owner")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another local projection supervisor owns this lock")
	}
	// A private lock path alone is administrator-controlled and cannot bound
	// two controllers configured with different paths. This fixed abstract
	// socket permits only one optional supervisor in this Linux network
	// namespace, conservatively even when the databases differ. No listener,
	// filesystem socket, security setting, or external process is modified.
	anchorFD, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		file.Close()
		return nil, errors.New("local projection supervisor namespace ownership unavailable")
	}
	anchor := os.NewFile(uintptr(anchorFD), "zboard-projection-supervisor-namespace")
	if err := syscall.Bind(anchorFD, &syscall.SockaddrUnix{Name: "\x00zboard.projection-supervisor.v1"}); err != nil {
		anchor.Close()
		file.Close()
		return nil, errors.New("local projection supervisor namespace ownership unavailable or already held")
	}
	// Both descriptors are inherited by managed children. Neither ownership
	// boundary is released by parent death while an old child remains alive.
	// Do not truncate, rewrite, remove or steal an existing ownership file.
	return &Ownership{file: file, anchor: anchor}, nil
}
