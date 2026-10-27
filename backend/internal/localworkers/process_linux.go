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
	file *os.File
	once sync.Once
	err  error
}

func (o *Ownership) Close() error {
	o.once.Do(func() { o.err = o.file.Close() })
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
	// Do not truncate, rewrite, remove or steal an existing ownership file.
	return &Ownership{file: file}, nil
}
