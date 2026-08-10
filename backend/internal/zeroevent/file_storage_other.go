//go:build !linux

package zeroevent

import "os"

func filesystemFreeBytes(string) (int64, error) {
	// Non-Linux builds still enforce MaxSize pressure. Returning a very large
	// free-space value disables the Linux-specific min-free-space signal without
	// making the spool unusable on development platforms.
	return int64(^uint64(0) >> 1), nil
}

func preallocateReserve(file *os.File, size int64) error {
	// Production file-spool deployments are Linux and use fallocate. Other
	// platforms keep a sparse logical reserve so local development does not write
	// hundreds of MiB during startup while still exercising reserve lifecycle.
	return file.Truncate(size)
}
