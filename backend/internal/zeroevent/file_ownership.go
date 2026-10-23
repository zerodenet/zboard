package zeroevent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrSpoolInUse           = errors.New("zero event spool directory is already owned")
	ErrSpoolLockUnsupported = errors.New("exclusive zero event spool ownership is unsupported on this platform")
)

// Keep this inode permanently: unlinking a locked file would let another
// process lock a replacement inode while the first owner still uses the spool.
const spoolOwnershipFileName = ".owner.lock"

func acquireSpoolOwnership(directory string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(directory, spoolOwnershipFileName), os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open spool ownership lock: %w", err)
	}
	if err := lockSpoolOwnership(file); err != nil {
		return nil, errors.Join(fmt.Errorf("acquire spool directory ownership: %w", err), file.Close())
	}
	return file, nil
}

func releaseSpoolOwnership(file *os.File) error {
	if file == nil {
		return nil
	}
	return errors.Join(unlockSpoolOwnership(file), file.Close())
}
