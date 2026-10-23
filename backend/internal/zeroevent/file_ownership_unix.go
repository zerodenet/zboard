//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package zeroevent

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockSpoolOwnership(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrSpoolInUse
	}
	return err
}

func unlockSpoolOwnership(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
