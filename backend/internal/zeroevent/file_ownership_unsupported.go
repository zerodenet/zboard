//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package zeroevent

import "os"

// Never silently substitute a stale PID file for kernel-enforced ownership.
// In particular Windows builds must fail before reserve/checkpoint/recovery
// until their file-lock and durable-directory semantics are implemented.
func lockSpoolOwnership(*os.File) error   { return ErrSpoolLockUnsupported }
func unlockSpoolOwnership(*os.File) error { return ErrSpoolLockUnsupported }
