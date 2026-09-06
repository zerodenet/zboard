//go:build linux

package main

import "golang.org/x/sys/unix"

func diskFreeBytes(dir string) (*uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return nil, err
	}
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	return &free, nil
}
