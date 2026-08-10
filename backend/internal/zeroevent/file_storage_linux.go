//go:build linux

package zeroevent

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func filesystemFreeBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	maxInt64 := uint64(^uint64(0) >> 1)
	if free > maxInt64 {
		return int64(maxInt64), nil
	}
	return int64(free), nil
}

func preallocateReserve(file *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	if err := unix.Fallocate(int(file.Fd()), 0, 0, size); err == nil {
		return nil
	} else if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) {
		return err
	}
	return zeroFillReserve(file, size)
}
