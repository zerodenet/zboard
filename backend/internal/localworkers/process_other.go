//go:build !linux

package localworkers

import (
	"errors"
	"os"
	"os/exec"
)

func configureChild(*exec.Cmd)               {}
func drainProcess(process *os.Process) error { return process.Signal(os.Interrupt) }
func BackgroundPriority() error              { return errors.New("supervised projection workers require Linux") }

type Ownership struct{ file, anchor *os.File }

func (o *Ownership) Close() error { return o.file.Close() }
func AcquireLock(string) (*Ownership, error) {
	return nil, errors.New("local projection supervisor ownership requires Linux")
}
